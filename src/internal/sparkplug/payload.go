// Package sparkplug implements the subset of Sparkplug B (topic namespace and
// Protocol Buffers payload) that the cake factory needs. The payload encoder
// is hand-written against sparkplug_b.proto so no code generation is needed.
package sparkplug

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Sparkplug B data types used here.
const (
	TypeInt64   uint32 = 4
	TypeDouble  uint32 = 10
	TypeBoolean uint32 = 11
	TypeString  uint32 = 12
)

// Metric is one named, typed value. Value is int64, float64, bool or string.
type Metric struct {
	Name      string
	Timestamp uint64
	Datatype  uint32
	Value     any
}

// Payload is a Sparkplug B payload.
type Payload struct {
	Timestamp uint64
	Metrics   []Metric
	Seq       uint64
}

// NewMetric builds a metric and derives the datatype from the Go value.
func NewMetric(name string, v any) Metric {
	m := Metric{Name: name}
	switch x := v.(type) {
	case bool:
		m.Datatype, m.Value = TypeBoolean, x
	case int:
		m.Datatype, m.Value = TypeInt64, int64(x)
	case int64:
		m.Datatype, m.Value = TypeInt64, x
	case float64:
		m.Datatype, m.Value = TypeDouble, x
	case string:
		m.Datatype, m.Value = TypeString, x
	default:
		m.Datatype, m.Value = TypeString, fmt.Sprint(x)
	}
	return m
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendTag(b []byte, field, wire uint64) []byte {
	return appendVarint(b, field<<3|wire)
}

func appendBytes(b []byte, field uint64, data []byte) []byte {
	b = appendTag(b, field, 2)
	b = appendVarint(b, uint64(len(data)))
	return append(b, data...)
}

func (m Metric) marshal() []byte {
	var b []byte
	b = appendBytes(b, 1, []byte(m.Name))
	if m.Timestamp != 0 {
		b = appendTag(b, 3, 0)
		b = appendVarint(b, m.Timestamp)
	}
	b = appendTag(b, 4, 0)
	b = appendVarint(b, uint64(m.Datatype))
	switch v := m.Value.(type) {
	case int64:
		b = appendTag(b, 11, 0)
		b = appendVarint(b, uint64(v))
	case float64:
		b = appendTag(b, 13, 1)
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
	case bool:
		b = appendTag(b, 14, 0)
		if v {
			b = appendVarint(b, 1)
		} else {
			b = appendVarint(b, 0)
		}
	case string:
		b = appendBytes(b, 15, []byte(v))
	}
	return b
}

// Marshal encodes the payload to Protocol Buffers wire format.
func (p Payload) Marshal() []byte {
	var b []byte
	if p.Timestamp != 0 {
		b = appendTag(b, 1, 0)
		b = appendVarint(b, p.Timestamp)
	}
	for _, m := range p.Metrics {
		b = appendBytes(b, 2, m.marshal())
	}
	b = appendTag(b, 3, 0)
	b = appendVarint(b, p.Seq)
	return b
}

var errTruncated = errors.New("sparkplug: truncated payload")

type reader struct {
	b []byte
	i int
}

func (r *reader) done() bool { return r.i >= len(r.b) }

func (r *reader) varint() (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.i >= len(r.b) {
			return 0, errTruncated
		}
		c := r.b[r.i]
		r.i++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, nil
		}
	}
	return 0, errors.New("sparkplug: varint overflow")
}

func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || r.i+n > len(r.b) {
		return nil, errTruncated
	}
	s := r.b[r.i : r.i+n]
	r.i += n
	return s, nil
}

// skip consumes a field of the given wire type that we do not care about.
func (r *reader) skip(wire uint64) error {
	switch wire {
	case 0:
		_, err := r.varint()
		return err
	case 1:
		_, err := r.take(8)
		return err
	case 2:
		n, err := r.varint()
		if err != nil {
			return err
		}
		_, err = r.take(int(n))
		return err
	case 5:
		_, err := r.take(4)
		return err
	}
	return fmt.Errorf("sparkplug: unsupported wire type %d", wire)
}

func unmarshalMetric(b []byte) (Metric, error) {
	var m Metric
	r := &reader{b: b}
	for !r.done() {
		tag, err := r.varint()
		if err != nil {
			return m, err
		}
		field, wire := tag>>3, tag&7
		switch {
		case field == 1 && wire == 2:
			n, err := r.varint()
			if err != nil {
				return m, err
			}
			s, err := r.take(int(n))
			if err != nil {
				return m, err
			}
			m.Name = string(s)
		case field == 3 && wire == 0:
			m.Timestamp, err = r.varint()
		case field == 4 && wire == 0:
			var v uint64
			v, err = r.varint()
			m.Datatype = uint32(v)
		case (field == 10 || field == 11) && wire == 0:
			var v uint64
			v, err = r.varint()
			m.Value = int64(v)
		case field == 12 && wire == 5:
			var s []byte
			s, err = r.take(4)
			if err == nil {
				m.Value = float64(math.Float32frombits(binary.LittleEndian.Uint32(s)))
			}
		case field == 13 && wire == 1:
			var s []byte
			s, err = r.take(8)
			if err == nil {
				m.Value = math.Float64frombits(binary.LittleEndian.Uint64(s))
			}
		case field == 14 && wire == 0:
			var v uint64
			v, err = r.varint()
			m.Value = v != 0
		case field == 15 && wire == 2:
			var n uint64
			n, err = r.varint()
			if err == nil {
				var s []byte
				s, err = r.take(int(n))
				m.Value = string(s)
			}
		default:
			err = r.skip(wire)
		}
		if err != nil {
			return m, err
		}
	}
	return m, nil
}

// Unmarshal decodes a Sparkplug B payload.
func Unmarshal(b []byte) (Payload, error) {
	var p Payload
	r := &reader{b: b}
	for !r.done() {
		tag, err := r.varint()
		if err != nil {
			return p, err
		}
		field, wire := tag>>3, tag&7
		switch {
		case field == 1 && wire == 0:
			p.Timestamp, err = r.varint()
		case field == 2 && wire == 2:
			var n uint64
			n, err = r.varint()
			if err == nil {
				var s []byte
				s, err = r.take(int(n))
				if err == nil {
					var m Metric
					m, err = unmarshalMetric(s)
					p.Metrics = append(p.Metrics, m)
				}
			}
		case field == 3 && wire == 0:
			p.Seq, err = r.varint()
		default:
			err = r.skip(wire)
		}
		if err != nil {
			return p, err
		}
	}
	return p, nil
}
