package adaptivepool

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func msgFmt(msg string, args ...any) string {
	if msg != "" {
		msg = fmt.Sprintf(msg+": ", args...)
	}
	return msg
}

func equal[T comparable](tb testing.TB, expected, got T, msg string,
	args ...any) {
	tb.Helper()
	if expected != got {
		tb.Fatalf("%sexpected %v, get %v", msgFmt(msg, args...), expected, got)
	}
}

func zero(tb testing.TB, v any) {
	tb.Helper()
	if v != nil && !reflect.ValueOf(v).IsZero() {
		tb.Fatalf("unexpected non-zero value: %v", v)
	}
}

func floatsEqual(tb testing.TB, expected, got, maxDiff float64, msg string,
	args ...any) {
	if diff := math.Abs(expected - got); diff > maxDiff {
		tb.Errorf("%sdiff too high; expected %v; got: %v; diff: %v; maxDiff: %v",
			msgFmt(msg, args...), expected, got, diff, maxDiff)
	}
}

func parseFloats(ss []string) ([]float64, error) {
	ret := make([]float64, 0, len(ss))
	for i, s := range ss {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %d-eth float: %w", i, err)
		}
		ret = append(ret, f)
	}
	return ret, nil
}
