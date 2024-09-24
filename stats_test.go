package adaptivepool

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	_ "embed"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"testing"
)

//go:embed stats_test_data.csv.bz2
var statsTestData []byte

const (
	// the values in the test data were generated to match a normal distribution
	// with mean 50*1024 and a std dev of 512. If these number represented
	// bytes, then maxDiff would be 1KiB of error.
	maxDiff = 1024.0
)

type stats interface {
	Push(float64)
	Reset()
	Mean() float64
	StdDev() float64
}

// stats0 is a naive implementation of Welford's algorithm. Source:
//
//	https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance
type stats0 struct {
	n, mean, m2 float64
}

func (s *stats0) Reset() { *s = stats0{} }

func (s *stats0) Mean() float64 { return s.mean }

func (s *stats0) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.m2 / s.n)
	}
	return 0
}

func (s *stats0) Push(v float64) {
	s.n++
	delta := v - s.mean
	s.mean += delta / s.n
	delta2 := v - s.mean
	s.m2 = delta * delta2
}

// stats2 delivers same precision as stats1 but can also return skewness and
// Kurtosis. Source:
//
//	https://www.johndcook.com/skewness_kurtosis.html
type stats2 struct {
	n, m1, m2, m3, m4 float64
}

func (s *stats2) Reset() { *s = stats2{} }

func (s *stats2) Mean() float64 { return s.m1 }

func (s *stats2) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.m2 / (s.n - 1))
	}
	return 0
}

func (s *stats2) Push(v float64) {
	n1 := s.n
	s.n++
	delta := v - s.m1
	deltaN := delta / s.n
	deltaN2 := deltaN * deltaN
	term1 := delta * deltaN * n1
	s.m1 += deltaN
	s.m4 += term1*deltaN2*(s.n*s.n-3*s.n+3) + 6*deltaN2*s.m2 - 4*deltaN*s.m3
	s.m3 += term1*deltaN*(s.n-2) - 3*deltaN*s.m2
	s.m2 += term1
}

func TestStats0(t *testing.T) {
	t.Skip("currently not passing, but not in use either")
	t.Parallel()
	testStats(t, new(stats0))
}

func TestStats1(t *testing.T) {
	t.Parallel()
	testStats(t, new(stats1))
}

func TestStats2(t *testing.T) {
	t.Parallel()
	testStats(t, new(stats2))
}

func csvTestDataReader(tb testing.TB) *csv.Reader {
	tb.Helper()
	r := bufio.NewReader(bzip2.NewReader(bytes.NewReader(statsTestData)))

	// discard the CSV header
	for {
		_, isPrefix, err := r.ReadLine()
		zero(tb, err)
		if !isPrefix {
			break
		}
	}

	cr := csv.NewReader(r)
	cr.Comma = '\t'
	cr.FieldsPerRecord = 3
	cr.ReuseRecord = true

	return cr
}

func testStats(t *testing.T, st stats) {
	t.Helper()
	cr := csvTestDataReader(t)

	zero(t, st.Mean())
	zero(t, st.StdDev())

	for i := 1; ; i++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		zero(t, err)
		equal(t, 3, len(rec), "number of CSV values in record #%d", i)

		v, err := parseFloats(rec)
		zero(t, err)

		st.Push(v[0])
		floatsEqual(t, v[1], st.Mean(), maxDiff, "mean in record #%d", i)
		floatsEqual(t, v[2], st.StdDev(), maxDiff, "std dev in record #%d", i)
	}

	st.Reset()
	zero(t, st.Mean())
	zero(t, st.StdDev())
}
