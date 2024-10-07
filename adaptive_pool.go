// Package adaptivepool provides a free list based on [sync.Pool] that can
// stochastically define which items should be reused, based on a measure of
// choice, called size.
package adaptivepool

import (
	"bytes"
	"sync"
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
	Create(n, mean, stdDev float64) T
	// Accept returns whether an item of the given size should be accepted into
	// the internal sync.Pool of an AdaptivePool, or otherwise just dropped for
	// garbage collection.
	Accept(n, mean, stdDev, itemSize float64) bool
}

var _ PoolItemProvider[[]int] = NormalSlice[int]{}

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
// stdDev`. If `n` is 1, then cap will be `mean`.
func (p NormalSlice[T]) Create(n, mean, stdDev float64) []T {
	return make([]T, 0, int(normalCreateSize(n, mean, stdDev, p.Threshold)))
}

// Accept will accept a new item if its length is in the inclusive range `mean ±
// Threshold * stdDev`. If `n` is 1, then the item is accepted.
func (p NormalSlice[T]) Accept(n, mean, stdDev, itemSize float64) bool {
	return normalAccept(n, mean, stdDev, p.Threshold, itemSize)
}

var _ PoolItemProvider[*bytes.Buffer] = NormalBytesBuffer{}

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
// stdDev`. If `n` is 1, then Cap will be `mean`.
func (p NormalBytesBuffer) Create(n, mean, stdDev float64) *bytes.Buffer {
	size := normalCreateSize(n, mean, stdDev, p.Threshold)
	return bytes.NewBuffer(make([]byte, 0, int(size)))
}

// Accept will accept a new item if its `Len` is in the inclusive range `mean ±
// Threshold * stdDev`. If `n` is 1, then the item is accepted.
func (p NormalBytesBuffer) Accept(n, mean, stdDev, itemSize float64) bool {
	return normalAccept(n, mean, stdDev, p.Threshold, itemSize)
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

	statsMu sync.RWMutex
	stats   Stats
}

// New creates an AdaptivePool using the given PoolItemProvider.
func New[T any](p PoolItemProvider[T]) *AdaptivePool[T] {
	ret := &AdaptivePool[T]{
		provider: p,
	}
	ret.pool = &sync.Pool{
		New: ret.new,
	}
	return ret
}

// ReadStats returns statistics about the size of items that were put in the
// pool.
func (p *AdaptivePool[T]) ReadStats() (n, mean, stdDev float64) {
	st := p.readStats()
	return st.N(), st.Mean(), st.StdDev()
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
	st := p.writeThenRead(s)
	if p.provider.Accept(st.N(), st.Mean(), st.StdDev(), s) {
		p.pool.Put(x)
	}
}

func (p *AdaptivePool[T]) new() any {
	return p.provider.Create(p.ReadStats())
}

func (p *AdaptivePool[T]) readStats() Stats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()
	return p.stats
}

func (p *AdaptivePool[T]) writeThenRead(s float64) Stats {
	// this could be changed to a TryLock and return an additional false on lock
	// failure, in which case the item would also not be put in the pool. This
	// could help bias towards the read path
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats.Push(s)
	return p.stats
}

func normalCreateSize(n, mean, stdDev, thresh float64) float64 {
	if n > 1 {
		return mean + thresh*stdDev
	}
	return mean
}

func normalAccept(n, mean, stdDev, thresh, itemSize float64) bool {
	sdThresh := thresh * stdDev
	return mean-sdThresh <= itemSize && itemSize <= mean+sdThresh || n < 2
}

type pool interface {
	Get() any
	Put(any)
}
