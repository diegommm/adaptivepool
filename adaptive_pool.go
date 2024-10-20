// Package adaptivepool provides a free list based on [sync.Pool] that can
// stochastically define which items should be reused, based on a measure of
// choice called size.
package adaptivepool

import (
	"bytes"
	"math"
	"sync"
	"sync/atomic"
)

// PoolItemProvider handles both item type-specific operations as well as the
// policy for determining when to reuse items in an [AdaptivePool].
// Implementations should correctly handle `stdDev` being NaN when `n` is 1.
type PoolItemProvider[T any] interface {
	// Sizeof measures the size of an item. This measurement is used to compute
	// stats that allow efficiently reusing and creating items in an
	// AdaptivePool. It should not hold references to the item.
	Sizeof(T) float64
	// Create returns a new item. It has a set of basic stats about the
	// AdaptivePool usage that allows efficient pre-allocation in many common
	// scenarios.
	Create(mean, stdDev float64) T
	// Accept returns whether an item of the given size should be accepted into
	// the internal sync.Pool of an AdaptivePool, or otherwise just dropped for
	// garbage collection.
	Accept(mean, stdDev, itemSize float64) bool
}

// NormalSlice is a generic [PoolItemProvider] for slice items, operating under
// the assumption that their `len` follow a Normal Distribution.
type NormalSlice[T any] struct {
	Threshold float64 // Threshold must be non-negative.
}

// Sizeof returns the length of the slice.
func (p NormalSlice[T]) Sizeof(v []T) float64 {
	return float64(len(v))
}

// Create returns a new slice with length zero and cap `mean + Threshold *
// stdDev`, or `mean` if `stdDev` is `NaN`.
func (p NormalSlice[T]) Create(mean, stdDev float64) []T {
	return make([]T, 0, int(normalCreateSize(mean, stdDev, p.Threshold)))
}

// Accept will accept a new item if its length is in the inclusive range `mean ±
// Threshold * stdDev`, or if `stdDev` is `NaN`.
func (p NormalSlice[T]) Accept(mean, stdDev, itemSize float64) bool {
	return normalAccept(mean, stdDev, p.Threshold, itemSize)
}

// NormalBytesBuffer is a [PoolItemProvider] for [*bytes.Buffer] items,
// operating under the assumption that their `Len` follow a Normal Distribution.
type NormalBytesBuffer struct {
	Threshold float64 // Threshold must be non-negative.
}

// Sizeof returns the length of the buffer.
func (p NormalBytesBuffer) Sizeof(v *bytes.Buffer) float64 {
	if v == nil {
		return 0
	}
	return float64(v.Len())
}

// Create returns a new buffer with `Len` zero and `Cap` `mean + Threshold *
// stdDev`, or `mean` if `stdDev` is `NaN`.
func (p NormalBytesBuffer) Create(mean, stdDev float64) *bytes.Buffer {
	size := normalCreateSize(mean, stdDev, p.Threshold)
	return bytes.NewBuffer(make([]byte, 0, int(size)))
}

// Accept will accept a new item if its `Len` is in the inclusive range `mean ±
// Threshold * stdDev`, or if `stdDev` is `NaN`.
func (p NormalBytesBuffer) Accept(mean, stdDev, itemSize float64) bool {
	return normalAccept(mean, stdDev, p.Threshold, itemSize)
}

// AdaptivePool is a [sync.Pool] that uses a [PoolItemProvider] to efficiently
// create and reuse new pool items. Statistics are updated each time the `Put`
// method is called for an item, regardless if it will be put back in the
// sync.Pool. As with a regular sync.Pool, it can be "seeded" with objects by
// calling `Put`, with the additional property that statistics will also be
// seeded this way.
type AdaptivePool[T any] struct {
	pool     pool
	provider PoolItemProvider[T]

	// reading is lock-free, and actually uses 32bit floating points to store
	// mean and stdDev in a single 64bit atomic value
	rStats atomic.Uint64

	statsMu sync.RWMutex
	stats   Stats
}

// New creates an AdaptivePool. See [Stats.SetMaxN] for a description of the
// `maxN` argument.
func New[T any](p PoolItemProvider[T], maxN float64) *AdaptivePool[T] {
	return new(AdaptivePool[T]).init(p, maxN)
}

func (p *AdaptivePool[T]) init(
	pp PoolItemProvider[T],
	maxN float64,
) *AdaptivePool[T] {
	p.provider = pp
	p.stats.SetMaxN(maxN)
	p.pool = &sync.Pool{
		New: p.new,
	}
	return p
}

// Stats returns a snapshot of the pool statistics.
func (p *AdaptivePool[T]) Stats() Stats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

// Get returns a new object from the pool, allocating it from the
// PoolItemProvider if needed.
func (p *AdaptivePool[T]) Get() T {
	return p.pool.Get().(T)
}

// Put updates the internal statistics with the size of the object and puts
// it back to the pool if [PoolItemProvider.Accept] allows it.
func (p *AdaptivePool[T]) Put(x T) {
	s := p.provider.Sizeof(x)
	mean, stdDev := p.writeThenRead(s)
	if p.provider.Accept(mean, stdDev, s) {
		p.pool.Put(x)
	}
}

func (p *AdaptivePool[T]) writeThenRead(s float64) (mean, stdDev float64) {
	// this could be changed to a TryLock and return an additional false on lock
	// failure, in which case the item would also not be put in the pool
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats.Push(s)
	mn32, sd32 := float32(p.stats.Mean()), float32(p.stats.StdDev())
	u64 := encodeBits(mn32, sd32)
	p.rStats.Store(u64)

	// reduced precision for consistency with the values passed to `Create`
	return float64(mn32), float64(sd32)
}

func (p *AdaptivePool[T]) new() any {
	mn32, sd32 := decodeBits(p.rStats.Load())
	return p.provider.Create(float64(mn32), float64(sd32))
}

func normalCreateSize(mean, stdDev, thresh float64) float64 {
	if math.IsNaN(stdDev) {
		return mean
	}
	return mean + thresh*stdDev
}

func normalAccept(mean, stdDev, thresh, itemSize float64) bool {
	sdThresh := thresh * stdDev
	return mean-sdThresh <= itemSize && itemSize <= mean+sdThresh ||
		math.IsNaN(stdDev)
}

func encodeBits(lo, hi float32) uint64 {
	return uint64(math.Float32bits(lo)) +
		uint64(math.Float32bits(hi))<<32
}

func decodeBits(u64 uint64) (lo, hi float32) {
	return math.Float32frombits(uint32(u64 & (1<<32 - 1))),
		math.Float32frombits(uint32(u64 >> 32))
}

type pool interface {
	Get() any
	Put(any)
}
