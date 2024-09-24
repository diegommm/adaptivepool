package adaptivepool

import (
	"errors"
	"io"
	"strconv"
	"testing"
)

func allTestDataInputValues(tb testing.TB) []float64 {
	tb.Helper()

	ret := make([]float64, 0, 10_000)

	cr := csvTestDataReader(tb)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		zero(tb, err)

		f, err := strconv.ParseFloat(rec[0], 64)
		zero(tb, err)

		ret = append(ret, f)
	}

	return ret
}

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

	// witness is only used to make sure the compiler doesn't optimize discarded
	// output values
	var witness float64

	for i := 0; i < b.N; i++ {
		for _, v := range values {
			st.Push(v)
			v := st.Mean()
			if v > witness {
				witness = v
			}
			v = st.StdDev()
			if v > witness {
				witness = v
			}
		}
	}
}
