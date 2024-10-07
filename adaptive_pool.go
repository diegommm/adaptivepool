package adaptivepool

import (
	"bytes"
	"sync"
)

// PoolItemProvider relays policies and inner working of specific items in an
// [AdaptivePool]. Implementations should correctly handle `stdDev` being NaN
// when `n` is 1.
type PoolItemProvider[T any] interface {
	// Sizeof measures the size of an item. This measurement is used to compute
	// stats that allow efficiently reusing and creating items in an
	// [AdaptivePool].
	Sizeof(T) float64
	// Create returns a new item.
	Create(n, mean, stdDev float64) T
	// Accept returns whether an item of the given size should be accepted into
	// the [AdaptivePool] or just dropped for garbage collection.
	Accept(n, mean, stdDev, itemSize float64) bool
}

var _ PoolItemProvider[[]int] = NormalSlice[int]{}

// NormalSlice is a generic [PoolItemProvider] for slices that assumes a Normal
// Distribution of their length.
type NormalSlice[T any] struct {
	Threshold float64 // Threshold must be non-negative.
}

// Sizeof returns the length of the slice item.
func (p NormalSlice[T]) Sizeof(v []T) float64 {
	return float64(len(v))
}

// Create returns a new slice with length zero and cap `mean + Threshold *
// stdDev`. If `n` is 1, then cap will be `mean`.
func (p NormalSlice[T]) Create(n, mean, stdDev float64) []T {
	return make([]T, 0, int(normalCreateSize(n, mean, stdDev, p.Threshold)))
}

// Accept will accept a new item if its length is in the inclusive range `mean ±
// Threshold * stdDev`. If `n` is 1, then the item is discarded.
func (p NormalSlice[T]) Accept(n, mean, stdDev, itemSize float64) bool {
	return normalAccept(n, mean, stdDev, p.Threshold, itemSize)
}

var _ PoolItemProvider[*bytes.Buffer] = NormalBytesBuffer{}

// NormalBytesBuffer is a [PoolItemProvider] for *[bytes.Buffer]s that assumes a
// Normal Distribution of their `Len`.
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
// Threshold * stdDev`. If `n` is 1, then the item is discarded.
func (p NormalBytesBuffer) Accept(n, mean, stdDev, itemSize float64) bool {
	return normalAccept(n, mean, stdDev, p.Threshold, itemSize)
}

// AdaptivePool is a [sync.Pool] that uses a [PoolItemProvider] to efficiently
// create and reuse new pool items. Statistics are updated each time the
// `Release` method is called for an item, regardless if it will be put back in
// the sync.Pool. As with a regular sync.Pool, it can be "seeded" with objects
// by calling `Release`, with the additionaly property that statistics will also
// be seeded this way.
type AdaptivePool[T any] struct {
	pool     pool
	provider PoolItemProvider[T]

	statsMu sync.Mutex
	stats   stats
}

// New creates an AdaptivePool for a given type that uses the `sizeof` function
// to measure the object's size, and `create` to allocate a new one.
func New[T any](p PoolItemProvider[T]) *AdaptivePool[T] {
	ret := &AdaptivePool[T]{
		provider: p,
		stats:    new(Stats),
	}
	ret.pool = &sync.Pool{
		New: ret.new,
	}
	return ret
}

func (p *AdaptivePool[T]) new() any {
	return p.provider.Create(p.ReadStats())
}

// ReadStats returns the current values of the internal statistics about item
// sizes released.
func (p *AdaptivePool[T]) ReadStats() (n, mean, stdDev float64) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats.N(), p.stats.Mean(), p.stats.StdDev()
}

// Acquire returns a new object from the pool, allocating it if needed.
func (p *AdaptivePool[T]) Acquire() T {
	return p.pool.Get().(T)
}

// Release updates the internal statistics with the size of the object and puts
// it back to the pool if the [PoolItemProvider.Accept] allows it.
func (p *AdaptivePool[T]) Release(x T) {
	s := p.provider.Sizeof(x)
	n, mn, sd := p.writeThenRead(s)
	if p.provider.Accept(n, mn, sd, s) {
		p.pool.Put(x)
	}
}

func (p *AdaptivePool[T]) writeThenRead(s float64) (n, mean, stdDev float64) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats.Push(s)
	return p.stats.N(), p.stats.Mean(), p.stats.StdDev()
}

func normalCreateSize(n, mean, stdDev, thresh float64) float64 {
	if n > 1 {
		return mean + thresh*stdDev
	}
	return mean
}

func normalAccept(n, mean, stdDev, thresh, itemSize float64) bool {
	return mean-thresh*stdDev <= itemSize && itemSize <= mean+thresh*stdDev
}

type pool interface {
	Get() any
	Put(any)
}
