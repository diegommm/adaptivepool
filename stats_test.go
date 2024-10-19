package adaptivepool

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

type stats interface {
	Push(float64)
	Reset()
	N() float64
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

func (s *stats0) N() float64 { return s.n }

func (s *stats0) Mean() float64 { return s.mean }

func (s *stats0) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.m2 / s.n)
	}
	return math.NaN()
}

func (s *stats0) Push(v float64) {
	s.n++
	delta := v - s.mean
	s.mean += delta / s.n
	delta2 := v - s.mean
	s.m2 = delta * delta2
}

// stats1 is generally better than stats0. It is also an implementation of
// Welford's algorithm but with a revision to reduce computing error, originally
// presented by Knuth's Art of Computer Programming. This is the fastest
// alternative among those who provide the best accuracy.
// Source:
//
//	https://www.johndcook.com/blog/standard_deviation/
type stats1 = Stats

// stats2 delivers comparable precision and performance as stats1 but can also
// return skewness and Kurtosis, at the expense of more code. Source:
//
//	https://www.johndcook.com/skewness_kurtosis.html
type stats2 struct {
	n, m1, m2, m3, m4 float64
}

func (s *stats2) Reset() { *s = stats2{} }

func (s *stats2) N() float64 { return s.n }

func (s *stats2) Mean() float64 { return s.m1 }

func (s *stats2) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.m2 / s.n)
	}
	return math.NaN()
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
	t.Parallel()

	// Mean appears to have great precision from the start and be constant
	const meanMaxRelErrPercExp = 12

	// Standard deviation, on the other side, appears to be unusable. This might
	// be related to an implementation mistake here.

	testStats(t, new(stats0),
		constMaxRelErrPerc(math.Pow(10, -meanMaxRelErrPercExp)),
		errTestSkip)
}

func TestStats12(t *testing.T) {
	t.Parallel()

	// Stats1 and Stats2 appear to follow comparable (if not the same) precision
	// models

	// Mean appears to have great precision from the start and be constant
	const meanMaxRelErrPercExp = 12

	// Parameters for powfRelErrPerc for standard deviation. Relative error
	// starts relatively high using the current data set, but rapidly decreases:
	//	errRelPerc<30 at N=2
	//	errRelPerc<20 at N=3
	//	errRelPerc<10 at N=6
	//	errRelPerc<5  at N=11
	//	errRelPerc<1  at N=51
	const (
		xShift = -1
		a      = 30
		b      = -0.7
		c      = 0
	)

	t.Run("stats1", func(t *testing.T) {
		t.Parallel()

		testStats(t, new(stats1),
			constMaxRelErrPerc(math.Pow(10, -meanMaxRelErrPercExp)),
			powfRelErrPerc(xShift, a, b, c))
	})

	t.Run("stats2", func(t *testing.T) {
		t.Parallel()

		testStats(t, new(stats2),
			constMaxRelErrPerc(math.Pow(10, -meanMaxRelErrPercExp)),
			powfRelErrPerc(xShift, a, b, c))
	})
}

// errTestFunc returns whether it passes.
type errTestFunc = func(n, expected, got float64) bool

func errTestSkip(n, expected, got float64) bool {
	return true
}

func constMaxRelErrPerc(maxRelErrPerc float64) errTestFunc {
	return func(_, expected, got float64) bool {
		return relErrPerc(expected, got) < maxRelErrPerc
	}
}

func powfRelErrPerc(xShift, a, b, c float64) errTestFunc {
	// xShift allows adjusting the curve for the first value for standard
	// deviation, which is not defined
	return func(n, expected, got float64) bool {
		return relErrPerc(expected, got) < powf(n+xShift, a, b, c)
	}
}

func powf(x, a, b, c float64) float64 {
	return a*math.Pow(x, b) + c
}

func relErrPerc(expected, got float64) float64 {
	return 100 * math.Abs(expected-got) / expected
}

func assertErrTest(tb testing.TB, f errTestFunc, n, expected, got float64,
	measure string) {
	if !f(n, expected, got) {
		tb.Errorf("error out of bounds for measured %s: N=%v; expected=%v;"+
			" got=%v", measure, n, expected, got)
	}
}

func testStats(t *testing.T, st stats, meanErrOK, sdErrOK errTestFunc) {
	t.Helper()
	cr := csvTestDataReader(t)

	zero(t, st.Mean())
	sd := st.StdDev()
	equal(t, true, math.IsNaN(sd), "unexpected non-NaN std dev for"+
		" non-initialized stats: %v", sd)

	v := make([]float64, 3)
	for i := 1; ; i++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		zero(t, err)
		equal(t, 3, len(rec), "number of CSV values in record #%d", i)

		err = parseFloats(rec, v)
		zero(t, err)

		st.Push(v[0])
		n := st.N()
		equal(t, float64(i), n, "expected values count")

		assertErrTest(t, meanErrOK, n, v[1], st.Mean(), "mean")
		sd := st.StdDev()
		if i < 2 {
			equal(t, true, math.IsNaN(sd), "unexpected non-NaN std dev for"+
				" iteration #%d: %v", i, sd)
		} else {
			assertErrTest(t, sdErrOK, n, v[2], sd, "standard deviation")
		}
	}

	st.Reset()
	zero(t, st.Mean())
	sd = st.StdDev()
	equal(t, true, math.IsNaN(sd), "unexpected non-NaN std dev for"+
		" cleared stats: %v", sd)
}

func TestStatsMaxN(t *testing.T) {
	t.Parallel()

	st := new(Stats)
	zero(t, st.MaxN())
	st.SetMaxN(0.1)
	zero(t, st.MaxN())
	st.SetMaxN(-1)
	zero(t, st.MaxN())

	st.Push(1)
	st.Push(1)
	st.Push(1)
	st.Push(1)

	st.SetMaxN(1)
	equal(t, 1, st.MaxN(), "maxN")
	equal(t, 1, st.N(), "maxN")

	st.SetMaxN(0)
	st.Push(1)
	st.Push(1)
	st.Push(1)
	st.Push(1)

	equal(t, 0, st.MaxN(), "maxN")
	equal(t, 5, st.N(), "maxN")
}

func TestStatsMaxNAdapting(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	testCases := []struct {
		// a "stage" is a series of random values that roughly resemble a Normal
		// Distribution. Each stage has its own, randomized sigma and mu

		stageN     int     // number of values pushed during a stage
		stageCount int     // number of stages to run
		maxN       float64 // value for SetMaxN
	}{
		{stageN: 1e3, stageCount: 1e2, maxN: 512},
		{stageN: 1e4, stageCount: 1e2, maxN: 512},
		{stageN: 1e5, stageCount: 1e2, maxN: 512},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("[%d] stageN=%v; stageCount=%v; maxN=%v", i,
			tc.stageN, tc.stageCount, tc.maxN),
			func(t *testing.T) {
				t.Parallel()
				testAdaptiveMaxN(t, tc.stageN, tc.stageCount, tc.maxN)
			},
		)
	}
}

func testAdaptiveMaxN(t *testing.T, stageN, stageCount int, maxN float64) {
	var withMax, withoutMax Stats
	withMax.SetMaxN(maxN)
	if got := withMax.MaxN(); maxN != got {
		t.Fatalf("expectet %v, got %v", maxN, got)
	}

	withMaxRelErr := make(muSigmas, 0, stageN*stageCount)
	withoutMaxRelErr := make(muSigmas, 0, stageN*stageCount)

	for i := 0; i < stageCount; i++ {
		mu, sigma := rand.Float64(), rand.Float64()
		for j := 0; j < stageN; j++ {
			value := math.FMA(rand.NormFloat64(), sigma, mu)

			withMax.Push(value)
			withMaxRelErr = append(withMaxRelErr, muSigma{
				mu:    relErrPerc(mu, withMax.Mean()),
				sigma: relErrPerc(sigma, withMax.StdDev()),
			})

			withoutMax.Push(value)
			withoutMaxRelErr = append(withoutMaxRelErr, muSigma{
				mu:    relErrPerc(mu, withoutMax.Mean()),
				sigma: relErrPerc(sigma, withoutMax.StdDev()),
			})
		}
	}

	withMaxStatsMu := withMaxRelErr.statsMu()
	withMaxStatsStdDev := withMaxRelErr.statsStdDev()

	withoutMaxStatsMu := withoutMaxRelErr.statsMu()
	withoutMaxStatsStdDev := withoutMaxRelErr.statsStdDev()

	if withMaxStatsMu.mean > withoutMaxStatsMu.mean {
		t.Errorf("mean of percent relative error computing mean is worse "+
			"in the adaptive max. Adaptive max: %v; Non-Adaptive max: %v",
			withMaxStatsMu.mean, withoutMaxStatsMu.mean)
	}

	if withMaxStatsMu.stdDev > withoutMaxStatsMu.stdDev {
		t.Errorf("stdDev of percent relative error computing mean is worse "+
			"in the adaptive max. Adaptive max: %v; Non-Adaptive max: %v",
			withMaxStatsMu.stdDev, withoutMaxStatsMu.stdDev)
	}

	if withMaxStatsStdDev.mean > withoutMaxStatsStdDev.mean {
		t.Errorf("mean of percent relative error computing stdDev is worse "+
			"in the adaptive max. Adaptive max: %v; Non-Adaptive max: %v",
			withMaxStatsStdDev.mean, withoutMaxStatsStdDev.mean)
	}

	if withMaxStatsStdDev.stdDev > withoutMaxStatsStdDev.stdDev {
		t.Errorf("stdDev of percent relative error computing stdDev is worse "+
			"in the adaptive max. Adaptive max: %v; Non-Adaptive max: %v",
			withMaxStatsStdDev.stdDev, withoutMaxStatsStdDev.stdDev)
	}
}

type muSigma struct {
	mu, sigma float64
}

type muSigmaStats struct {
	mean, stdDev float64
}

type muSigmas []muSigma

func (ms muSigmas) stats(getVal func(muSigma) float64) muSigmaStats {
	slices.SortFunc(ms, func(a, b muSigma) int {
		return cmp.Compare(getVal(a), getVal(b))
	})
	var sum float64
	for i := range ms {
		if v := getVal(ms[i]); !math.IsNaN(v) {
			sum += v
		}
	}
	mean := sum / float64(len(ms))

	diff := make([]float64, 0, len(ms))
	for i := range ms {
		if v := getVal(ms[i]); !math.IsNaN(v) {
			diff = append(diff, math.Pow(mean-v, 2))
		}
	}
	slices.Sort(diff)

	var stdDev float64
	for _, v := range diff {
		stdDev += v
	}
	stdDev = math.Sqrt(stdDev / float64(len(diff)))

	return muSigmaStats{
		mean:   mean,
		stdDev: stdDev,
	}
}

func (ms muSigmas) statsMu() muSigmaStats {
	return ms.stats(func(ms muSigma) float64 { return ms.mu })
}

func (ms muSigmas) statsStdDev() muSigmaStats {
	return ms.stats(func(ms muSigma) float64 { return ms.sigma })
}
