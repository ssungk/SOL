package amf

import (
	"io"
)

// AMF0 Type Markers
const (
	numberMarker      = 0x00
	booleanMarker     = 0x01
	stringMarker      = 0x02
	objectMarker      = 0x03
	movieClipMarker   = 0x04 // Not supported
	nullMarker        = 0x05
	undefinedMarker   = 0x06
	referenceMarker   = 0x07
	ecmaArrayMarker   = 0x08
	objectEndMarker   = 0x09
	strictArrayMarker = 0x0A
	dateMarker        = 0x0B
	longStringMarker  = 0x0C
	unsupportedMarker = 0x0D
	xmlDocumentMarker = 0x0F
	typedObjectMarker = 0x10
	avmPlusMarker     = 0x11 // AMF3
)

// AMF3 Type Markers
const (
	amf3UndefinedMarker = 0x00
	amf3NullMarker      = 0x01
	amf3FalseMarker     = 0x02
	amf3TrueMarker      = 0x03
	amf3IntegerMarker   = 0x04
	amf3DoubleMarker    = 0x05
	amf3StringMarker    = 0x06
	amf3XMLDocMarker    = 0x07
	amf3DateMarker      = 0x08
	amf3ArrayMarker     = 0x09
	amf3ObjectMarker    = 0x0A
	amf3XMLMarker       = 0x0B
	amf3ByteArrayMarker = 0x0C
)

// AMF3Context holds the state for a single AMF3 encoding or decoding session,
// managing reference tables for strings and complex objects.
type AMF3Context struct {
	stringTable    []string
	objectTable    []any
	traitTable     []any // Traits are not fully supported in this simplified version
	stringTableMap map[string]int
}

// NewAMF3Context creates and initializes a new AMF3Context.
func NewAMF3Context() *AMF3Context {
	return &AMF3Context{
		stringTable:    make([]string, 0),
		objectTable:    make([]any, 0),
		traitTable:     make([]any, 0),
		stringTableMap: make(map[string]int),
	}
}

// readByte reads a single byte from the reader.
func readByte(r io.Reader) (byte, error) {
	buf := make([]byte, 1)
	_, err := io.ReadFull(r, buf)
	return buf[0], err
}

// readBytes reads n bytes from the reader.
func readBytes(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

// writeByte writes a single byte to the writer.
func writeByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}
