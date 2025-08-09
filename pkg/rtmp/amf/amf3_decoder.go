package amf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// decodeU29 decodes a variable-length U29 integer.
func (ctx *AMF3Context) decodeU29(r io.Reader) (uint32, error) {
	var result uint32
	var b byte
	var err error
	for i := 0; i < 3; i++ {
		b, err = readByte(r)
		if err != nil {
			return 0, err
		}
		if b < 0x80 {
			result = (result << 7) | uint32(b)
			return result, nil
		}
		result = (result << 7) | uint32(b&0x7F)
	}
	b, err = readByte(r)
	if err != nil {
		return 0, err
	}
	result = (result << 8) | uint32(b)
	return result, nil
}

// decodeInteger decodes an AMF3 integer.
func (ctx *AMF3Context) decodeInteger(r io.Reader) (int32, error) {
	val, err := ctx.decodeU29(r)
	if err != nil {
		return 0, err
	}
	// Sign-extend if the 29th bit is set
	if val&0x10000000 != 0 {
		return int32(val | 0xE0000000), nil
	}
	return int32(val), nil
}

// decodeDouble decodes an AMF3 double.
func (ctx *AMF3Context) decodeDouble(r io.Reader) (float64, error) {
	var val float64
	err := binary.Read(r, binary.BigEndian, &val)
	return val, err
}

// decodeString decodes an AMF3 string.
func (ctx *AMF3Context) decodeString(r io.Reader) (string, error) {
	return ctx.decodeStringValue(r)
}

// decodeStringValue decodes the string payload.
func (ctx *AMF3Context) decodeStringValue(r io.Reader) (string, error) {
	u29, err := ctx.decodeU29(r)
	if err != nil {
		return "", err
	}

	if u29&1 == 0 { // It's a reference
		idx := int(u29 >> 1)
		if idx >= len(ctx.stringTable) {
			return "", errors.New("string reference out of bounds")
		}
		return ctx.stringTable[idx], nil
	}

	length := int(u29 >> 1)
	if length == 0 {
		return "", nil
	}

	buf, err := readBytes(r, length)
	if err != nil {
		return "", err
	}

	str := string(buf)
	ctx.stringTable = append(ctx.stringTable, str)
	return str, nil
}

// decodeObject decodes an AMF3 object.
func (ctx *AMF3Context) decodeObject(r io.Reader) (any, error) {
	u29, err := ctx.decodeU29(r)
	if err != nil {
		return nil, err
	}

	if u29&1 == 0 { // Reference
		idx := int(u29 >> 1)
		if idx >= len(ctx.objectTable) {
			return nil, errors.New("object reference out of bounds")
		}
		return ctx.objectTable[idx], nil
	}

	// Inline object, traits may be referenced or inline
	if u29&2 == 0 { // Trait reference
		return nil, errors.New("trait references not supported")
	}

	// Inline traits
	// For simplicity, we assume not dynamic and no externalizable
	className, err := ctx.decodeStringValue(r)
	if err != nil {
		return nil, err
	}

	obj := make(map[string]any)
	if className != "" {
		// This could be a typed object, store class name if needed
	}

	ctx.objectTable = append(ctx.objectTable, obj)

	for {
		key, err := ctx.decodeStringValue(r)
		if err != nil {
			return nil, err
		}
		if key == "" {
			break
		}
		value, err := ctx.DecodeAMF3(r)
		if err != nil {
			return nil, err
		}
		obj[key] = value
	}

	return obj, nil
}

// decodeArray decodes an AMF3 array.
func (ctx *AMF3Context) decodeArray(r io.Reader) (any, error) {
	u29, err := ctx.decodeU29(r)
	if err != nil {
		return nil, err
	}

	if u29&1 == 0 { // Reference
		idx := int(u29 >> 1)
		if idx >= len(ctx.objectTable) {
			return nil, errors.New("array reference out of bounds")
		}
		arr, ok := ctx.objectTable[idx].([]any)
		if !ok {
			return nil, errors.New("referenced object is not an array")
		}
		return arr, nil
	}

	length := int(u29 >> 1)
	arr := make([]any, length)
	ctx.objectTable = append(ctx.objectTable, arr)

	// Associative part (not handled in this simplified version)
	for {
		key, err := ctx.decodeStringValue(r)
		if err != nil {
			return nil, err
		}
		if key == "" {
			break
		}
		// Skip associative values
		val, err := ctx.DecodeAMF3(r)
		if err != nil {
			return nil, err
		}
		_ = val
	}

	for i := 0; i < length; i++ {
		val, err := ctx.DecodeAMF3(r)
		if err != nil {
			return nil, err
		}
		arr[i] = val
	}

	return arr, nil
}

// decodeDate decodes an AMF3 date.
func (ctx *AMF3Context) decodeDate(r io.Reader) (any, error) {
	u29, err := ctx.decodeU29(r)
	if err != nil {
		return nil, err
	}

	if u29&1 == 0 { // Reference
		idx := int(u29 >> 1)
		if idx >= len(ctx.objectTable) {
			return nil, errors.New("date reference out of bounds")
		}
		t, ok := ctx.objectTable[idx].(time.Time)
		if !ok {
			return nil, errors.New("referenced object is not a time.Time")
		}
		return t, nil
	}

	var millis float64
	if err := binary.Read(r, binary.BigEndian, &millis); err != nil {
		return nil, err
	}

	t := time.UnixMilli(int64(millis))
	ctx.objectTable = append(ctx.objectTable, t)
	return t, nil
}

// DecodeAMF3 decodes a single AMF3 value.
func (ctx *AMF3Context) DecodeAMF3(r io.Reader) (any, error) {
	marker, err := readByte(r)
	if err != nil {
		return nil, err
	}

	switch marker {
	case amf3UndefinedMarker, amf3NullMarker:
		return nil, nil
	case amf3FalseMarker:
		return false, nil
	case amf3TrueMarker:
		return true, nil
	case amf3IntegerMarker:
		return ctx.decodeInteger(r)
	case amf3DoubleMarker:
		return ctx.decodeDouble(r)
	case amf3StringMarker:
		return ctx.decodeString(r)
	case amf3DateMarker:
		return ctx.decodeDate(r)
	case amf3ArrayMarker:
		return ctx.decodeArray(r)
	case amf3ObjectMarker:
		return ctx.decodeObject(r)
	default:
		return nil, fmt.Errorf("unsupported AMF3 marker: 0x%02x", marker)
	}
}

// DecodeAMF3Sequence decodes a sequence of AMF3 values.
func DecodeAMF3Sequence(r io.Reader) ([]any, error) {
	var values []any
	ctx := NewAMF3Context()
	for {
		val, err := ctx.DecodeAMF3(r)
		if err != nil {
			if err == io.EOF {
				return values, nil
			}
			return nil, err
		}
		values = append(values, val)
	}
}

