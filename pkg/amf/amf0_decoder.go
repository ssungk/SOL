package amf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

func DecodeAMF0Sequence(r io.Reader) ([]any, error) {
	values := make([]any, 0, 5)

	for {
		val, err := DecodeAMF0(r)
		switch {
		case err == nil:
			values = append(values, val)
		case errors.Is(err, io.EOF):
			return values, nil
		default:
			return nil, fmt.Errorf("AMF0 decode failed: %w", err)
		}
	}
}

func DecodeAMF0(r io.Reader) (any, error) {
	marker := make([]byte, 1)
	if _, err := io.ReadFull(r, marker); err != nil {
		return nil, err
	}

	switch marker[0] {
	case numberMarker:
		return decodeNumber(r)
	case booleanMarker:
		return decodeBoolean(r)
	case stringMarker:
		return decodeString(r)
	case objectMarker:
		return decodeObject(r)
	case nullMarker, undefinedMarker:
		return decodeNull(r)
	case ecmaArrayMarker:
		return decodeECMAArray(r)
	case strictArrayMarker:
		return decodeStrictArray(r)
	case dateMarker:
		return decodeDate(r)
	case longStringMarker:
		return decodeLongString(r)
	default:
		return nil, fmt.Errorf("unsupported AMF0 marker: 0x%x", marker[0])
	}
}

func decodeNumber(r io.Reader) (float64, error) {
	var num float64
	err := binary.Read(r, binary.BigEndian, &num)
	return num, err
}

func decodeBoolean(r io.Reader) (bool, error) {
	b := make([]byte, 1)
	if _, err := io.ReadFull(r, b); err != nil {
		return false, err
	}
	return b[0] != 0, nil
}

func decodeString(r io.Reader) (string, error) {
	var length uint16
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func decodeLongString(r io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func decodeNull(_ io.Reader) (any, error) {
	return nil, nil
}

func decodeECMAArray(r io.Reader) (map[string]any, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	return decodeObject(r)
}

func decodeObject(r io.Reader) (map[string]any, error) {
	obj := make(map[string]any)
	end := make([]byte, 1)

	for {
		key, err := decodeString(r)
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			if _, err := io.ReadFull(r, end); err != nil {
				return nil, err
			}
			if end[0] == objectEndMarker {
				break
			}
			return nil, errors.New("expected object end marker")
		}
		val, err := DecodeAMF0(r)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	return obj, nil
}

func decodeStrictArray(r io.Reader) ([]any, error) {
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	arr := make([]any, count)
	for i := uint32(0); i < count; i++ {
		v, err := DecodeAMF0(r)
		if err != nil {
			return nil, err
		}
		arr[i] = v
	}
	return arr, nil
}

func decodeDate(r io.Reader) (time.Time, error) {
	var millis float64
	if err := binary.Read(r, binary.BigEndian, &millis); err != nil {
		return time.Time{}, err
	}

	offset := make([]byte, 2)
	if _, err := io.ReadFull(r, offset); err != nil {
		return time.Time{}, err
	}

	sec := int64(millis / 1000)
	fracMillis := math.Mod(millis, 1000)
	nanoSec := int64(fracMillis * 1e6)

	return time.Unix(sec, nanoSec).UTC(), nil
}
