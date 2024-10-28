package adaptivepool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ReaderBufferer buffers data from [io.Reader]s and [io.ReadCloser]s into
// [BufferedReader]s that, upon calling their `Close` method, will put the data
// back into an [AdaptivePool] for reuse.
type ReaderBufferer struct {
	bufPool AdaptivePool[[]byte]
	rdPool  sync.Pool
}

// NewReaderBufferer returns a new ReaderBufferer. Example:
//
//	rb := NewReaderBufferer(NormalEstimator{2}, 500)
func NewReaderBufferer(e Estimator, maxN float64) *ReaderBufferer {
	return new(ReaderBufferer).init(e, maxN)
}

func (p *ReaderBufferer) init(e Estimator, maxN float64) *ReaderBufferer {
	p.rdPool.New = newBytesReader
	p.bufPool.init(SliceProvider[byte]{}, e, maxN)
	return p
}

func newBytesReader() any {
	return bytes.NewReader(nil)
}

// Reader buffers the contents of the given io.Reader in a BufferedReader.
func (p *ReaderBufferer) Reader(r io.Reader) (*BufferedReader, error) {
	return p.buf(r, nil, 0)
}

// ReaderWithCost buffers the contents of the given io.Reader in a
// BufferedReader. If `ct` is positive, then `ct` bytes will be pre-allocated,
// otherwise an estimation will be used based on the past observed values.
func (p *ReaderBufferer) ReaderWithCost(r io.Reader,
	ct int) (*BufferedReader, error) {
	return p.buf(r, nil, ct)
}

// ReadCloser buffers the contents of the given io.ReadCloser in a
// BufferedReader. It always calls the argument's `Close` method, and it fails
// if it returns an error.
func (p *ReaderBufferer) ReadCloser(rc io.ReadCloser) (*BufferedReader, error) {
	return p.buf(rc, rc, 0)
}

// ReadCloserWithCost buffers the contents of the given io.ReadCloser in a
// BufferedReader. If `ct` is positive, then `ct` bytes will be pre-allocated,
// otherwise an estimation will be used based on the past observed values. It
// always calls the argument's `Close` method, and it fails if it returns an
// error.
func (p *ReaderBufferer) ReadCloserWithCost(rc io.ReadCloser,
	ct int) (*BufferedReader, error) {
	return p.buf(rc, rc, ct)
}

func (p *ReaderBufferer) buf(r io.Reader,
	c io.Closer, ct int) (*BufferedReader, error) {
	buf := p.getBuf(ct)
	bytesBuf := bytes.NewBuffer(buf[:0])
	n, readErr := bytesBuf.ReadFrom(r)
	if readErr != nil && c == nil {
		p.bufPool.Put(buf)
		return nil, fmt.Errorf("buffer io.Reader: %w; bytes read: %v", readErr,
			n)
	}
	buf = bytesBuf.Bytes() // reslices up to what was read

	if c != nil {
		closeErr := c.Close()
		if readErr != nil || closeErr != nil {
			p.bufPool.Put(buf)
			return nil, fmt.Errorf("buffer io.ReadCloser: read error: %w; "+
				"close error: %w; bytes read: %v", readErr, closeErr, n)
		}
	}

	rd := p.rdPool.Get().(*bytes.Reader)
	rd.Reset(buf)

	return &BufferedReader{
		reader:  rd,
		buf:     buf,
		release: p.release,
	}, nil
}

func (p *ReaderBufferer) getBuf(ct int) []byte {
	if ct > 0 {
		return p.bufPool.GetWithCost(ct)
	}
	return p.bufPool.Get()
}

func (p *ReaderBufferer) release(buf []byte, rd *bytes.Reader) {
	rd.Reset(nil)
	p.rdPool.Put(rd)
	p.bufPool.Put(buf)
}

// NOTE: we explicitly do not want to offer io.ReaderAt in BufferedReader
// because, as per its docs, "Clients of ReadAt can execute parallel ReadAt
// calls on the same input source". This means that we should add a sync.RWMutex
// to protect the underlying implementation and make it more heavyweight in
// order to guard the parallel ReadAt operations from potential Close
// operations. Clients can still use the Seek method and then Read as a
// sequential workaround.

// BufferedReader holds a read-only buffer of the contents extracted from an
// [io.Reader] or [io.ReadCloser]. Its `Close` method releases internal buffers
// for reuse, and after that it will be empty. It is not safe for concurrent
// use.
type BufferedReader struct {
	reader  *bytes.Reader
	buf     []byte
	release func([]byte, *bytes.Reader)
}

// Bytes returns the internal buffered []byte, transferring their ownership to
// the caller. The data will not be later put back into a pool by the
// implementation, and subsequent calls to any method will behave as if `Close`
// had been called. Subsequent calls to this method return nil, the same as if
// `Close` had been called before.
func (bb *BufferedReader) Bytes() []byte {
	if bb.reader != nil {
		bb.release(nil, bb.reader)
		buf := bb.buf
		*bb = BufferedReader{}
		return buf
	}
	return nil
}

// Len returns the number of unread bytes.
func (bb *BufferedReader) Len() int {
	if bb.reader != nil {
		return bb.reader.Len()
	}
	return 0
}

// Read is part of the implementation of the io.Reader interface.
func (bb *BufferedReader) Read(p []byte) (int, error) {
	if bb.reader != nil {
		return bb.reader.Read(p)
	}
	return 0, io.EOF
}

// Close is part of the implementation of the io.Closer interface. This method
// releases the internal buffer for reuse. After this, the *BufferedReader will
// be empty. This method is idempotent and always returns a nil error.
func (bb *BufferedReader) Close() error {
	if bb.reader != nil {
		bb.release(bb.buf, bb.reader)
		*bb = BufferedReader{}
	}
	return nil
}

// Seek is part of the implementation of the io.Seeker interface.
func (bb *BufferedReader) Seek(offset int64, whence int) (int64, error) {
	if bb.reader != nil {
		return bb.reader.Seek(offset, whence)
	}

	switch whence {
	case io.SeekStart, io.SeekCurrent, io.SeekEnd:
	default:
		return 0, errors.New("BufferedReader.Seek: invalid whence")
	}
	if offset < 0 {
		return 0, errors.New("BufferedReader.Seek: negative position")
	}

	return 0, nil
}

// ReadByte is part of the implementation of the io.ByteReader interface.
func (bb *BufferedReader) ReadByte() (byte, error) {
	if bb.reader != nil {
		return bb.reader.ReadByte()
	}
	return 0, io.EOF
}

// UnreadByte is part of the implementation of the io.ByteScanner interface.
func (bb *BufferedReader) UnreadByte() error {
	if bb.reader != nil {
		return bb.reader.UnreadByte()
	}
	return errors.New("BufferedReader.UnreadByte: resource closed")
}

// ReadRune is part of the implementation of the io.RuneReader interface.
func (bb *BufferedReader) ReadRune() (r rune, cost int, err error) {
	if bb.reader != nil {
		return bb.reader.ReadRune()
	}
	return 0, 0, io.EOF
}

// UnreadRune is part of the implementation of the io.RuneScanner interface.
func (bb *BufferedReader) UnreadRune() error {
	if bb.reader != nil {
		return bb.reader.UnreadRune()
	}
	return errors.New("BufferedReader.UnreadRune: resource closed")
}

// WriteTo is part of the implementation of the io.WriterTo interface.
func (bb *BufferedReader) WriteTo(w io.Writer) (n int64, err error) {
	if bb.reader != nil {
		return bb.reader.WriteTo(w)
	}
	return 0, nil
}
