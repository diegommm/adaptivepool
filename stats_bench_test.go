package adaptivepool

import "testing"

func BenchmarkStats(b *testing.B) {
	// Consider running this benchmark like this for consistency with previous
	// commits
	//	go test -run=- -bench=Stats/implem -count=20 | benchstat -col=/implem -

	values := allTestDataInputValues(b)
	b.Run("implem=stats0", benchStats(new(stats0), values))
	b.Run("implem=stats1", benchStats(new(stats1), values))
	b.Run("implem=stats2", benchStats(new(stats2), values))
}

func benchStats(st stats, values []float64) func(b *testing.B) {
	lower := func(a, b float64) float64 {
		if a < b {
			return a
		}
		return b
	}
	var witness float64 // prevent the compiler from being too smart

	return func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, v := range values {
				st.Push(v)
				witness = lower(witness, st.N())
				witness = lower(witness, st.Mean())
				witness = lower(witness, st.StdDev())
			}
		}
	}
}
