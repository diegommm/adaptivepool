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

// stats allows testing different implementations of Stats. There were
// originally three of them, and they can be found in the git history. The
// current implementation was known as `stats1`, and the others were named
// `stats0` and `stats2. Brief description of the three:
//
//   - stats0: was an implementation based on the Welford's online algorithm
//     taken from the wikipedia on Oct 2024. It was quite fast and relatively
//     good at keeping up with the Mean, but its StdDev implementation was
//     certainly broken. Link:
//     https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance
//   - stats1: the current implementation. Based on a C++ example code found on
//     Oct 2024 in a web page. From the three, it was the fastest to run and
//     shared the same good precision results as `stats2`. According to the web
//     page author, it is also an implementation of Welford's algorithm but with
//     a revision to reduce computing error, originally presented by Knuth's Art
//     of Computer Programming. `stats1` was later modified to account for
//     seasonal changes with `SetMaxN`, and a minor reduction on precision loss
//     added by using math.FMA. Link:
//     https://www.johndcook.com/blog/standard_deviation/
//   - stats2: also taken from the same web page as `stats1` in Oct 2024, but
//     with the ability to also provide skewness and Kurtois, which were a
//     non-goal for this project. It appeared to have the same precision and
//     adaptation as `stats1`, though significantly more verbose, and slightly
//     slower. Link:
//     https://www.johndcook.com/skewness_kurtosis.html
type stats interface {
	Push(float64)
	Reset()
	N() float64
	MaxN() float64
	SetMaxN(float64)
	Mean() float64
	StdDev() float64
}

var _ stats = new(Stats)

func TestStats(t *testing.T) {
	t.Parallel()

	// Mean appears to have great precision from the start and be constant
	const meanMaxRelErrPercExp = 12

	// Parameters for powfRelErrPerc for standard deviation. Relative error
	// starts relatively high using the current data set, but rapidly decreases:
	//	errRelPerc<30 at N=2
	//	errRelPerc<20 at N=3
	//	errRelPerc<10 at N=6
	//	errRelPerc<5  at N=11
	//	errRelPerc<1  at N=51
	//
	// NOTE: this could mean that 100 (or 500 for some comfort, if the
	// application allows it) would be a reasonable starting value for `maxN`.
	const (
		xShift = -1
		a      = 30
		b      = -0.7
		c      = 0
	)

	testStats(t, new(Stats),
		constMaxRelErrPerc(math.Pow(10, -meanMaxRelErrPercExp)),
		powfRelErrPerc(xShift, a, b, c))
}

// errTestFunc returns whether it passes.
type errTestFunc = func(n, expected, got float64) bool

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

	zero(t, st.Mean(), "Mean in zero value")
	sd := st.StdDev()
	equal(t, true, math.IsNaN(sd), "unexpected non-NaN std dev for"+
		" non-initialized stats: %v", sd)

	v := make([]float64, 3)
	for i := 1; ; i++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		zero(t, err, "read CSV record #%d", i)
		equal(t, 3, len(rec), "number of CSV values in record #%d", i)

		err = parseFloats(rec, v)
		zero(t, err, "parse floats from CSV record #%d; record: %v", i, rec)

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
	zero(t, st.Mean(), "Mean after Reset")
	sd = st.StdDev()
	equal(t, true, math.IsNaN(sd), "unexpected non-NaN std dev for"+
		" cleared stats: %v", sd)
}

func TestStatsMaxN(t *testing.T) {
	t.Parallel()

	st := new(Stats)
	zero(t, st.MaxN(), "MaxN in zero value")
	st.SetMaxN(0.1)
	zero(t, st.MaxN(), "MaxN should not change if set to number < 1")
	st.SetMaxN(-1)
	zero(t, st.MaxN(), "MaxN should not change if set to number < 1")

	st.Push(1)
	st.Push(1)
	st.Push(1)
	st.Push(1)

	st.SetMaxN(1.1)
	equal(t, 1, st.MaxN(), "maxN should be round to nearest integer")
	equal(t, 1, st.N(), "N should have been capped to maxN")

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
