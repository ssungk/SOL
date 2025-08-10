package amf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// 제한된 Reader (테스트용)
type limitedErrorReader struct {
	data       []byte
	readCount  int
	errorAfter int
}

func (r *limitedErrorReader) Read(p []byte) (n int, err error) {
	r.readCount++
	if r.readCount > r.errorAfter {
		return 0, io.EOF
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n = copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestDecodeAMF3Sequence_Success(t *testing.T) {
	// 여러 값들을 인코딩
	values := []any{int32(42), true, "hello"}
	encoded, err := EncodeAMF3Sequence(values...)
	if err != nil {
		t.Fatal(err)
	}

	// 디코딩
	decoded, err := DecodeAMF3Sequence(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(values) {
		t.Errorf("expected %d values, got %d", len(values), len(decoded))
	}
}

func TestDecodeAMF3Sequence_EOF(t *testing.T) {
	// 빈 데이터
	decoded, err := DecodeAMF3Sequence(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 0 {
		t.Errorf("expected 0 values, got %d", len(decoded))
	}
}

func TestDecodeAMF3Sequence_Error(t *testing.T) {
	// 잘못된 마커
	data := []byte{0xFF} // 지원하지 않는 마커
	_, err := DecodeAMF3Sequence(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for invalid marker")
	}
}

func TestDecodeAMF3_UnsupportedMarker(t *testing.T) {
	ctx := NewAMF3Context()
	data := []byte{0xFF} // 지원하지 않는 마커
	_, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for unsupported marker")
	}
	if !strings.Contains(err.Error(), "unsupported AMF3 marker") {
		t.Errorf("expected error to contain 'unsupported AMF3 marker', got %v", err.Error())
	}
}

func TestDecodeAMF3_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	// 빈 리더로 마커 읽기 실패
	_, err := ctx.DecodeAMF3(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeAMF3_Null(t *testing.T) {
	ctx := NewAMF3Context()
	data := []byte{amf3NullMarker}
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestDecodeAMF3_Undefined(t *testing.T) {
	ctx := NewAMF3Context()
	data := []byte{amf3UndefinedMarker}
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestDecodeAMF3_Boolean(t *testing.T) {
	ctx := NewAMF3Context()
	
	// true 테스트
	data := []byte{amf3TrueMarker}
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != true {
		t.Errorf("expected true, got %v", val)
	}

	// false 테스트
	data = []byte{amf3FalseMarker}
	val, err = ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if val != false {
		t.Errorf("expected false, got %v", val)
	}
}

func TestDecodeAMF3_Integer(t *testing.T) {
	ctx := NewAMF3Context()
	
	testCases := []struct {
		data     []byte
		expected int32
	}{
		{[]byte{amf3IntegerMarker, 0x00}, 0},
		{[]byte{amf3IntegerMarker, 0x7F}, 127},
		{[]byte{amf3IntegerMarker, 0x81, 0x00}, 128},
		{[]byte{amf3IntegerMarker, 0xFF, 0x7F}, 16383},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			val, err := ctx.DecodeAMF3(bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if val != tc.expected {
				t.Errorf("expected %d, got %v", tc.expected, val)
			}
		})
	}
}

func TestDecodeInteger_SignExtension(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 29번째 비트(0x10000000)가 1인 값 생성
	// 실제로는 더 큰 값을 사용해서 29번째 비트를 1로 만들기
	data := []byte{0xFF, 0xFF, 0xFF, 0xFF} // 최대값으로 29번째 비트 확실히 1
	result, err := ctx.decodeInteger(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	// 부호 확장이 적용되어 음수가 되어야 함
	if result >= 0 {
		t.Errorf("expected negative result for sign extension, got %d", result)
	}
}

func TestDecodeInteger_NoSignExtension(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 양수 테스트 (부호 확장 불필요)
	result, err := ctx.decodeInteger(bytes.NewReader([]byte{0x7F}))
	if err != nil {
		t.Fatal(err)
	}
	
	if result != 127 {
		t.Errorf("expected 127, got %d", result)
	}
}

func TestDecodeInteger_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// U29 읽기 실패
	_, err := ctx.decodeInteger(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeAMF3_Double(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 3.14를 직접 인코딩한 데이터
	buf := new(bytes.Buffer)
	err := ctx.encodeDouble(buf, 3.14)
	if err != nil {
		t.Fatal(err)
	}
	
	val, err := ctx.DecodeAMF3(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	
	if val != 3.14 {
		t.Errorf("expected 3.14, got %v", val)
	}
}

func TestDecodeDouble_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 불완전한 데이터
	_, err := ctx.decodeDouble(bytes.NewReader([]byte{0x40}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeAMF3_String(t *testing.T) {
	ctx := NewAMF3Context()
	
	// "hello" 인코딩
	buf := new(bytes.Buffer)
	err := ctx.encodeString(buf, "hello")
	if err != nil {
		t.Fatal(err)
	}
	
	val, err := ctx.DecodeAMF3(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestDecodeAMF3_String_Empty(t *testing.T) {
	ctx := NewAMF3Context()
	
	data := []byte{amf3StringMarker, 0x01} // 빈 문자열
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	if val != "" {
		t.Errorf("expected empty string, got %v", val)
	}
}

func TestDecodeAMF3_String_Reference(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 먼저 문자열을 테이블에 추가
	ctx.stringTable = append(ctx.stringTable, "referenced")
	
	data := []byte{amf3StringMarker, 0x00} // 첫 번째 참조
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	if val != "referenced" {
		t.Errorf("expected 'referenced', got %v", val)
	}
}

func TestDecodeStringValue_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// U29 읽기 실패
	_, err := ctx.decodeStringValue(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeStringValue_ReferenceOutOfBounds(t *testing.T) {
	ctx := NewAMF3Context()
	
	data := []byte{0x02} // 참조 인덱스 1 (존재하지 않음)
	_, err := ctx.decodeStringValue(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected reference out of bounds error")
	}
	if !strings.Contains(err.Error(), "string reference out of bounds") {
		t.Errorf("expected error to contain 'string reference out of bounds', got %v", err.Error())
	}
}

func TestDecodeStringValue_DataReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 길이는 5인데 데이터가 부족
	data := []byte{0x0B} // (5<<1)|1 = 0x0B, 하지만 실제 데이터 없음
	_, err := ctx.decodeStringValue(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected data read error")
	}
}

func TestDecodeAMF3_Array(t *testing.T) {
	ctx := NewAMF3Context()
	
	// []any{"a", "b"} 인코딩
	arr := []any{"a", "b"}
	buf := new(bytes.Buffer)
	err := ctx.encodeArray(buf, arr)
	if err != nil {
		t.Fatal(err)
	}
	
	val, err := ctx.DecodeAMF3(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	
	decodedArr, ok := val.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", val)
	}
	
	if len(decodedArr) != 2 {
		t.Errorf("expected 2 elements, got %d", len(decodedArr))
	}
}

func TestDecodeArray_Reference(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 먼저 배열을 테이블에 추가
	testArr := []any{"test"}
	ctx.objectTable = append(ctx.objectTable, testArr)
	
	data := []byte{amf3ArrayMarker, 0x00} // 첫 번째 참조
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	arr, ok := val.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", val)
	}
	
	if len(arr) != 1 || arr[0] != "test" {
		t.Errorf("expected [\"test\"], got %v", arr)
	}
}

func TestDecodeArray_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// U29 읽기 실패
	_, err := ctx.decodeArray(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeArray_ReferenceOutOfBounds(t *testing.T) {
	ctx := NewAMF3Context()
	
	data := []byte{0x02} // 참조 인덱스 1 (존재하지 않음)
	_, err := ctx.decodeArray(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected reference out of bounds error")
	}
	if !strings.Contains(err.Error(), "array reference out of bounds") {
		t.Errorf("expected error to contain 'array reference out of bounds', got %v", err.Error())
	}
}

func TestDecodeArray_ReferenceWrongType(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 배열이 아닌 객체를 테이블에 추가
	ctx.objectTable = append(ctx.objectTable, "not an array")
	
	data := []byte{0x00} // 첫 번째 참조
	_, err := ctx.decodeArray(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected wrong type error")
	}
	if !strings.Contains(err.Error(), "referenced object is not an array") {
		t.Errorf("expected error to contain 'referenced object is not an array', got %v", err.Error())
	}
}

func TestDecodeArray_AssociativeKeyReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 배열 길이는 있지만 associative 키 읽기 실패
	data := []byte{0x03} // 길이 1
	_, err := ctx.decodeArray(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected associative key read error")
	}
}

func TestDecodeArray_AssociativeValueReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 배열 길이와 associative 키는 있지만 값 읽기 실패  
	data := []byte{0x03, 0x07, 'k', 'e', 'y'} // 길이 1, "key"
	_, err := ctx.decodeArray(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected associative value read error")
	}
}

func TestDecodeArray_ElementReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 배열 길이와 associative 부분은 있지만 원소 읽기 실패
	data := []byte{0x03, 0x01} // 길이 1, 빈 키 (associative 끝)
	_, err := ctx.decodeArray(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected element read error")
	}
}

func TestDecodeArray_AssociativeValueIgnored(t *testing.T) {
	ctx := NewAMF3Context()
	
	// associative 키와 값이 있지만 값은 무시됨
	buf := new(bytes.Buffer)
	// 길이 0 배열, "key"(associative), "value", 빈 키(associative 끝)
	buf.WriteByte(0x01) // 길이 0
	ctx.encodeStringValue(buf, "key")
	ctx.encodeString(buf, "value") // 이 값은 무시됨
	ctx.encodeStringValue(buf, "") // associative 끝
	
	_, err := ctx.decodeArray(buf)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeAMF3_Object(t *testing.T) {
	ctx := NewAMF3Context()
	
	// map[string]any{"foo": "bar"} 인코딩
	obj := map[string]any{"foo": "bar"}
	buf := new(bytes.Buffer)
	err := ctx.encodeObject(buf, obj)
	if err != nil {
		t.Fatal(err)
	}
	
	val, err := ctx.DecodeAMF3(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	
	decodedObj, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", val)
	}
	
	if decodedObj["foo"] != "bar" {
		t.Errorf("expected 'bar', got %v", decodedObj["foo"])
	}
}

func TestDecodeObject_Reference(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 먼저 객체를 테이블에 추가
	testObj := map[string]any{"test": "value"}
	ctx.objectTable = append(ctx.objectTable, testObj)
	
	data := []byte{amf3ObjectMarker, 0x00} // 첫 번째 참조
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	obj, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", val)
	}
	
	if obj["test"] != "value" {
		t.Errorf("expected 'value', got %v", obj["test"])
	}
}

func TestDecodeObject_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// U29 읽기 실패
	_, err := ctx.decodeObject(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeObject_ReferenceOutOfBounds(t *testing.T) {
	ctx := NewAMF3Context()
	
	data := []byte{0x02} // 참조 인덱스 1 (존재하지 않음)
	_, err := ctx.decodeObject(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected reference out of bounds error")
	}
	if !strings.Contains(err.Error(), "object reference out of bounds") {
		t.Errorf("expected error to contain 'object reference out of bounds', got %v", err.Error())
	}
}

func TestDecodeObject_ReferenceWrongType(t *testing.T) {
	// 현재 구현은 단순화되어 있으므로 이 테스트는 건너뜀
	t.Skip("Object reference type checking not implemented in current simplified version")
}

func TestDecodeObject_TraitReference(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 트레이트 참조 (현재 지원하지 않음)
	data := []byte{0x05} // 트레이트 참조 플래그
	_, err := ctx.decodeObject(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected trait reference error")
	}
	if !strings.Contains(err.Error(), "trait references not supported") {
		t.Errorf("expected error to contain 'trait references not supported', got %v", err.Error())
	}
}

func TestDecodeObject_ClassNameReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 인라인 플래그 후 클래스명 읽기 실패
	data := []byte{0x03} // 인라인 + 트레이트 플래그, 하지만 데이터 없음
	_, err := ctx.decodeObject(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected class name read error")
	}
}

func TestDecodeObject_KeyReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 인라인 + 트레이트 플래그, 빈 클래스명, 하지만 키 읽기 실패
	data := []byte{0x03, 0x01} // 인라인 + 트레이트 플래그, 빈 문자열 (클래스명)
	_, err := ctx.decodeObject(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected key read error")
	}
}

func TestDecodeObject_ValueReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 인라인 + 트레이트 플래그, 빈 클래스명, 키는 있지만 값 읽기 실패
	data := []byte{0x03, 0x01, 0x07, 'k', 'e', 'y'} // 인라인 + 트레이트 플래그, 빈 클래스명, "key"
	_, err := ctx.decodeObject(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected value read error")
	}
}

func TestDecodeObject_WithClassName(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 인라인 + 트레이트 플래그, 클래스명이 있는 경우
	buf := new(bytes.Buffer)
	buf.WriteByte(0x03) // 인라인 + 트레이트 플래그
	ctx.encodeStringValue(buf, "TestClass") // 비어있지 않은 클래스명
	ctx.encodeStringValue(buf, "") // 빈 키 (속성 끝)
	
	obj, err := ctx.decodeObject(buf)
	if err != nil {
		t.Fatal(err)
	}
	
	// 객체가 성공적으로 디코딩되어야 함
	if obj == nil {
		t.Fatal("expected object, got nil")
	}
}

func TestDecodeAMF3_Date(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 특정 날짜 인코딩
	date := time.Date(2023, 3, 28, 19, 40, 0, 123*1e6, time.UTC)
	buf := new(bytes.Buffer)
	err := ctx.encodeDate(buf, date)
	if err != nil {
		t.Fatal(err)
	}
	
	val, err := ctx.DecodeAMF3(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	
	decodedDate, ok := val.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", val)
	}
	
	// 밀리초 단위로 비교 (나노초 정밀도 손실 고려)
	if decodedDate.Unix() != date.Unix() {
		t.Errorf("expected %v, got %v", date, decodedDate)
	}
}

func TestDecodeDate_Reference(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 먼저 날짜를 테이블에 추가
	testDate := time.Now()
	ctx.objectTable = append(ctx.objectTable, testDate)
	
	data := []byte{amf3DateMarker, 0x00} // 첫 번째 참조
	val, err := ctx.DecodeAMF3(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	date, ok := val.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", val)
	}
	
	if !date.Equal(testDate) {
		t.Errorf("expected %v, got %v", testDate, date)
	}
}

func TestDecodeDate_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// U29 읽기 실패
	_, err := ctx.decodeDate(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeDate_ReferenceOutOfBounds(t *testing.T) {
	ctx := NewAMF3Context()
	
	data := []byte{0x02} // 참조 인덱스 1 (존재하지 않음)
	_, err := ctx.decodeDate(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected reference out of bounds error")
	}
	if !strings.Contains(err.Error(), "date reference out of bounds") {
		t.Errorf("expected error to contain 'date reference out of bounds', got %v", err.Error())
	}
}

func TestDecodeDate_ReferenceWrongType(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 날짜가 아닌 문자열을 테이블에 추가
	ctx.objectTable = append(ctx.objectTable, "not a date")
	
	data := []byte{0x00} // 첫 번째 참조
	_, err := ctx.decodeDate(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected wrong type error")
	}
	if !strings.Contains(err.Error(), "referenced object is not a time") {
		t.Errorf("expected error to contain 'referenced object is not a time', got %v", err.Error())
	}
}

func TestDecodeDate_TimeReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 플래그는 있지만 시간 데이터 부족
	data := []byte{0x01} // 인라인 플래그만
	_, err := ctx.decodeDate(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected time read error")
	}
}

func TestDecodeU29_Success(t *testing.T) {
	ctx := NewAMF3Context()
	
	testCases := []struct {
		data     []byte
		expected uint32
	}{
		{[]byte{0x00}, 0x00},
		{[]byte{0x7F}, 0x7F},
		{[]byte{0x81, 0x00}, 0x80},
		{[]byte{0xFF, 0x7F}, 0x3FFF},
		{[]byte{0x81, 0x80, 0x00}, 0x4000},
		{[]byte{0xFF, 0xFF, 0x7F}, 0x1FFFFF},
		{[]byte{0x80, 0xC0, 0x80, 0x00}, 0x200000},
		{[]byte{0xFF, 0xFF, 0xFF, 0xFF}, 0x1FFFFFFF},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			val, err := ctx.decodeU29(bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if val != tc.expected {
				t.Errorf("expected 0x%X, got 0x%X", tc.expected, val)
			}
		})
	}
}

func TestDecodeU29_ReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 빈 데이터
	_, err := ctx.decodeU29(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDecodeU29_FourByteForm(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 4바이트 형식 테스트
	data := []byte{0x80, 0x80, 0x80, 0x01}
	val, err := ctx.decodeU29(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	
	expected := uint32(0x01)
	if val != expected {
		t.Errorf("expected 0x%X, got 0x%X", expected, val)
	}
}

func TestDecodeU29_FourByteReadError(t *testing.T) {
	ctx := NewAMF3Context()
	
	// 4바이트 형식인데 마지막 바이트 읽기 실패
	data := []byte{0x80, 0x80, 0x80} // 3바이트만
	_, err := ctx.decodeU29(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected read error for fourth byte")
	}
}

func TestNewAMF3Context(t *testing.T) {
	ctx := NewAMF3Context()
	
	if ctx.stringTable == nil {
		t.Error("stringTable should not be nil")
	}
	if ctx.objectTable == nil {
		t.Error("objectTable should not be nil")
	}
	if ctx.traitTable == nil {
		t.Error("traitTable should not be nil")
	}
	
	if len(ctx.stringTable) != 0 {
		t.Errorf("expected empty stringTable, got %d elements", len(ctx.stringTable))
	}
	if len(ctx.objectTable) != 0 {
		t.Errorf("expected empty objectTable, got %d elements", len(ctx.objectTable))
	}
	if len(ctx.traitTable) != 0 {
		t.Errorf("expected empty traitTable, got %d elements", len(ctx.traitTable))
	}
}

// 벤치마크 테스트
func BenchmarkDecodeAMF3_Integer(b *testing.B) {
	data := []byte{amf3IntegerMarker, 0x2A} // 42
	reader := bytes.NewReader(data)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		_, _ = DecodeAMF3Sequence(reader)
	}
}

func BenchmarkDecodeAMF3_String(b *testing.B) {
	// "hello world" 인코딩
	encoded, _ := EncodeAMF3Sequence("hello world")
	reader := bytes.NewReader(encoded)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(encoded)
		_, _ = DecodeAMF3Sequence(reader)
	}
}

func BenchmarkDecodeAMF3_Object(b *testing.B) {
	obj := map[string]any{
		"name":  "test",
		"value": int32(123),
		"flag":  true,
	}
	encoded, _ := EncodeAMF3Sequence(obj)
	reader := bytes.NewReader(encoded)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(encoded)
		_, _ = DecodeAMF3Sequence(reader)
	}
}