package adaptivepool

import (
	"sync"
	"testing"
)

type nopProvider struct{}

func (p nopProvider) Costof(v int) int     { return 1 }
func (p nopProvider) New(prealloc int) int { return 1 }
func (p nopProvider) Reset(v int) int      { return 0 }

var _ ItemProvider[int] = nopProvider{}

type nopEstimator struct {
	accept bool
}

func (nopEstimator) Suggest(EstimatorStats) int             { return 1 }
func (e nopEstimator) Accept(s EstimatorStats, ct int) bool { return e.accept }

var _ Estimator = nopEstimator{}

func BenchmarkAdaptivePool(b *testing.B) {
	// Consider running this benchmark like this for consistency with previous
	// commits
	//	go test -run=- -bench=AdaptivePool -count=20 |
	//	benchstat -col=/implem,/method -

	// For the Get* methods we do our best to try to add enough items before the
	// actual benchmark starts, but it cannot be guaranteed that all of them
	// will be available by the last iterations because sync.Pool is free to not
	// hold them. Hence, we know it is possible that there is some error in
	// those cases. For this reason, we try to report an extra metric called
	// "valid" in those cases, but there are some cases where that just cannot
	// be done either. By judging the results of the ones with the "valid"
	// metric, it would appear that sync.Pool does not kick any of the items we
	// pre-Put in it, probably due to them being so tightly close. The `if` and
	// the counter being incremented could also cause a distortion, but should
	// be negligible

	b.Run("implem=sync.Pool/method=Get/created=true", func(b *testing.B) {
		p := &sync.Pool{
			New: func() any {
				return 1
			},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p.Get()
		}
	})

	b.Run("implem=sync.Pool/method=Get/created=false", func(b *testing.B) {
		p := new(sync.Pool)
		for i := 0; i < b.N; i++ {
			p.Put(1) //nolint:staticcheck
		}
		var valid int
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if p.Get() != nil {
				valid++
			}
		}
		b.ReportMetric(float64(valid), "valid")
	})

	b.Run("implem=sync.Pool/method=Put/accepted=true", func(b *testing.B) {
		p := new(sync.Pool)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p.Put(1) //nolint:staticcheck
		}
	})

	b.Run("implem=sync.Pool/method=Put/accepted=false", func(b *testing.B) {
		p := new(sync.Pool)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p.Put(nil)
		}
	})

	b.Run("implem=AdaptivePool/method=Get/created=true", func(b *testing.B) {
		ap := New(nopProvider{}, nopEstimator{true}, 500)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ap.Get()
		}
	})

	b.Run("implem=AdaptivePool/method=Get/created=false", func(b *testing.B) {
		ap := New(nopProvider{}, nopEstimator{true}, 500)
		for i := 0; i < b.N; i++ {
			ap.Put(2)
		}
		var valid int
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ap.Get() == 2 {
				valid++
			}
		}
		b.ReportMetric(float64(valid), "valid")
	})

	b.Run("implem=AdaptivePool/method=GetWithCost/created=true/fromPool=true",
		func(b *testing.B) {
			ap := New(nopProvider{}, nopEstimator{true}, 500)
			for i := 0; i < b.N; i++ {
				ap.Put(1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ap.GetWithCost(2)
			}
			// we don't have a way to report the "valid" metric here
		})

	b.Run("implem=AdaptivePool/method=GetWithCost/created=true/fromPool=false",
		func(b *testing.B) {
			ap := New(nopProvider{}, nopEstimator{true}, 500)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ap.GetWithCost(2)
			}
		})

	b.Run("implem=AdaptivePool/method=GetWithCost/created=false/fromPool=true",
		func(b *testing.B) {
			ap := New(nopProvider{}, nopEstimator{true}, 500)
			for i := 0; i < b.N; i++ {
				ap.Put(1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ap.GetWithCost(1)
			}
			// we don't have a way to report the "valid" metric here
		})

	b.Run("implem=AdaptivePool/method=Put/accepted=true",
		func(b *testing.B) {
			ap := New(nopProvider{}, nopEstimator{true}, 500)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ap.Put(1)
			}
		})

	b.Run("implem=AdaptivePool/method=Put/accepted=false",
		func(b *testing.B) {
			ap := New(nopProvider{}, nopEstimator{false}, 500)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ap.Put(1)
			}
		})
}
