// Package aap implements the phone (mobile device) side of the Android Auto
// Protocol: framing, the TLS layer, service discovery and the video channel.
//
// The message layouts were written from the public protocol descriptions used
// by open head unit projects; no code was copied from them.
package aap

import (
	"encoding/binary"
	"errors"
	"math"
)

// The few protobuf messages the phone side needs are encoded by hand so the
// service does not need generated code.

var errTruncated = errors.New("aap: truncated protobuf")

type pbWriter struct{ b []byte }

func (w *pbWriter) tag(field, wire int) { w.b = binary.AppendUvarint(w.b, uint64(field<<3|wire)) }

func (w *pbWriter) uint(field int, v uint64) {
	w.tag(field, 0)
	w.b = binary.AppendUvarint(w.b, v)
}

// int writes a proto int32/int64 (negative values use ten bytes, as protobuf does).
func (w *pbWriter) int(field int, v int64) { w.uint(field, uint64(v)) }

func (w *pbWriter) bool(field int, v bool) {
	if v {
		w.uint(field, 1)
	} else {
		w.uint(field, 0)
	}
}

func (w *pbWriter) bytes(field int, v []byte) {
	w.tag(field, 2)
	w.b = binary.AppendUvarint(w.b, uint64(len(v)))
	w.b = append(w.b, v...)
}

func (w *pbWriter) string(field int, v string) { w.bytes(field, []byte(v)) }

// pbField is one decoded field. Varint values are in V; length-delimited
// values in B.
type pbField struct {
	Num  int
	Wire int
	V    uint64
	B    []byte
}

func pbParse(b []byte) ([]pbField, error) {
	var out []pbField
	for len(b) > 0 {
		k, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errTruncated
		}
		b = b[n:]
		f := pbField{Num: int(k >> 3), Wire: int(k & 7)}
		switch f.Wire {
		case 0:
			f.V, n = binary.Uvarint(b)
			if n <= 0 {
				return nil, errTruncated
			}
			b = b[n:]
		case 1:
			if len(b) < 8 {
				return nil, errTruncated
			}
			f.V = binary.LittleEndian.Uint64(b)
			b = b[8:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || l > uint64(len(b)-n) {
				return nil, errTruncated
			}
			f.B = b[n : n+int(l)]
			b = b[n+int(l):]
		case 5:
			if len(b) < 4 {
				return nil, errTruncated
			}
			f.V = uint64(binary.LittleEndian.Uint32(b))
			b = b[4:]
		default:
			return nil, errors.New("aap: unsupported protobuf wire type")
		}
		out = append(out, f)
	}
	return out, nil
}

// pbMessage gives keyed access to a parsed message.
type pbMessage []pbField

func pbDecode(b []byte) (pbMessage, error) {
	f, e := pbParse(b)
	return pbMessage(f), e
}

func (m pbMessage) uint(num int) (uint64, bool) {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].Num == num && m[i].Wire != 2 {
			return m[i].V, true
		}
	}
	return 0, false
}

func (m pbMessage) int32(num int) (int32, bool) {
	v, ok := m.uint(num)
	if !ok {
		return 0, false
	}
	return int32(int64(v)), true
}

func (m pbMessage) str(num int) string {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].Num == num && m[i].Wire == 2 {
			return string(m[i].B)
		}
	}
	return ""
}

func (m pbMessage) all(num int) [][]byte {
	var out [][]byte
	for _, f := range m {
		if f.Num == num && f.Wire == 2 {
			out = append(out, f.B)
		}
	}
	return out
}

func (m pbMessage) sub(num int) (pbMessage, bool) {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].Num == num && m[i].Wire == 2 {
			s, e := pbDecode(m[i].B)
			return s, e == nil
		}
	}
	return nil, false
}

func clampU32(v uint64) uint32 {
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}
