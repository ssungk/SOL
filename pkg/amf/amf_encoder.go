package amf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

func EncodeAMF0Sequence(values ...any) ([]byte, error) {
	buf := new(bytes.Buffer)
	for _, val := range values {
		if err := encodeValue(buf, val); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeValue(w io.Writer, value any) error {
	switch v := value.(type) {
	case nil:
		_, err := w.Write([]byte{nullMarker})
		return err
	case bool:
		b := byte(0)
		if v {
			b = 1
		}
		_, err := w.Write([]byte{booleanMarker, b})
		return err
	case float64:
		if err := writeByte(w, numberMarker); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, v)
	case float32:
		if err := writeByte(w, numberMarker); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, float64(v))
	case int:
		if err := writeByte(w, numberMarker); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, float64(v))
	case int32:
		if err := writeByte(w, numberMarker); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, float64(v))
	case int64:
		if err := writeByte(w, numberMarker); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, float64(v))
	case string:
		return encodeString(w, v)
	case map[string]any:
		return encodeObject(w, v)
	case []any:
		return encodeStrictArray(w, v)
	case time.Time:
		return encodeDate(w, v)
	default:
		return errors.New("unsupported AMF0 type")
	}
}

func encodeString(w io.Writer, s string) error {
	length := len(s)
	if length < 65536 {
		if err := writeByte(w, stringMarker); err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint16(length)); err != nil {
			return err
		}
		_, err := io.WriteString(w, s)
		return err
	} else {
		if err := writeByte(w, longStringMarker); err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint32(length)); err != nil {
			return err
		}
		_, err := io.WriteString(w, s)
		return err
	}
}

func encodeObject(w io.Writer, obj map[string]any) error {
	if err := writeByte(w, objectMarker); err != nil {
		return err
	}
	for key, val := range obj {
		if err := encodeObjectProperty(w, key, val); err != nil {
			return err
		}
	}
	// object end marker: 0x00 0x00 0x09
	_, err := w.Write([]byte{0x00, 0x00, objectEndMarker})
	return err
}

func encodeObjectProperty(w io.Writer, key string, val any) error {
	keyLen := len(key)
	if keyLen > 65535 {
		return errors.New("object key too long")
	}
	if err := binary.Write(w, binary.BigEndian, uint16(keyLen)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, key); err != nil {
		return err
	}
	return encodeValue(w, val)
}

func encodeStrictArray(w io.Writer, arr []any) error {
	if err := writeByte(w, strictArrayMarker); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(arr))); err != nil {
		return err
	}
	for _, v := range arr {
		if err := encodeValue(w, v); err != nil {
			return err
		}
	}
	return nil
}

func encodeDate(w io.Writer, t time.Time) error {
	if err := writeByte(w, dateMarker); err != nil {
		return err
	}
	ms := float64(t.UnixNano()) / 1e6
	if err := binary.Write(w, binary.BigEndian, ms); err != nil {
		return err
	}
	// timezone, always 0
	return binary.Write(w, binary.BigEndian, int16(0))
}

func writeByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}
