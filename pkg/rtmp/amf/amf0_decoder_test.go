package amf

import (
	"bytes"
	"testing"
	"time"
)

func TestDecodeAMF0Sequence(t *testing.T) {
	data := []byte{0x00,
		0x40, 0x09, 0x1e, 0xb8, 0x51, 0xeb, 0x85, 0x1f,
		0x01, 0x01,
		0x02, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o',
		0x03, 0x00, 0x03, 'f', 'o', 'o', 0x02, 0x00, 0x03, 'b', 'a', 'r', 0x00, 0x00, 0x09}
	values, err := DecodeAMF0Sequence(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := values[0].(float64); !ok {
		t.Fatal(err)
	}

	if _, ok := values[1].(bool); !ok {
		t.Fatal(err)
	}

	if _, ok := values[2].(string); !ok {
		t.Fatal(err)
	}

	if _, ok := values[3].(map[string]any); !ok {
		t.Fatal(err)
	}
}

func TestDecodeAMF0Sequence_MalformedData(t *testing.T) {
	data := []byte{0x00, 0x40, 0x09, 0x1e, 0xb8, 0x51}
	_, err := DecodeAMF0Sequence(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed data")
	}
}

func TestDecodeAMF0_InvalidInput_EmptyReader(t *testing.T) {
	_, err := DecodeAMF0(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error for empty reader")
	}
}

func TestDecodeAMF0_UnsupportedMarker(t *testing.T) {
	data := []byte{0xff}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for unsupported marker")
	}
}

func TestDecodeAMF0_Number(t *testing.T) {
	data := []byte{0x00, 0x40, 0x09, 0x1e, 0xb8, 0x51, 0xeb, 0x85, 0x1f} // 3.14
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val.(float64) != 3.14 {
		t.Errorf("expected 3.14, got %v", val)
	}
}

func TestDecodeAMF0_Number_MalformedData(t *testing.T) {
	data := []byte{0x00, 0x40, 0x09, 0x1e, 0xb8, 0x51, 0xeb, 0x85}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed number data")
	}
}

func TestDecodeAMF0_Boolean(t *testing.T) {
	data := []byte{0x01, 0x01} // booleanMarker, true
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val.(bool) != true {
		t.Errorf("expected true, got %v", val)
	}
}

func TestDecodeAMF0_Boolean_MalformedData(t *testing.T) {
	data := []byte{0x01}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed boolean data")
	}
}

func TestDecodeAMF0_String(t *testing.T) {
	data := []byte{0x02, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := val.(string); !ok || s != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestDecodeAMF0_String_MalformedShortLength(t *testing.T) {
	data := []byte{0x02, 0x00} // string length incomplete
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete string length")
	}
}

func TestDecodeAMF0_String_MalformedShortData(t *testing.T) {
	data := []byte{0x02, 0x00, 0x05, 'h', 'e', 'l'} // string data too short
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete string data")
	}
}

func TestDecodeAMF0_Object(t *testing.T) {
	data := []byte{
		0x03, // objectMarker
		0x00, 0x03, 'f', 'o', 'o',
		0x02, 0x00, 0x03, 'b', 'a', 'r',
		0x00, 0x00, 0x09, // object end
	}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := val.(map[string]any)
	if !ok || obj["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %v", obj)
	}
}

func TestDecodeAMF0_Object_MalformedShortKey(t *testing.T) {
	data := []byte{0x03, 0x00, 0x03, 'f', 'o'}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete object key")
	}
}

func TestDecodeAMF0_Object_MalformedShortValue(t *testing.T) {
	data := []byte{
		0x03,
		0x00, 0x03, 'f', 'o', 'o',
		0x02, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete object value")
	}
}

func TestDecodeAMF0_Object_MissingEndMarker(t *testing.T) {
	data := []byte{
		0x03, // objectMarker
		0x00, 0x03, 'f', 'o', 'o',
		0x02, 0x00, 0x03, 'b', 'a', 'r',
		0x00, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for missing object end marker")
	}
}

func TestDecodeAMF0_Object_InvalidEndMarker(t *testing.T) {
	data := []byte{
		0x03, // objectMarker
		0x00, 0x03, 'f', 'o', 'o',
		0x02, 0x00, 0x03, 'b', 'a', 'r',
		0x00, 0x00, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for invalid object end marker")
	}
}

func TestDecodeAMF0_Null(t *testing.T) {
	data := []byte{0x05}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestDecodeAMF0_Undefined(t *testing.T) {
	data := []byte{0x06}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestDecodeAMF0_ECMAArray(t *testing.T) {
	data := []byte{
		0x08,                   // ecmaArrayMarker
		0x00, 0x00, 0x00, 0x01, // length
		0x00, 0x03, 'k', 'e', 'y',
		0x02, 0x00, 0x03, 'v', 'a', 'l',
		0x00, 0x00, 0x09, // end
	}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := val.(map[string]any)
	if !ok || m["key"] != "val" {
		t.Errorf("expected key=val, got %v", m)
	}
}

func TestDecodeAMF0_ECMAArray_MalformedLength(t *testing.T) {
	data := []byte{0x08, 0x00, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed ECMA array length")
	}
}

func TestDecodeAMF0_StrictArray(t *testing.T) {
	data := []byte{
		0x0A,                   // strictArrayMarker
		0x00, 0x00, 0x00, 0x02, // length = 2
		0x02, 0x00, 0x01, 'a',
		0x02, 0x00, 0x01, 'b',
	}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := val.([]any)
	if !ok || len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Errorf("expected [a b], got %v", val)
	}
}

func TestDecodeAMF0_StrictArray_MalformedLength(t *testing.T) {
	data := []byte{0x0A, 0x00, 0x00, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed strict array length")
	}
}

func TestDecodeAMF0_StrictArray_MalformedElement(t *testing.T) {
	data := []byte{0x0A, 0x00, 0x00, 0x00, 0x02,
		0x02, 0x00, 0x01, 'a',
		0x02, 0x00, 0x01,
	}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed strict array element")
	}
}

func TestDecodeAMF0_Date(t *testing.T) {
	expected := time.Date(2023, 3, 28, 19, 40, 0, 123*1e6, time.UTC)

	data := []byte{
		0x0B, // dateMarker
		0x42, 0x78, 0x72, 0x9B,
		0xC0, 0x2F, 0xB0, 0x00,
		0x00, 0x00, // timezone offset (ignored)
	}

	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := val.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", val)
	}

	if !got.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestDecodeAMF0_Date_MalformedShortData(t *testing.T) {
	data := []byte{0x0B, 0x00, 0x01}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Error("expected error for malformed date data")
	}
}

func TestDecodeAMF0_Date_MalformedMissingOffset(t *testing.T) {
	data := make([]byte, 9)
	data[0] = 0x0B
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Error("expected error for missing date offset")
	}
}

func TestDecodeAMF0_LongString(t *testing.T) {
	data := []byte{0x0c, 0x00, 0x00, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'}
	val, err := DecodeAMF0(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := val.(string); !ok || s != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestDecodeAMF0_LongString_MalformedShortLength(t *testing.T) {
	data := []byte{0x0c, 0x00, 0x00, 0x00}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete long string length")
	}
}

func TestDecodeAMF0_LongString_MalformedShortData(t *testing.T) {
	data := []byte{0x0c, 0x00, 0x00, 0x00, 0x05, 'h', 'e', 'l'}
	_, err := DecodeAMF0(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for incomplete long string data")
	}
}
