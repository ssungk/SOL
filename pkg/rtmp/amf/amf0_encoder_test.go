package amf

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 에러를 발생시키는 Writer (에러 케이스 테스트용)
type errorWriter struct {
	errorAfter int
	writeCount int
}

func (ew *errorWriter) Write(p []byte) (n int, err error) {
	ew.writeCount++
	if ew.writeCount > ew.errorAfter {
		return 0, errors.New("write error")
	}
	return len(p), nil
}

// 특정 바이트 수 이후 에러를 발생시키는 Writer
type errorAfterBytesWriter struct {
	writtenBytes int
	errorAfter   int
}

func (ew *errorAfterBytesWriter) Write(p []byte) (n int, err error) {
	if ew.writtenBytes+len(p) > ew.errorAfter {
		return 0, errors.New("write error after bytes")
	}
	ew.writtenBytes += len(p)
	return len(p), nil
}

func TestEncodeAMF0Sequence_Success(t *testing.T) {
	values := []any{3.14, true, "hello", map[string]any{"foo": "bar"}}
	data, err := EncodeAMF0Sequence(values...)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty encoded data")
	}

	// 디코딩해서 원래 값과 비교
	decoded, err := DecodeAMF0Sequence(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(values) {
		t.Errorf("expected %d values, got %d", len(values), len(decoded))
	}
}

func TestEncodeAMF0Sequence_Error(t *testing.T) {
	// 지원하지 않는 타입으로 에러 발생
	type unsupportedType struct{}
	_, err := EncodeAMF0Sequence(unsupportedType{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestEncodeAMF0_Number(t *testing.T) {
	data, err := EncodeAMF0Sequence(3.14)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x00, 0x40, 0x09, 0x1e, 0xb8, 0x51, 0xeb, 0x85, 0x1f}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeValue_Float32(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, float32(3.14))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Int(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, int(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Int32(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, int32(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Int64(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, int64(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Uint(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, uint(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Uint32(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, uint32(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_Uint64(t *testing.T) {
	buf := new(bytes.Buffer)
	err := encodeValue(buf, uint64(42))
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[0] != numberMarker {
		t.Errorf("expected numberMarker, got 0x%02x", data[0])
	}
}

func TestEncodeValue_NumberWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, 3.14)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestEncodeValue_IntWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, int(42))
	if err == nil {
		t.Fatal("expected int write error")
	}
}

func TestEncodeValue_Float32WriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, float32(3.14))
	if err == nil {
		t.Fatal("expected float32 write error")
	}
}

func TestEncodeValue_Int32WriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, int32(42))
	if err == nil {
		t.Fatal("expected int32 write error")
	}
}

func TestEncodeValue_UintWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, uint(42))
	if err == nil {
		t.Fatal("expected uint write error")
	}
}

func TestEncodeValue_Uint32WriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, uint32(42))
	if err == nil {
		t.Fatal("expected uint32 write error")
	}
}

func TestEncodeValue_Uint64WriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, uint64(42))
	if err == nil {
		t.Fatal("expected uint64 write error")
	}
}

func TestEncodeValue_Int64BinaryWriteError(t *testing.T) {
	// int64 케이스에서 binary.Write 에러 테스트
	ew := &errorWriter{errorAfter: 1} // numberMarker 쓰기는 성공, binary.Write에서 실패
	err := encodeValue(ew, int64(42))
	if err == nil {
		t.Fatal("expected int64 binary write error")
	}
}

func TestEncodeValue_Int64WriteError(t *testing.T) {
	// int64 케이스에서 writeByte(numberMarker) 에러 테스트
	ew := &errorWriter{errorAfter: 0} // numberMarker 쓰기에서 실패
	err := encodeValue(ew, int64(42))
	if err == nil {
		t.Fatal("expected int64 write error")
	}
}

func TestEncodeValue_NumberBinaryWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	err := encodeValue(ew, 3.14)
	if err == nil {
		t.Fatal("expected binary write error")
	}
}

func TestEncodeAMF0_Boolean(t *testing.T) {
	// true 테스트
	data, err := EncodeAMF0Sequence(true)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte{0x01, 0x01}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v for true, got %v", expected, data)
	}

	// false 테스트
	data, err = EncodeAMF0Sequence(false)
	if err != nil {
		t.Fatal(err)
	}
	expected = []byte{0x01, 0x00}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v for false, got %v", expected, data)
	}
}

func TestEncodeValue_BooleanWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, true)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestEncodeAMF0_String(t *testing.T) {
	data, err := EncodeAMF0Sequence("hello")
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x02, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeAMF0_String_Empty(t *testing.T) {
	data, err := EncodeAMF0Sequence("")
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x02, 0x00, 0x00}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeString_ShortString_MarkerError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeString(ew, "hello")
	if err == nil {
		t.Fatal("expected marker write error")
	}
}

func TestEncodeString_ShortString_LengthError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	err := encodeString(ew, "hello")
	if err == nil {
		t.Fatal("expected length write error")
	}
}

func TestEncodeString_ShortString_DataError(t *testing.T) {
	ew := &errorWriter{errorAfter: 2}
	err := encodeString(ew, "hello")
	if err == nil {
		t.Fatal("expected data write error")
	}
}

func TestEncodeAMF0_LongString(t *testing.T) {
	// 65536 바이트 이상의 긴 문자열
	longStr := strings.Repeat("a", 70000)
	data, err := EncodeAMF0Sequence(longStr)
	if err != nil {
		t.Fatal(err)
	}

	if data[0] != longStringMarker {
		t.Errorf("expected longStringMarker (0x%02x), got 0x%02x", longStringMarker, data[0])
	}

	// 길이는 marker(1) + length(4) + data(70000) = 70005
	if len(data) != 70005 {
		t.Errorf("expected 70005 bytes, got %d", len(data))
	}
}

func TestEncodeString_LongString_MarkerError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	longStr := strings.Repeat("a", 70000)
	err := encodeString(ew, longStr)
	if err == nil {
		t.Fatal("expected marker write error")
	}
}

func TestEncodeString_LongString_LengthError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	longStr := strings.Repeat("a", 70000)
	err := encodeString(ew, longStr)
	if err == nil {
		t.Fatal("expected length write error")
	}
}

func TestEncodeString_LongString_DataError(t *testing.T) {
	ew := &errorWriter{errorAfter: 2}
	longStr := strings.Repeat("a", 70000)
	err := encodeString(ew, longStr)
	if err == nil {
		t.Fatal("expected data write error")
	}
}

func TestEncodeAMF0_Object(t *testing.T) {
	obj := map[string]any{"foo": "bar"}
	data, err := EncodeAMF0Sequence(obj)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{
		0x03, // objectMarker
		0x00, 0x03, 'f', 'o', 'o',
		0x02, 0x00, 0x03, 'b', 'a', 'r',
		0x00, 0x00, 0x09, // object end
	}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeAMF0_Object_Empty(t *testing.T) {
	obj := map[string]any{}
	data, err := EncodeAMF0Sequence(obj)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x03, 0x00, 0x00, 0x09}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeObject_MarkerError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	obj := map[string]any{"foo": "bar"}
	err := encodeObject(ew, obj)
	if err == nil {
		t.Fatal("expected marker write error")
	}
}

func TestEncodeObject_PropertyWriteOrder(t *testing.T) {
	// 프로퍼티 작성 중 에러 테스트
	// objectMarker(1) + key길이(2) + key(3) + valueMarker(1) + value길이(2) + value(5) = 14바이트
	// 10바이트 이후 에러로 endMarker 전에 실패
	ew := &errorAfterBytesWriter{errorAfter: 10}
	obj := map[string]any{"foo": "bar"}
	err := encodeObject(ew, obj)
	if err == nil {
		t.Fatal("expected property write error")
	}
}

func TestEncodeObject_EndMarkerError(t *testing.T) {
	// 빈 객체: objectMarker(1바이트) + endMarker(3바이트) = 총 4바이트
	// 2바이트 이후 에러 발생시켜서 endMarker 쓰기에서 실패
	ew := &errorAfterBytesWriter{errorAfter: 2}
	obj := map[string]any{}
	err := encodeObject(ew, obj)
	if err == nil {
		t.Fatal("expected end marker write error")
	}
}

func TestEncodeObjectProperty_KeyTooLong(t *testing.T) {
	buf := new(bytes.Buffer)
	longKey := strings.Repeat("a", 70000)
	err := encodeObjectProperty(buf, longKey, "value")
	if err == nil {
		t.Fatal("expected error for key too long")
	}
	if !strings.Contains(err.Error(), "object key too long") {
		t.Errorf("expected error to contain 'object key too long', got %v", err.Error())
	}
}

func TestEncodeObjectProperty_LengthError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeObjectProperty(ew, "key", "value")
	if err == nil {
		t.Fatal("expected length write error")
	}
}

func TestEncodeObjectProperty_KeyError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	err := encodeObjectProperty(ew, "key", "value")
	if err == nil {
		t.Fatal("expected key write error")
	}
}

func TestEncodeObjectProperty_ValueError(t *testing.T) {
	// 지원하지 않는 타입으로 에러 발생
	buf := new(bytes.Buffer)
	type unsupportedType struct{}
	err := encodeObjectProperty(buf, "key", unsupportedType{})
	if err == nil {
		t.Fatal("expected value encode error")
	}
	if !strings.Contains(err.Error(), "unsupported AMF0 type") {
		t.Errorf("expected error to contain 'unsupported AMF0 type', got %v", err.Error())
	}
}

func TestEncodeAMF0_Null(t *testing.T) {
	data, err := EncodeAMF0Sequence(nil)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x05}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeValue_NullWriteError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := encodeValue(ew, nil)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestEncodeAMF0_StrictArray(t *testing.T) {
	arr := []any{"a", "b"}
	data, err := EncodeAMF0Sequence(arr)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{
		0x0A,                   // strictArrayMarker
		0x00, 0x00, 0x00, 0x02, // length = 2
		0x02, 0x00, 0x01, 'a',
		0x02, 0x00, 0x01, 'b',
	}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeAMF0_StrictArray_Empty(t *testing.T) {
	arr := []any{}
	data, err := EncodeAMF0Sequence(arr)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0x0A, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(data, expected) {
		t.Errorf("expected %v, got %v", expected, data)
	}
}

func TestEncodeStrictArray_MarkerError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	arr := []any{"a", "b"}
	err := encodeStrictArray(ew, arr)
	if err == nil {
		t.Fatal("expected marker write error")
	}
}

func TestEncodeStrictArray_LengthError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	arr := []any{"a", "b"}
	err := encodeStrictArray(ew, arr)
	if err == nil {
		t.Fatal("expected length write error")
	}
}

func TestEncodeStrictArray_ElementError(t *testing.T) {
	// 배열 요소 작성 중 에러
	// strictArrayMarker(1) + length(4) + 첫번째요소(stringMarker(1) + length(2) + "a"(1)) = 9바이트
	// 8바이트 이후 에러로 두번째 요소에서 실패  
	ew := &errorAfterBytesWriter{errorAfter: 8}
	arr := []any{"a", "b"}
	err := encodeStrictArray(ew, arr)
	if err == nil {
		t.Fatal("expected element write error")
	}
}

func TestEncodeAMF0_Date(t *testing.T) {
	date := time.Date(2023, 3, 28, 19, 40, 0, 123*1e6, time.UTC)
	data, err := EncodeAMF0Sequence(date)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 11 {
		t.Errorf("expected 11 bytes for date, got %d", len(data))
	}

	if data[0] != dateMarker {
		t.Errorf("expected dateMarker (0x%02x), got 0x%02x", dateMarker, data[0])
	}
}

func TestEncodeDate_MarkerError(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	date := time.Now()
	err := encodeDate(ew, date)
	if err == nil {
		t.Fatal("expected marker write error")
	}
}

func TestEncodeDate_MillisError(t *testing.T) {
	ew := &errorWriter{errorAfter: 1}
	date := time.Now()
	err := encodeDate(ew, date)
	if err == nil {
		t.Fatal("expected millis write error")
	}
}

func TestEncodeDate_TimezoneError(t *testing.T) {
	ew := &errorWriter{errorAfter: 2}
	date := time.Now()
	err := encodeDate(ew, date)
	if err == nil {
		t.Fatal("expected timezone write error")
	}
}

func TestEncodeAMF0_UnsupportedType(t *testing.T) {
	type customType struct {
		field string
	}

	_, err := EncodeAMF0Sequence(customType{field: "test"})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}

	if !strings.Contains(err.Error(), "unsupported AMF0 type") {
		t.Errorf("expected error to contain 'unsupported AMF0 type', got %v", err.Error())
	}
}

func TestWriteByte_Success(t *testing.T) {
	buf := new(bytes.Buffer)
	err := writeByte(buf, 0xFF)
	if err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if len(data) != 1 || data[0] != 0xFF {
		t.Errorf("expected [0xFF], got %v", data)
	}
}

func TestWriteByte_Error(t *testing.T) {
	ew := &errorWriter{errorAfter: 0}
	err := writeByte(ew, 0xFF)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestEncodeAMF0_RoundTrip(t *testing.T) {
	// 인코딩 후 디코딩해서 원래 값과 비교하는 라운드트립 테스트
	testCases := []any{
		3.14,
		true,
		false,
		"hello world",
		nil,
		[]any{1.0, 2.0, 3.0},
		map[string]any{
			"name":  "test",
			"value": 123.45,
			"flag":  true,
		},
	}

	for i, original := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			// 인코딩
			encoded, err := EncodeAMF0Sequence(original)
			if err != nil {
				t.Fatalf("encoding failed: %v", err)
			}

			// 디코딩
			decoded, err := DecodeAMF0Sequence(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("decoding failed: %v", err)
			}

			if len(decoded) != 1 {
				t.Fatalf("expected 1 decoded value, got %d", len(decoded))
			}

			// 타입별 비교
			switch orig := original.(type) {
			case float64:
				if decoded[0] != orig {
					t.Errorf("expected %v, got %v", orig, decoded[0])
				}
			case bool:
				if decoded[0] != orig {
					t.Errorf("expected %v, got %v", orig, decoded[0])
				}
			case string:
				if decoded[0] != orig {
					t.Errorf("expected %v, got %v", orig, decoded[0])
				}
			case nil:
				if decoded[0] != nil {
					t.Errorf("expected nil, got %v", decoded[0])
				}
			default:
				// 복잡한 타입은 nil이 아님만 확인
				if decoded[0] == nil {
					t.Errorf("decoded value is nil")
				}
			}
		})
	}
}

// 벤치마크 테스트
func BenchmarkEncodeAMF0_Number(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = EncodeAMF0Sequence(3.14)
	}
}

func BenchmarkEncodeAMF0_String(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = EncodeAMF0Sequence("hello world")
	}
}

func BenchmarkEncodeAMF0_Object(b *testing.B) {
	obj := map[string]any{
		"name":  "test",
		"value": 123.45,
		"flag":  true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeAMF0Sequence(obj)
	}
}
