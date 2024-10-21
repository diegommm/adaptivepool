package adaptivepool

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

var (
	_ PoolItemProvider[[]byte]        = NormalSlice[byte]{}
	_ PoolItemProvider[*bytes.Buffer] = NormalBytesBuffer{}
)

func TestAdaptivePool(t *testing.T) {
	t.Parallel()

	t.Run("ramping sizes", func(t *testing.T) {
		t.Parallel()
		const thresh = 1
		v := func(n int) []int {
			return make([]int, n)
		}
		capv := func(v []int) float64 {
			return float64(cap(v))
		}

		x := newAdaptivePoolAsserter(t, NormalSlice[int]{
			Threshold: thresh,
		}, capv)
		x.assertStats(0, 0, math.NaN())
		x.assertPut(nil, true) // should be a nop
		x.assertStats(0, 0, math.NaN())
		x.assertGet(0)
		x.assertGet(0)            // should not change capacity
		x.assertPut(v(10), false) // n=1 ; mean=10   ; stdDev=NaN
		x.assertStats(1, 10, math.NaN())
		x.assertGet(10)
		x.assertGet(10)           // should not change capacity
		x.assertPut(v(10), false) // n=2 ; mean=10   ; stdDev=0
		x.assertPut(v(10), false) // n=3 ; mean=10   ; stdDev=0
		x.assertPut(v(20), true)  // n=4 ; mean=12.5 ; stdDev=4.3
		x.assertPut(v(20), true)  // n=5 ; mean=14   ; stdDev=4.8
		x.assertPut(v(20), false) // n=6 ; mean=15   ; stdDev=5
		x.assertGet(20)
		x.assertGet(20)          // should not change capacity
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
		x.assertGet(48) // should not change capacity

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
		capv := func(v *bytes.Buffer) float64 {
			return float64(v.Cap())
		}

		x := newAdaptivePoolAsserter(t, NormalBytesBuffer{
			Threshold: thresh,
		}, capv)
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
		expectedSize := normalCreateSize(values[1], sd, thresh)
		x.assertGet(float64(int(expectedSize)))

		x.ap.Put(nil) // should not panic
	})
}

type adaptivePoolAsserter[T any] struct {
	t        *testing.T
	pool     *testPool
	provider PoolItemProvider[T]
	capv     func(T) float64
	ap       *AdaptivePool[T]
}

func newAdaptivePoolAsserter[T any](
	t *testing.T,
	p PoolItemProvider[T],
	capv func(T) float64,
) adaptivePoolAsserter[T] {
	pool := new(testPool)
	ap := New[T](p, 0)
	ap.pool = pool
	pool.New = ap.new
	return adaptivePoolAsserter[T]{
		t:        t,
		pool:     pool,
		provider: p,
		capv:     capv,
		ap:       ap,
	}
}

func (a adaptivePoolAsserter[T]) assertGet(expectedSize float64) {
	a.t.Helper()
	item := a.ap.Get()
	if s := a.provider.Sizeof(item); s > 0 {
		a.t.Fatalf("created items should have non-positive size, got %v", s)
	}
	got := a.capv(item)
	if got != expectedSize {
		a.t.Fatalf("expected item with size %v, got %v",
			expectedSize, got)
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
	st := a.ap.Stats()
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

// testPool always returns a new object for Get, and counts calls to Put,
// dropping all values passed to it. Not safe for concurrent use.
type testPool struct {
	New      func() any
	putCount uint
}

func (p *testPool) Get() any  { return p.New() }
func (p *testPool) Put(x any) { p.putCount++ }

func TestNormalCreateSize(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		n, mean, stdDev, thresh, expected float64
	}{
		{0, 42, 0, 0, 42},
		{1, 42, 0, 0, 42},
		{2, 3, 5, 7, 38},
	}

	for i, tc := range testCases {
		sd := tc.stdDev
		if tc.n < 2 {
			sd = math.NaN()
		}
		got := normalCreateSize(tc.mean, sd, tc.thresh)
		if got != tc.expected {
			t.Errorf("testCase[%v] unexpected %v, got %v", i, tc.expected, got)
		}
	}
}

func TestNormalAccept(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		n, mean, stdDev, thresh, itemSize float64
		expected                          bool
	}{
		{0, 0, math.NaN(), 0, 0, true},
		{1, 0, math.NaN(), 0, 0, true},
		{2, 10, 3, 1, 0, false},
		{2, 10, 3, 1, 10, true},
		{2, 10, 3, 1, 7, true},
		{2, 10, 3, 1, 13, true},
		{2, 10, 3, 1, 6.99, false},
		{2, 10, 3, 1, 13.01, false},
	}

	for i, tc := range testCases {
		sd := tc.stdDev
		if tc.n < 2 {
			sd = math.NaN()
		}
		got := normalAccept(tc.mean, sd, tc.thresh, tc.itemSize)
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
