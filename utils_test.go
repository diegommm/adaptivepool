package adaptivepool

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"testing"
)

//go:embed stats_test_data.csv.bz2
var statsTestData []byte

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

func zero(tb testing.TB, v any, msg string, args ...any) {
	tb.Helper()
	if v != nil && !reflect.ValueOf(v).IsZero() {
		tb.Fatalf("%sunexpected non-zero value: %v", msgFmt(msg, args...), v)
	}
}

func parseFloats(ss []string, ret []float64) error {
	for i, s := range ss {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("parse %d-eth float: %w", i, err)
		}
		ret[i] = f
	}
	return nil
}

func csvTestDataReader(tb testing.TB) *csv.Reader {
	tb.Helper()
	r := bufio.NewReader(bzip2.NewReader(bytes.NewReader(statsTestData)))

	// discard the CSV header
	for {
		_, isPrefix, err := r.ReadLine()
		zero(tb, err, "ReadLine from test data")
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

func allTestDataInputValues(tb testing.TB) []float64 {
	tb.Helper()

	ret := make([]float64, 0, 10_000)

	cr := csvTestDataReader(tb)
	for i := 1; ; i++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		zero(tb, err, "read CSV record #%d", i)

		f, err := strconv.ParseFloat(rec[0], 64)
		zero(tb, err, "parse first float value from record #%d", i)

		ret = append(ret, f)
	}

	return ret
}
