package srt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// EncryptionType 암호화 타입 (constants.go에서 정의된 상수 사용)
type EncryptionType int

// SRTEncryption SRT 암호화 구조체
type SRTEncryption struct {
	encryptionType EncryptionType
	passphrase     string
	key            []byte
	gcm            cipher.AEAD
	enabled        bool
}

// NewSRTEncryption 새 SRT 암호화 인스턴스 생성
func NewSRTEncryption(encType EncryptionType, passphrase string) (*SRTEncryption, error) {
	enc := &SRTEncryption{
		encryptionType: encType,
		passphrase:     passphrase,
		enabled:        encType != EncryptionNone && passphrase != "",
	}

	if !enc.enabled {
		return enc, nil
	}

	// 패스워드에서 키 생성
	if err := enc.generateKey(); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	// AES-GCM 초기화
	if err := enc.initializeGCM(); err != nil {
		return nil, fmt.Errorf("failed to initialize AES-GCM: %w", err)
	}

	return enc, nil
}

// generateKey 패스워드에서 암호화 키 생성
func (e *SRTEncryption) generateKey() error {
	var keySize int
	switch e.encryptionType {
	case EncryptionAES128:
		keySize = 16
	case EncryptionAES192:
		keySize = 24
	case EncryptionAES256:
		keySize = 32
	default:
		return fmt.Errorf("unsupported encryption type: %d", e.encryptionType)
	}

	// SHA-256을 사용하여 패스워드에서 키 생성
	hash := sha256.Sum256([]byte(e.passphrase))
	e.key = hash[:keySize]

	return nil
}

// initializeGCM AES-GCM 암호화 초기화
func (e *SRTEncryption) initializeGCM() error {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	e.gcm, err = cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	return nil
}

// IsEnabled 암호화가 활성화되어 있는지 확인
func (e *SRTEncryption) IsEnabled() bool {
	return e.enabled
}

// GetEncryptionType 암호화 타입 반환
func (e *SRTEncryption) GetEncryptionType() EncryptionType {
	return e.encryptionType
}

// Encrypt 데이터 암호화
func (e *SRTEncryption) Encrypt(plaintext []byte) ([]byte, error) {
	if !e.enabled {
		return plaintext, nil
	}

	// 논스 생성 (GCM은 일반적으로 12바이트 논스를 사용)
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 암호화 수행
	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)
	
	return ciphertext, nil
}

// Decrypt 데이터 복호화
func (e *SRTEncryption) Decrypt(ciphertext []byte) ([]byte, error) {
	if !e.enabled {
		return ciphertext, nil
	}

	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// 논스와 실제 암호문 분리
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	// 복호화 수행
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// EncryptPacket SRT 패킷 암호화
func (e *SRTEncryption) EncryptPacket(packet *UDTPacket) error {
	if !e.enabled || len(packet.Payload) == 0 {
		return nil
	}

	// 페이로드만 암호화 (헤더는 평문으로 유지)
	encryptedPayload, err := e.Encrypt(packet.Payload)
	if err != nil {
		return fmt.Errorf("failed to encrypt packet payload: %w", err)
	}

	packet.Payload = encryptedPayload
	return nil
}

// DecryptPacket SRT 패킷 복호화
func (e *SRTEncryption) DecryptPacket(packet *UDTPacket) error {
	if !e.enabled || len(packet.Payload) == 0 {
		return nil
	}

	// 페이로드 복호화
	decryptedPayload, err := e.Decrypt(packet.Payload)
	if err != nil {
		return fmt.Errorf("failed to decrypt packet payload: %w", err)
	}

	packet.Payload = decryptedPayload
	return nil
}

// ValidateKey 키 유효성 검증
func (e *SRTEncryption) ValidateKey() error {
	if !e.enabled {
		return nil
	}

	if len(e.key) == 0 {
		return fmt.Errorf("encryption key not generated")
	}

	var expectedKeySize int
	switch e.encryptionType {
	case EncryptionAES128:
		expectedKeySize = 16
	case EncryptionAES192:
		expectedKeySize = 24
	case EncryptionAES256:
		expectedKeySize = 32
	default:
		return fmt.Errorf("unsupported encryption type")
	}

	if len(e.key) != expectedKeySize {
		return fmt.Errorf("invalid key size: expected %d, got %d", expectedKeySize, len(e.key))
	}

	return nil
}

// GetKeySize 키 크기 반환
func (e *SRTEncryption) GetKeySize() int {
	return len(e.key)
}

// ParseEncryptionType 문자열을 암호화 타입으로 변환
func ParseEncryptionType(encStr string) EncryptionType {
	switch encStr {
	case "aes128":
		return EncryptionAES128
	case "aes192":
		return EncryptionAES192
	case "aes256":
		return EncryptionAES256
	case "none", "":
		return EncryptionNone
	default:
		return EncryptionNone
	}
}

// EncryptionTypeToString 암호화 타입을 문자열로 변환
func EncryptionTypeToString(encType EncryptionType) string {
	switch encType {
	case EncryptionAES128:
		return "aes128"
	case EncryptionAES192:
		return "aes192"
	case EncryptionAES256:
		return "aes256"
	case EncryptionNone:
		return "none"
	default:
		return "none"
	}
}