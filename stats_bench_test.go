package adaptivepool

import "testing"

func BenchmarkStats(b *testing.B) {
	// Consider running this benchmark like this for consistency with previous
	// commits
	//	go test -run=- -bench=Stats -count=20 | benchstat -col=/implem -

	b.Run("implem=default", func(b *testing.B) {
		st := new(Stats)
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			st.Push(float64(i))
			st.N()
			st.Mean()
			st.StdDev()
		}
	})
}
