package adaptivepool

import (
	"bytes"
	"math"
	"sync"
	"sync/atomic"
)

// ItemProvider handles and measures items for an [AdaptivePool].
type ItemProvider[T any] interface {
	// Costof measures the cost of an item. Items with cost zero will not be put
	// back in the pool nor will be fed into statistics.
	Costof(T) int
	// New creates a new item with cost zero, but pre-allocated to `prealloc`
	// cost. If it is not possible to perform this preallocation, it is
	// acceptable to return an item with a smaller preallocated cost.
	New(prealloc int) T
	// Reset clears leftover data from past uses.
	Reset(T) T
}

// SliceProvider is a generic [ItemProvider] for slice items.
type SliceProvider[T any] struct{}

// Costof returns the capacity of the slice.
func (p SliceProvider[T]) Costof(v []T) int {
	return cap(v)
}

// New returns a new slice with `len` zero and `cap` equal to `prealloc`.
func (p SliceProvider[T]) New(prealloc int) []T {
	return make([]T, 0, prealloc)
}

// Reset clears the underlying elements and reslices the item to zero-length.
func (p SliceProvider[T]) Reset(v []T) []T {
	if v != nil {
		clear(v[:cap(v)])
		v = v[:0]
	}
	return v
}

// BytesBufferProvider is an [ItemProvider] for [*bytes.Buffer] items.
type BytesBufferProvider struct{}

// Costof returns the capacity of the buffer if it's not nil, and zero if it's
// nil.
func (p BytesBufferProvider) Costof(v *bytes.Buffer) int {
	if v != nil {
		return v.Cap()
	}
	return 0
}

// Reset clears the underlying data and returns the buffer after resetting it.
func (p BytesBufferProvider) Reset(v *bytes.Buffer) *bytes.Buffer {
	if v != nil {
		v.Reset()
		b := v.Bytes()
		clear(b[:cap(b)])
	}
	return v
}

// New returns a new *bytes.Buffer with `Len` zero and `Cap` equal to
// `prealloc`.
func (p BytesBufferProvider) New(prealloc int) *bytes.Buffer {
	return bytes.NewBuffer(make([]byte, 0, prealloc))
}

// EstimatorStats provides a set of statistics based on observed item costs put
// in an [AdaptivePool].
type EstimatorStats struct {
	Mean   float64 // Arithmetic Mean
	StdDev float64 // Population Standard Deviation

	// FIXME: it would be interesting to at least provide N. The current
	// implementation is lock-free in the read-path at the cost of Mean and
	// StdDev actually having float32 precision. In order to add more stats, it
	// would probably require to protect the read-path. That could also give
	// back their precision to Mean and StdDev. We have this struct so that
	// future iterations can solve for that independently of the rest of the
	// code, allowing previous Estimator implementations to keep working.
}

// Estimator provides opinions for decisions made by an [AdaptivePool].
// Implementations should correctly handle `stdDev` being NaN.
type Estimator interface {
	// Suggest returns a suggested item cost for a new item.
	Suggest(EstimatorStats) int
	// Accept returns whether an item of the given cost should be accepted into
	// the internal sync.Pool of an AdaptivePool, or otherwise just dropped for
	// garbage collection.
	Accept(s EstimatorStats, itemCost int) bool
}

// NormalEstimator is an [Estimator] that assumes a Normal Distribution over the
// item costs put into an [AdaptivePool].
type NormalEstimator struct {
	Threshold float64 // Threshold must be non-negative.
	MinCost   int     // Minimum cost that will be suggested.
}

// Suggest returns `mean ± e.Threshold * stdDev` as an estimation if `stdDev` is
// not `NaN`, or `mean` otherwise. If `MinCost` is positive, then it will not
// return a value less than that.
func (e NormalEstimator) Suggest(s EstimatorStats) int {
	if math.IsNaN(s.StdDev) {
		return max(e.MinCost, int(math.Round(s.Mean)))
	}
	return max(e.MinCost, int(math.Round(s.Mean+e.Threshold*s.StdDev)))
}

// Accept will return true if `stdDev` is `NaN` or if `itemCost` is in the
// inclusive range `mean ± e.Threshold * stdDev`.
func (e NormalEstimator) Accept(s EstimatorStats, itemCost int) bool {
	if math.IsNaN(s.StdDev) {
		return true
	}
	ct64 := float64(itemCost)
	sdThresh := e.Threshold * s.StdDev
	return s.Mean-sdThresh <= ct64 && ct64 <= s.Mean+sdThresh
}

// AdaptivePool uses an [Estimator] to more effectively use an internal
// [sync.Pool] that holds items created by an [ItemProvider].
type AdaptivePool[T any] struct {
	pool      pool
	provider  ItemProvider[T]
	estimator Estimator

	// reading is lock-free, and actually uses 32bit floating points to store
	// mean and stdDev in a single 64bit atomic value
	rStats atomic.Uint64

	statsMu sync.RWMutex
	stats   Stats
}

// New creates an AdaptivePool. See [Stats.SetMaxN] for a description of the
// `maxN` argument.
func New[T any](p ItemProvider[T], e Estimator, maxN float64) *AdaptivePool[T] {
	return new(AdaptivePool[T]).init(p, e, maxN)
}

func (p *AdaptivePool[T]) init(
	pp ItemProvider[T],
	e Estimator,
	maxN float64,
) *AdaptivePool[T] {
	p.provider = pp
	p.estimator = e
	p.stats.SetMaxN(maxN)
	p.pool = new(sync.Pool)
	return p
}

// Get returns a new object from the pool, allocating it from the ItemProvider
// if needed.
func (p *AdaptivePool[T]) Get() T {
	if v := p.pool.Get(); v != nil {
		return v.(T)
	}
	mn32, sd32 := decodeBits(p.rStats.Load())
	cost := p.estimator.Suggest(EstimatorStats{
		Mean:   float64(mn32),
		StdDev: float64(sd32),
	})
	return p.provider.New(cost)
}

// GetWithCost returns a new object with the specified cost from the pool,
// allocating it from the ItemProvider if needed.
func (p *AdaptivePool[T]) GetWithCost(cost int) T {
	if v := p.pool.Get(); v != nil {
		// if the item we got from the pool is smaller than needed, drop it for
		// garbage collection and instead directly allocate a new one with the
		// appropriate cost
		if ret := v.(T); p.provider.Costof(ret) >= cost {
			return ret
		}
	}
	return p.provider.New(cost)
}

// Put updates the internal statistics with the cost of the object and puts
// it back into the pool if [Estimator.Accept] allows it. Items with a
// non-positive cost are immediately dropped.
func (p *AdaptivePool[T]) Put(x T) {
	// we call Reset here, which would allow an implementation to hijack the
	// item if they wanted to, and then return a zero-cost item
	p.provider.Reset(x)
	s := p.provider.Costof(x)
	if s < 1 {
		return
	}
	mean, stdDev := p.writeThenRead(s)
	st := EstimatorStats{
		Mean:   mean,
		StdDev: stdDev,
	}
	if p.estimator.Accept(st, s) {
		p.pool.Put(x)
	}
}

func (p *AdaptivePool[T]) writeThenRead(s int) (mean, stdDev float64) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats.Push(float64(s))
	mn32, sd32 := float32(p.stats.Mean()), float32(p.stats.StdDev())
	u64 := encodeBits(mn32, sd32)
	p.rStats.Store(u64)

	// reduced precision for consistency with the values passed to `Create`
	return float64(mn32), float64(sd32)
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
