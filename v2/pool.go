package zlog

import (
	"fmt"
	"io"
	"reflect"
	"sync"
)

// Pooled buffers, modeled on the way the slog.JSONHandler does it.

// BufPool is the global pool of buffers.
var bufPool = sync.Pool{
	New: func() any {
		n := make([]byte, 0, 1024)
		if len(n) != 0 {
			panic("WTF")
		}
		return (*buffer)(&n)
	},
}

// Buffer is a byte buffer implemented over a slice.
//
// Implementing it this way makes all the helper functions methods instead of
// just free functions.
type buffer []byte

// NewBuffer returns a buffer from the global pool, allocating if necessary.
func newBuffer() *buffer {
	return bufPool.Get().(*buffer)
}

// Custom methods first:

// Release returns modestly sized buffers back to the [bufPool] and leaks large
// buffers.
//
// As a convenience, this may be called on a nil receiver.
func (b *buffer) Release() {
	const maxSz = 16 << 10
	if b == nil {
		return
	}
	if cap(*b) <= maxSz {
		*b = (*b)[:0]
		bufPool.Put(b)
	}
}

// Tail reports the last-written byte, like an backwards [io.ByteReader].
func (b *buffer) Tail() byte {
	return (*b)[len(*b)-1]
}

// Unwrite slices off the last-written byte.
func (b *buffer) Unwrite() {
	*b = (*b)[:len(*b)-1]
}

// ReplaceTail replaces the last-written byte.
//
// This method is more efficient than [Unwrite] + [WriteByte].
func (b *buffer) ReplaceTail(c byte) {
	(*b)[len(*b)-1] = c
}

// Clone returns a new buffer with the contents of the receiver.
//
// The receiver does not have Release called on it. As a convenience, this may
// be called on a nil receiver.
func (b *buffer) Clone() (out *buffer) {
	out = newBuffer()
	if b == nil {
		return out
	}
	if cap(*b) > cap(*out) {
		// Leak small buffer, this new larger will end up in the pool
		// instead.
		*out = make([]byte, len(*b), cap(*b))
	}
	if len(*b) > len(*out) {
		*out = (*out)[:len(*b)]
	}
	copy(*out, *b)
	return out
}

// Boring methods:

var (
	_ io.Writer       = (*buffer)(nil)
	_ io.WriterTo     = (*buffer)(nil)
	_ io.ByteWriter   = (*buffer)(nil)
	_ io.StringWriter = (*buffer)(nil)
)

// WriteString implements [io.StringWriter].
func (b *buffer) WriteString(s string) (int, error) {
	*b = append(*b, s...)
	return len(s), nil
}

// WriteByte implements [io.ByteWriter].
func (b *buffer) WriteByte(c byte) error {
	*b = append(*b, c)
	return nil
}

// Write implements [io.Writer].
func (b *buffer) Write(in []byte) (int, error) {
	*b = append(*b, in...)
	return len(in), nil
}

// WriteTo implements [io.WriterTo].
func (b *buffer) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(*b)
	return int64(n), err
}

// The global statePools.
var pools = map[reflect.Type]interface{}{
	reflect.TypeOf(stateJSON{}): &statePool[*stateJSON]{
		New: func() *stateJSON { return new(stateJSON) },
	},
	reflect.TypeOf(stateJournal{}): &statePool[*stateJournal]{
		New: func() *stateJournal { return new(stateJournal) },
	},
}

// GetPool returns the type-specific [statePool].
//
// GetPool panics if the type parameter is not a pointer type.
func getPool[T state]() *statePool[T] {
	var t T
	typ := reflect.TypeOf(t).Elem()
	var v interface{}
	var ok bool
	v, ok = pools[typ]
	if !ok {
		panic(fmt.Sprintf("programmer error: called with unexpected type: %T", t))
	}
	return v.(*statePool[T])
}

// StatePool is just a typed wrapper around a [sync.Pool].
type statePool[V state] struct {
	sync.Pool
	// New is a constructor for the concrete type.
	New func() V
}

// Get returns a new stateObj.
func (p *statePool[V]) Get(g []string, b *buffer) (v V) {
	if x := p.Pool.Get(); x != nil {
		v = x.(V)
	} else {
		v = p.New()
	}
	v.Reset(g, b)
	return v
}

// Put returns the stateObj to the pool.
func (p *statePool[V]) Put(v V) {
	p.Pool.Put(v)
}
