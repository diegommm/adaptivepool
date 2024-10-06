package adaptivepool

import (
	"testing"
)

func BenchmarkStats0(b *testing.B) {
	benchStats(b, new(stats0))
}

func BenchmarkStats1(b *testing.B) {
	benchStats(b, new(stats1))
}

func BenchmarkStats2(b *testing.B) {
	benchStats(b, new(stats2))
}

func benchStats(b *testing.B, st stats) {
	b.Helper()

	values := allTestDataInputValues(b)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, v := range values {
			st.Push(v)
		}
	}
}
