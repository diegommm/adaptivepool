package adaptivepool

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

var (
	_ ItemProvider[[]byte]        = SliceProvider[byte]{}
	_ ItemProvider[*bytes.Buffer] = BytesBufferProvider{}
)

func (p *AdaptivePool[T]) getStats() Stats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

func TestAdaptivePool(t *testing.T) {
	t.Parallel()

	t.Run("ramping costs", func(t *testing.T) {
		t.Parallel()
		const thresh = 1
		v := func(n int) []byte {
			return make([]byte, n)
		}
		lenv := func(v []byte) int {
			return len(v)
		}

		x := newAdaptivePoolAsserter(t, SliceProvider[byte]{},
			NormalEstimator{thresh, 0}, lenv)
		x.assertStats(0, 0, math.NaN())
		x.assertPut(nil, true) // should be a nop
		x.assertStats(0, 0, math.NaN())
		x.assertGet(0)
		x.assertGet(0)            // should not change cost
		x.assertPut(v(10), false) // n=1 ; mean=10   ; stdDev=NaN
		x.assertStats(1, 10, math.NaN())
		x.assertGet(10)
		x.assertGet(10)           // should not change cost
		x.assertPut(v(10), false) // n=2 ; mean=10   ; stdDev=0
		x.assertPut(v(10), false) // n=3 ; mean=10   ; stdDev=0
		x.assertPut(v(20), true)  // n=4 ; mean=12.5 ; stdDev=4.3
		x.assertPut(v(20), true)  // n=5 ; mean=14   ; stdDev=4.8
		x.assertPut(v(20), false) // n=6 ; mean=15   ; stdDev=5
		x.assertGet(20)
		x.assertGet(20)          // should not change cost
		x.assertPut(v(30), true) // n=7 ; mean=17.1 ; stdDev=6.9
		x.assertPut(v(30), true) // n=8 ; mean=18.7 ; stdDev=7.8
		x.assertPut(v(30), true) // n=9 ; mean=20   ; stdDev=8.1
		x.assertPut(v(50), true) // n=10; mean=23   ; stdDev=11.8
		x.assertPut(v(50), true) // n=11; mean=25.4 ; stdDev=13.7
		x.assertPut(v(50), true) // n=12; mean=27.5 ; stdDev=14.7
		x.assertPut(v(50), true) // n=13; mean=29.3 ; stdDev=15.4
		x.assertPut(v(50), true) // n=14; mean=30.7 ; stdDev=15.7
		x.assertPut(v(50), true) // n=15; mean=32   ; stdDev=16
		x.assertGet(48)
		x.assertGet(48) // should not change cost

		// we have added enough cherry-picked values so that stats will likely
		// be very precise already, even if the Stats implementation is not very
		// good
		x.assertStats(15, 32, 16)
	})

	t.Run("test data from file", func(t *testing.T) {
		t.Parallel()
		const thresh = 2.5
		v := func(n int) *bytes.Buffer {
			return bytes.NewBuffer(make([]byte, n))
		}
		lenv := func(v *bytes.Buffer) int {
			return v.Len()
		}

		x := newAdaptivePoolAsserter(t, BytesBufferProvider{},
			NormalEstimator{thresh, 0}, lenv)
		x.assertStats(0, 0, math.NaN())
		x.assertPut(nil, true) // should be a nop
		x.assertStats(0, 0, math.NaN())
		x.assertGet(0)

		values := make([]float64, 3)
		cr := csvTestDataReader(t)
		var i float64
		for {
			rec, err := cr.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
			zero(t, err, "read CSV record #%d", i)
			equal(t, 3, len(rec), "number of CSV values in record #%d", i)

			err = parseFloats(rec, values)
			zero(t, err, "parse floats from CSV record #%d; record: %v", i, rec)

			x.ap.Put(v(int(values[0])))
		}
		x.assertStats(i, values[1], values[2])
		sd := values[2]
		if i < 2 {
			sd = math.NaN()
		}
		st := EstimatorStats{
			Mean:   values[1],
			StdDev: sd,
		}
		expectedCost := x.es.Suggest(st)
		x.assertGet(expectedCost)

		x.ap.Put(nil) // should not panic
	})

	t.Run("different get", func(t *testing.T) {
		t.Parallel()
		const ct = 10
		pr := &testProvider[[]byte]{
			ItemProvider: SliceProvider[byte]{},
		}
		pl := &testPool{
			pool: new(slicePool),
		}
		ap := New(pr, NormalEstimator{2, 0}, 500)
		ap.pool = pl

		plGet := pl.getCount
		prNew := pr.newCount
		ap.Get()
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew+1, pr.newCount, "should have tried ItemProvider")

		plGet = pl.getCount
		prNew = pr.newCount
		ap.GetWithCost(ct)
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew+1, pr.newCount, "should have tried ItemProvider")

		plGet = pl.getCount
		prNew = pr.newCount
		ap.GetWithCost(0)
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew+1, pr.newCount, "should have tried ItemProvider")

		ap.Put(pr.ItemProvider.New(ct)) // right from the hose
		plGet = pl.getCount
		prNew = pr.newCount
		ap.Get()
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew, pr.newCount, "should not have tried ItemProvider")

		ap.Put(pr.ItemProvider.New(ct)) // right from the hose
		plGet = pl.getCount
		prNew = pr.newCount
		ap.GetWithCost(ct)
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew, pr.newCount, "should not have tried ItemProvider")

		ap.Put(pr.ItemProvider.New(ct)) // right from the hose
		plGet = pl.getCount
		prNew = pr.newCount
		ap.GetWithCost(ct * ct * ct)
		equal(t, plGet+1, pl.getCount, "should have tried from the pool")
		equal(t, prNew+1, pr.newCount, "should have tried ItemProvider")
	})
}

type adaptivePoolAsserter[T any] struct {
	t        *testing.T
	pool     *testPool
	provider ItemProvider[T]
	lenv     func(T) int
	ap       *AdaptivePool[T]
	es       Estimator
}

func newAdaptivePoolAsserter[T any](
	t *testing.T,
	p ItemProvider[T],
	e Estimator,
	lenv func(T) int,
) adaptivePoolAsserter[T] {
	pool := new(testPool)
	ap := New[T](p, e, 0)
	ap.pool = pool
	return adaptivePoolAsserter[T]{
		t:        t,
		pool:     pool,
		provider: p,
		lenv:     lenv,
		ap:       ap,
		es:       e,
	}
}

func (a adaptivePoolAsserter[T]) assertGet(expectCost int) {
	a.t.Helper()
	item := a.ap.Get()
	if gotLen := a.lenv(item); gotLen != 0 {
		a.t.Fatalf("expected item with length zero, got %v", gotLen)
	}
	if gotCt := a.provider.Costof(item); gotCt != expectCost {
		a.t.Fatalf("expected item with capacity %v, got %v", expectCost, gotCt)
	}
}

func (a adaptivePoolAsserter[T]) assertPut(v T, expectDropped bool) {
	a.t.Helper()
	curCount := a.pool.putCount
	a.ap.Put(v)
	wasDropped := curCount == a.pool.putCount
	if wasDropped != expectDropped {
		var expectedStr string
		if !expectDropped {
			expectedStr = "not "
		}
		a.t.Fatalf("item put was %vexpected to be dropped", expectedStr)
	}
}

func (a adaptivePoolAsserter[T]) assertStats(n, mean, stdDev float64) {
	// NOTE: numbers are round to 1 decimal to simplify tests
	a.t.Helper()
	st := a.ap.getStats()
	gotN, gotMean, gotStdDev := st.N(), st.Mean(), st.StdDev()
	mean, stdDev = roundOneDecimal(mean), roundOneDecimal(stdDev)
	gotMean, gotStdDev = roundOneDecimal(gotMean), roundOneDecimal(gotStdDev)
	if n != gotN || mean != gotMean ||
		math.IsNaN(stdDev) != math.IsNaN(gotStdDev) ||
		(!math.IsNaN(stdDev) && stdDev != gotStdDev) {
		a.t.Fatalf("expected stats (n, mean, stdDev): (%v, %v, %v), "+
			"got: (%v, %v, %v)", n, mean, stdDev, gotN, gotMean, gotStdDev)
	}
}

func roundOneDecimal(v float64) float64 {
	return math.Round(v*10) / 10
}

type testProvider[T any] struct {
	ItemProvider[T]
	newCount int
}

func (p *testProvider[T]) New(prealloc int) T {
	p.newCount++
	return p.ItemProvider.New(prealloc)
}

type slicePool []any

func (p *slicePool) Get() any {
	if len(*p) > 0 {
		ret := (*p)[len(*p)-1]
		*p = (*p)[:len(*p)-1]
		return ret
	}
	return nil
}

func (p *slicePool) Put(v any) {
	*p = append(*p, v)
}

type testPool struct {
	pool
	getCount int
	putCount int
}

func (p *testPool) Get() any {
	p.getCount++
	if p.pool != nil {
		return p.pool.Get()
	}
	return nil
}

func (p *testPool) Put(x any) {
	p.putCount++
	if p.pool != nil {
		p.pool.Put(x)
	}
}

func TestNormalEstimator_Suggest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		mean, stdDev, thresh float64
		expected             int
	}{
		{42, math.NaN(), 0, 42},
		{42, math.NaN(), 0, 42},
		{3, 5, 7, 38},
	}

	for i, tc := range testCases {
		st := EstimatorStats{
			Mean:   tc.mean,
			StdDev: tc.stdDev,
		}
		got := NormalEstimator{tc.thresh, 0}.Suggest(st)
		if got != tc.expected {
			t.Errorf("testCase[%v] unexpected %v, got %v", i, tc.expected, got)
		}
		newGot := NormalEstimator{tc.thresh, got + 1}.Suggest(st)
		if newGot != got+1 {
			t.Errorf("testCase[%v] min cost is %v, got %v", i, got+1, newGot)
		}
	}
}

func TestNormalEstimator_Accept(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		mean, stdDev, thresh float64
		itemCost             int
		expected             bool
	}{
		{0, math.NaN(), 0, 0, true},
		{0, math.NaN(), 0, 0, true},
		{10, 3, 1, 0, false},
		{10, 3, 1, 10, true},
		{10, 3, 1, 7, true},
		{10, 3, 1, 13, true},
		{10, 3, 1, 6, false},
		{10, 3, 1, 14, false},
	}

	for i, tc := range testCases {
		st := EstimatorStats{
			Mean:   tc.mean,
			StdDev: tc.stdDev,
		}
		got := NormalEstimator{tc.thresh, 0}.Accept(st, tc.itemCost)
		if got != tc.expected {
			t.Errorf("testCase[%v] unexpected %v", i, got)
		}
	}
}

func TestEncoding(t *testing.T) {
	t.Parallel()
	testCases := []uint64{
		0, 1<<64 - 1, (1<<32 - 1) << 16, 42<<32 + 42, 1, 1 << 32,
	}
	for i, tc := range testCases {
		got := encodeBits(decodeBits(tc))
		if got != tc {
			t.Errorf("[#%d] got %d, want %d", i, got, tc)
		}
	}
}
