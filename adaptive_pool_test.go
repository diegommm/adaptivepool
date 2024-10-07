package adaptivepool

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
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

		x := newAdaptivePoolAsserter(t, NormalSlice[int]{thresh}, capv)
		x.assertStats(0, 0, math.NaN())
		x.assertAcquire(0)
		x.assertAcquire(0)           // should not change capacity
		x.assertRelease(v(10), true) // n=1 ; mean=10   ; stdDev=NaN
		x.assertStats(1, 10, math.NaN())
		x.assertAcquire(10)
		x.assertAcquire(10)           // should not change capacity
		x.assertRelease(v(10), false) // n=2 ; mean=10   ; stdDev=0
		x.assertRelease(v(10), false) // n=3 ; mean=10   ; stdDev=0
		x.assertRelease(v(20), true)  // n=4 ; mean=12.5 ; stdDev=4.3
		x.assertRelease(v(20), true)  // n=5 ; mean=14   ; stdDev=4.8
		x.assertRelease(v(20), false) // n=6 ; mean=15   ; stdDev=5
		x.assertAcquire(20)
		x.assertAcquire(20)          // should not change capacity
		x.assertRelease(v(30), true) // n=7 ; mean=17.1 ; stdDev=6.9
		x.assertRelease(v(30), true) // n=8 ; mean=18.7 ; stdDev=7.8
		x.assertRelease(v(30), true) // n=9 ; mean=20   ; stdDev=8.1
		x.assertRelease(v(50), true) // n=10; mean=23   ; stdDev=11.8
		x.assertRelease(v(50), true) // n=11; mean=25.4 ; stdDev=13.7
		x.assertRelease(v(50), true) // n=12; mean=27.5 ; stdDev=14.7
		x.assertRelease(v(50), true) // n=13; mean=29.3 ; stdDev=15.4
		x.assertRelease(v(50), true) // n=14; mean=30.7 ; stdDev=15.7
		x.assertRelease(v(50), true) // n=15; mean=32   ; stdDev=16
		x.assertAcquire(48)
		x.assertAcquire(48) // should not change capacity

		// we have added enough cherry-picked values so that stats will likely
		// be very precise already, even if the Stats implementation is not very
		// good
		x.assertStats(15, 32, 16)
	})

	t.Run("seeding", func(t *testing.T) {
		t.Parallel()
		const thresh = 2.5
		v := func(n int) *bytes.Buffer {
			return bytes.NewBuffer(make([]byte, n))
		}
		capv := func(v *bytes.Buffer) float64 {
			return float64(v.Cap())
		}

		x := newAdaptivePoolAsserter(t, NormalBytesBuffer{thresh}, capv)
		x.assertAcquire(0)

		values := make([]float64, 3)
		cr := csvTestDataReader(t)
		var i float64
		for {
			rec, err := cr.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
			zero(t, err)
			equal(t, 3, len(rec), "number of CSV values in record #%d", i)

			err = parseFloats(rec, values)
			zero(t, err)

			x.ap.Release(v(int(values[0])))
		}
		x.assertStats(i, values[1], values[2])
		expectedSize := normalCreateSize(i, values[1], values[2], thresh)
		x.assertAcquire(float64(int(expectedSize)))

		x.ap.Release(nil) // should not panic
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
	ap := New[T](p)
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

func (a adaptivePoolAsserter[T]) assertAcquire(expectedSize float64) {
	a.t.Helper()
	item := a.ap.Acquire()
	if s := a.provider.Sizeof(item); s != 0 {
		a.t.Fatalf("acquired items should have size zero, got %v", s)
	}
	got := a.capv(item)
	if got != expectedSize {
		a.t.Fatalf("expected to acquire item with size %v, got %v",
			expectedSize, got)
	}
}

func (a adaptivePoolAsserter[T]) assertRelease(v T, expectDropped bool) {
	a.t.Helper()
	curCount := a.pool.putCount
	a.ap.Release(v)
	wasDropped := curCount == a.pool.putCount
	if wasDropped != expectDropped {
		var expectedStr string
		if !expectDropped {
			expectedStr = "not "
		}
		a.t.Fatalf("released item was %vexpected to be dropped", expectedStr)
	}
}

func (a adaptivePoolAsserter[T]) assertStats(n, mean, stdDev float64) {
	// NOTE: numbers are round to 1 decimal to simplify tests
	a.t.Helper()
	gotN, gotMean, gotStdDev := a.ap.ReadStats()
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
		got := normalCreateSize(tc.n, tc.mean, tc.stdDev, tc.thresh)
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
		{0, 0, math.NaN(), 0, 0, false},
		{1, 0, math.NaN(), 0, 0, false},
		{2, 10, 3, 1, 0, false},
		{2, 10, 3, 1, 10, true},
		{2, 10, 3, 1, 7, true},
		{2, 10, 3, 1, 13, true},
		{2, 10, 3, 1, 6.99, false},
		{2, 10, 3, 1, 13.01, false},
	}

	for i, tc := range testCases {
		got := normalAccept(tc.n, tc.mean, tc.stdDev, tc.thresh, tc.itemSize)
		if got != tc.expected {
			t.Errorf("testCase[%v] unexpected %v", i, got)
		}
	}
}
