// 카메라 비밀번호 등 민감 정보를 AES-GCM으로 암호화/복호화한다.
// 마스터 키는 환경변수(WEBNVR_MASTER_KEY)에서 읽고, 카메라별 고유 키는 PBKDF2로 파생한다.
package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// EnvMasterKey는 마스터 키를 담는 환경변수 이름이다.
const EnvMasterKey = "WEBNVR_MASTER_KEY"

// EncryptedPrefix는 암호화된 값임을 표시하는 접두어다.
const EncryptedPrefix = "encrypted:"

// KeySource는 현재 마스터 키의 출처다.
type KeySource string

const (
	KeySourceEnv      KeySource = "env"      // 환경변수
	KeySourceFile     KeySource = "file"     // 키 파일 (config/.masterkey)
	KeySourceFallback KeySource = "fallback" // 개발용 폴백 (레거시)
)

const pbkdf2Iterations = 100_000

var errNotEncrypted = errors.New("암호화된 형식이 아님 (encrypted: 접두어 없음)")

// sessionKey는 App 기동 시 설정되는 세션 키다 (파일 또는 마이그레이션 결과).
var (
	sessionKey   []byte
	sessionSrc   KeySource
	fallbackWarn bool
)

// SetSessionKey는 세션 마스터 키를 설정한다. (App 기동 시 1회)
func SetSessionKey(key []byte, src KeySource) {
	sessionKey = key
	sessionSrc = src
}

// KeySourceOf는 현재 적용 중인 키 출처를 반환한다.
func KeySourceOf() KeySource {
	if os.Getenv(EnvMasterKey) != "" {
		return KeySourceEnv
	}
	if sessionKey != nil {
		return sessionSrc
	}
	return KeySourceFallback
}

// normalizeKey는 임의 키 문자열을 32바이트로 정규화한다.
func normalizeKey(v string) []byte {
	if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
		return b
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == 32 {
		return b
	}
	// 32바이트가 아닌 임의 문자열은 SHA-256으로 정규화한다.
	k := sha256.Sum256([]byte(v))
	return k[:]
}

// MasterKey는 현재 적용할 마스터 키를 반환한다.
// 우선순위: 환경변수 > 세션 키(키 파일/마이그레이션) > 개발용 폴백.
func MasterKey() ([]byte, error) {
	if v := os.Getenv(EnvMasterKey); v != "" {
		return normalizeKey(v), nil
	}
	if sessionKey != nil {
		return sessionKey, nil
	}
	if !fallbackWarn {
		fallbackWarn = true
		slog.Warn("마스터 키가 설정되지 않아 개발용 폴백 키 사용", "env", EnvMasterKey)
	}
	k := sha256.Sum256([]byte("webnvr-dev-fallback-key"))
	return k[:], nil
}

// derivedKey는 카메라 ID를 솔트로 사용해 카메라별 AES-256 키를 파생한다.
func derivedKey(master []byte, cameraID string) ([]byte, error) {
	salt := sha256.Sum256([]byte("webnvr:camera:" + cameraID))
	return pbkdf2.Key(sha256.New, string(master), salt[:], pbkdf2Iterations, 32)
}

// EncryptSecret은 평문을 AES-256-GCM으로 암호화해 "encrypted:<base64>" 형태로 반환한다.
func EncryptSecret(cameraID, plaintext string) (string, error) {
	master, err := MasterKey()
	if err != nil {
		return "", fmt.Errorf("마스터 키 조회 실패: %w", err)
	}
	key, err := derivedKey(master, cameraID)
	if err != nil {
		return "", fmt.Errorf("카메라 키 파생 실패: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncryptedPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptSecret은 "encrypted:<base64>" 값을 복호화해 평문을 반환한다.
// 접두어가 없는 값(구버전 평문)은 그대로 반환한다.
func DecryptSecret(cameraID, stored string) (string, error) {
	if !strings.HasPrefix(stored, EncryptedPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, EncryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("암호문 디코딩 실패: %w", err)
	}

	master, err := MasterKey()
	if err != nil {
		return "", fmt.Errorf("마스터 키 조회 실패: %w", err)
	}
	key, err := derivedKey(master, cameraID)
	if err != nil {
		return "", fmt.Errorf("카메라 키 파생 실패: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errNotEncrypted
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("복호화 실패 (키 불일치 또는 손상): %w", err)
	}
	return string(pt), nil
}
