package amf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// encodeU29 encodes a uint32 as a variable-length U29 integer.
func (ctx *AMF3Context) encodeU29(w io.Writer, value uint32) error {
	buf := make([]byte, 4)
	n := 0
	if value < 0x80 {
		buf[0] = byte(value)
		n = 1
	} else if value < 0x4000 {
		buf[0] = byte(value>>7) | 0x80
		buf[1] = byte(value & 0x7F)
		n = 2
	} else if value < 0x200000 {
		buf[0] = byte(value>>14) | 0x80
		buf[1] = byte(value>>7) | 0x80
		buf[2] = byte(value & 0x7F)
		n = 3
	} else if value < 0x40000000 {
		buf[0] = byte(value>>22) | 0x80
		buf[1] = byte(value>>15) | 0x80
		buf[2] = byte(value>>8) | 0x80
		buf[3] = byte(value)
		n = 4
	} else {
		return fmt.Errorf("U29 out of range: %d", value)
	}
	_, err := w.Write(buf[:n])
	return err
}

// encodeInteger encodes an int32 value.
func (ctx *AMF3Context) encodeInteger(w io.Writer, value int32) error {
	if value >= -0x10000000 && value <= 0x0FFFFFFF { // Check if it fits in 29 bits
		if err := writeByte(w, amf3IntegerMarker); err != nil {
			return err
		}
		return ctx.encodeU29(w, uint32(value))
	} else {
		return ctx.encodeDouble(w, float64(value))
	}
}

// encodeDouble encodes a float64 value.
func (ctx *AMF3Context) encodeDouble(w io.Writer, value float64) error {
	if err := writeByte(w, amf3DoubleMarker); err != nil {
		return err
	}
	return binary.Write(w, binary.BigEndian, value)
}

// encodeString encodes a string value, using string reference table.
func (ctx *AMF3Context) encodeString(w io.Writer, value string) error {
	if err := writeByte(w, amf3StringMarker); err != nil {
		return err
	}
	return ctx.encodeStringValue(w, value)
}

// encodeStringValue encodes the string payload.
func (ctx *AMF3Context) encodeStringValue(w io.Writer, value string) error {
	if value == "" {
		return ctx.encodeU29(w, 1) // Length 0, inline
	}

	if idx, ok := ctx.stringTableMap[value]; ok {
		return ctx.encodeU29(w, uint32(idx<<1)) // Reference
	}

	ctx.stringTable = append(ctx.stringTable, value)
	ctx.stringTableMap[value] = len(ctx.stringTable) - 1

	if err := ctx.encodeU29(w, uint32(len(value)<<1)|1); err != nil {
		return err
	}
	_, err := w.Write([]byte(value))
	return err
}

// encodeObject encodes a map[string]any value.
func (ctx *AMF3Context) encodeObject(w io.Writer, value map[string]any) error {
	if err := writeByte(w, amf3ObjectMarker); err != nil {
		return err
	}

	// For simplicity, this implementation does not use object reference table.
	// Always encode as inline object with inline traits.

	if err := ctx.encodeU29(w, 0x0B); err != nil { // Inline object, inline traits, not dynamic
		return err
	}

	// Class name (empty)
	if err := ctx.encodeStringValue(w, ""); err != nil {
		return err
	}

	// Encode properties
	for key, val := range value {
		if err := ctx.encodeStringValue(w, key); err != nil {
			return err
		}
		if err := ctx.encodeValue(w, val); err != nil {
			return err
		}
	}

	// End of dynamic properties (empty key)
	return ctx.encodeStringValue(w, "")
}

// encodeArray encodes a []any value.
func (ctx *AMF3Context) encodeArray(w io.Writer, value []any) error {
	if err := writeByte(w, amf3ArrayMarker); err != nil {
		return err
	}

	// For simplicity, this implementation does not use object reference table.
	if err := ctx.encodeU29(w, uint32(len(value)<<1)|1); err != nil { // Length, inline
		return err
	}

	// Associative portion (empty key to terminate)
	if err := ctx.encodeStringValue(w, ""); err != nil {
		return err
	}

	// Dense portion
	for _, item := range value {
		if err := ctx.encodeValue(w, item); err != nil {
			return err
		}
	}
	return nil
}

// encodeDate encodes a time.Time value.
func (ctx *AMF3Context) encodeDate(w io.Writer, value time.Time) error {
	if err := writeByte(w, amf3DateMarker); err != nil {
		return err
	}
	// For simplicity, does not use object reference table.
	if err := ctx.encodeU29(w, 1); err != nil { // Inline, not a reference
		return err
	}
	return binary.Write(w, binary.BigEndian, float64(value.UnixMilli()))
}

// encodeValue encodes a single value of any supported type.
func (ctx *AMF3Context) encodeValue(w io.Writer, value any) error {
	switch v := value.(type) {
	case nil:
		return writeByte(w, amf3NullMarker)
	case bool:
		if v {
			return writeByte(w, amf3TrueMarker)
		} else {
			return writeByte(w, amf3FalseMarker)
		}
	case int:
		return ctx.encodeInteger(w, int32(v))
	case int32:
		return ctx.encodeInteger(w, v)
	case int64:
		return ctx.encodeDouble(w, float64(v))
	case uint:
		return ctx.encodeDouble(w, float64(v))
	case uint32:
		return ctx.encodeDouble(w, float64(v))
	case uint64:
		return ctx.encodeDouble(w, float64(v))
	case float32:
		return ctx.encodeDouble(w, float64(v))
	case float64:
		return ctx.encodeDouble(w, v)
	case string:
		return ctx.encodeString(w, v)
	case map[string]any:
		return ctx.encodeObject(w, v)
	case []any:
		return ctx.encodeArray(w, v)
	case time.Time:
		return ctx.encodeDate(w, v)
	default:
		return fmt.Errorf("unsupported AMF3 type: %T", value)
	}
}

// EncodeAMF3Sequence encodes a sequence of values into a byte slice.
func EncodeAMF3Sequence(values ...any) ([]byte, error) {
	buf := new(bytes.Buffer)
	ctx := NewAMF3Context()
	for _, value := range values {
		if err := ctx.encodeValue(buf, value); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

