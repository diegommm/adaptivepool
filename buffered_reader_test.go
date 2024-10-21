package adaptivepool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"testing/iotest"
)

// testData with non-ASCII characters at the beginning to test the ReadRune
// method correctly returning UTF-8 runes.
const testData = `痛苦
	I was looking for a job and then I found a job
	And Heaven knows, I'm miserable now
`

var _ interface { // assert interfaces from standard library
	io.ReadSeekCloser
	io.ByteScanner
	io.RuneScanner
	io.WriterTo
} = (*BufferedReader)(nil)

func TestReaderBufferer(t *testing.T) {
	t.Parallel()
	errTest := errors.New("hated because of great qualities")
	errTest2 := errors.New("loved despite of great faults")

	t.Run("Reader: happy path - empty", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		br, err := brr.Reader(bytes.NewReader(nil))
		zero(t, err, "Reader error on empty io.Reader")
		equal(t, true, br != nil, "nil Reader")

		zero(t, iotest.TestReader(br, nil),
			"iotest.TestReader error on non-closed *BufferedReader")
		// first call Bytes on br so we later don't put it back into the pool.
		// This is because if Bytes is called, then the buffer no longer belongs
		// to us
		finishAndTestBufferedReader(t, br, false)

		st := brr.Stats()
		zero(t, st.N(), "should not have been put back into the pool")
	})

	t.Run("ReadCloser: happy path - non-empty", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		rc := io.NopCloser(bytes.NewReader([]byte(testData)))
		br, err := brr.ReadCloser(rc)
		zero(t, err, "Reader error on non-empty io.Reader")
		equal(t, true, br != nil, "nil Reader")

		zero(t, iotest.TestReader(br, []byte(testData)),
			"iotest.TestReader error on non-closed *BufferedReader")
		finishAndTestBufferedReader(t, br, true)

		st := brr.Stats()
		equal(t, 1, st.N(), "should have been put back into the pool")
	})

	t.Run("Reader: fail reading", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		br, err := brr.Reader(iotest.ErrReader(errTest))
		equal(t, true, errors.Is(err, errTest), "should have failed reading")
		zero(t, br, "should return nil on error")
	})

	t.Run("ReadCloser: fail reading", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		rc := io.NopCloser(iotest.ErrReader(errTest))
		br, err := brr.ReadCloser(rc)
		equal(t, true, errors.Is(err, errTest), "should have failed reading")
		zero(t, br, "should return nil on error")
	})

	t.Run("ReadCloser: fail closing", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		rc := readCloser{
			Reader: bytes.NewReader(nil),
			Closer: closerFunc(func() error { return errTest }),
		}
		br, err := brr.ReadCloser(rc)
		equal(t, true, errors.Is(err, errTest), "should have failed closing")
		zero(t, br, "should return nil on error")
	})

	t.Run("ReadCloser: fail reading and closing", func(t *testing.T) {
		t.Parallel()
		brr := NewReaderBufferer(512, 2, 500)

		rc := readCloser{
			Reader: iotest.ErrReader(errTest),
			Closer: closerFunc(func() error { return errTest2 }),
		}
		br, err := brr.ReadCloser(rc)
		equal(t, true, errors.Is(err, errTest), "should have failed reading")
		equal(t, true, errors.Is(err, errTest2), "should have failed closing")
		zero(t, br, "should return nil on error")
	})
}

type readCloser struct {
	io.Reader
	io.Closer
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func newTestBufferedReader(buf []byte) *BufferedReader {
	return &BufferedReader{
		reader:  bytes.NewReader(buf),
		buf:     buf,
		release: func([]byte, *bytes.Reader) {},
	}
}

func TestBufferedReader(t *testing.T) {
	t.Parallel()

	t.Run("without data - Close then Bytes", func(t *testing.T) {
		t.Parallel()
		br := newTestBufferedReader(nil)
		zero(t, iotest.TestReader(br, nil),
			"iotest.TestReader error on non-closed *BufferedReader")
		finishAndTestBufferedReader(t, br, true)
	})

	t.Run("without data - Bytes then Close", func(t *testing.T) {
		t.Parallel()
		br := newTestBufferedReader(nil)
		zero(t, iotest.TestReader(br, nil),
			"iotest.TestReader error on non-closed *BufferedReader")
		finishAndTestBufferedReader(t, br, false)
	})

	t.Run("with data - Close then Bytes", func(t *testing.T) {
		t.Parallel()
		br := newTestBufferedReader([]byte(testData))
		zero(t, iotest.TestReader(br, []byte(testData)),
			"iotest.TestReader error on non-closed *BufferedReader")
		finishAndTestBufferedReader(t, br, true)
	})

	t.Run("with data - Bytes then Close", func(t *testing.T) {
		t.Parallel()
		br := newTestBufferedReader([]byte(testData))
		zero(t, iotest.TestReader(br, []byte(testData)),
			"iotest.TestReader error on non-closed *BufferedReader")
		finishAndTestBufferedReader(t, br, false)
	})
}

func finishAndTestBufferedReader(t *testing.T, br *BufferedReader,
	// true: call Close first, then Bytes; false: call Bytes first, then Close
	closeFirst bool,
) {
	finishAndTestBufferedReaderInternal(t, br, closeFirst, true)
}

func finishAndTestBufferedReaderInternal(t *testing.T, br *BufferedReader,
	closeFirst bool, runTheOtherAfter bool) {
	t.Helper()

	_, err := br.Seek(0, io.SeekStart)
	zero(t, err, "Seek(0, io.SeekStart)")

	if l := br.Len(); l > 0 {
		// io.ByteScanner methods
		_, err := br.ReadByte()
		zero(t, err, "ReadByte on non-empty *BufferedReader")

		gotL := br.Len()
		equal(t, l-1, gotL, "should have one less byte; want: %d, got: %d", l-1,
			gotL)

		err = br.UnreadByte()
		zero(t, err, "UnreadByte after ReadByte")

		gotL = br.Len()
		equal(t, l, gotL, "should have the same original length; want: %d, "+
			"got: %d", l-1, gotL)

		// io.RuneScanner methods
		_, s, err := br.ReadRune()
		zero(t, err, "ReadRune on non-empty *BufferedReader")
		if s < 2 {
			t.Fatalf("unexpected rune size %d from non-empty *BufferedReader "+
				"(remember to use test data starting with non-ASCII, wide "+
				"characters", s)
		}

		gotL = br.Len()
		wantL := l - s
		equal(t, wantL, gotL, "should have %d less byte(s); want: %d, got: %d",
			s, wantL, gotL)

		err = br.UnreadRune()
		zero(t, err, "UnreadRune after ReadRune")

		gotL = br.Len()
		equal(t, l, gotL, "should have the same original length; want: %d, "+
			"got: %d", l-1, gotL)
	}

	if closeFirst {
		zero(t, br.Close(), "close *BufferedReader")
		zero(t, br.Close(), "close *BufferedReader for the second time")

	} else {
		_, err := br.Seek(0, io.SeekStart)
		zero(t, err, "Seek(0, io.SeekStart)")
		l := br.Len()

		buf := new(bytes.Buffer)
		gotL, err := br.WriteTo(buf)
		zero(t, err, "WriteTo")
		equal(t, int64(l), gotL, "unexpected read buf with length %d, "+
			"expected length %d; data: %q", gotL, l, buf.String())

		wantBuf := buf.Bytes()
		gotBuf := br.Bytes()
		equal(t, true, slices.Equal(wantBuf, gotBuf), "should return same "+
			"data\n\twant: %q\n\tgot: %q", wantBuf, gotBuf)

		if !runTheOtherAfter {
			zero(t, len(wantBuf), "no data should be read after closing, "+
				"got: %q", wantBuf)
			zero(t, len(gotBuf), "no data should be returned after closing, "+
				"got: %q", gotBuf)
		}

	}

	zero(t, br.Len(), "should have length zero after closing")
	zero(t, iotest.TestReader(br, nil),
		"iotest.TestReader error on closed *BufferedReader")

	// as a closed *BufferedReader will no longer use its internal
	// *bytes.Reader, we will test that once closed it behaves the same as an
	// empty one, hence we compare all its ouputs. When comparing errors, we
	// will prioritize detecting io.EOF, otherwise just make sure they both err
	// under the same conditions

	isEOF := func(err error) string {
		if errors.Is(err, io.EOF) {
			return "yes"
		}
		return fmt.Sprintf("no, it is %v", err)
	}

	compareErrs := func(want, got error) error {
		if (want == nil) != (got == nil) {
			return fmt.Errorf("want error: %w; got error: %w", want, got)
		}
		if errors.Is(want, io.EOF) != errors.Is(got, io.EOF) {
			return fmt.Errorf("disagree on io.EOF; want error is io.EOF: %v; "+
				"got error is io.EOF: %v", isEOF(want), isEOF(got))
		}
		return nil
	}

	emptyBytesReader := bytes.NewReader(nil)

	wantInt, wantErr := emptyBytesReader.Read(make([]byte, 10))
	gotInt, gotErr := br.Read(make([]byte, 10))
	zero(t, compareErrs(wantErr, gotErr), "disagree on Read error after close")
	equal(t, wantInt, gotInt, "disagree on Read int after close")

	wantInt64, wantErr := emptyBytesReader.Seek(0, io.SeekStart)
	gotInt64, gotErr := br.Seek(0, io.SeekStart)
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on Seek(0, io.SeekStart) error after close")
	equal(t, wantInt64, gotInt64,
		"disagree on Seek(0, io.SeekStart) int64 after close")

	wantInt64, wantErr = emptyBytesReader.Seek(-1, io.SeekStart)
	gotInt64, gotErr = br.Seek(-1, io.SeekStart)
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on Seek(-1, io.SeekStart) error after close")
	equal(t, wantInt64, gotInt64,
		"disagree on Seek(-1, io.SeekStart) int64 after close")

	wantInt64, wantErr = emptyBytesReader.Seek(0, -1)
	gotInt64, gotErr = br.Seek(0, -1)
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on Seek(0, -1) error after close")
	equal(t, wantInt64, gotInt64,
		"disagree on Seek(0, -1) int64 after close")

	wantByte, wantErr := emptyBytesReader.ReadByte()
	gotByte, gotErr := br.ReadByte()
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on ReadByte error after close")
	equal(t, wantByte, gotByte,
		"disagree on ReadByte byte after close")

	wantErr = emptyBytesReader.UnreadByte()
	gotErr = br.UnreadByte()
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on UnreadByte error after close")

	wantRune, wantInt, wantErr := emptyBytesReader.ReadRune()
	gotRune, gotInt, gotErr := br.ReadRune()
	zero(t, compareErrs(wantErr, gotErr), "disagree on ReadRune error after close")
	equal(t, wantInt, gotInt, "disagree on ReadRune int after close")
	equal(t, wantRune, gotRune, "disagree on ReadRune rune after close")

	wantErr = emptyBytesReader.UnreadRune()
	gotErr = br.UnreadRune()
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on UnreadRune error after close")

	wantInt64, wantErr = emptyBytesReader.WriteTo(io.Discard)
	gotInt64, gotErr = br.WriteTo(io.Discard)
	zero(t, compareErrs(wantErr, gotErr),
		"disagree on WriteTo error after close")
	equal(t, wantInt64, gotInt64,
		"disagree on WriteTo int64 after close")

	if runTheOtherAfter {
		finishAndTestBufferedReaderInternal(t, br, !closeFirst, false)
	}
}
