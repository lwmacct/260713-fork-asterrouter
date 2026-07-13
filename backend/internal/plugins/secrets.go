package plugins

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/cryptoutil"
)

func (s *Service) decryptConfigSecret(record configRecord, key string) (string, error) {
	ciphertext := strings.TrimSpace(record.SecretCiphertexts[key])
	if ciphertext == "" {
		return "", nil
	}
	return decryptSecret(s.secretKey, ciphertext)
}

func encryptSecret(secretKey string, value string) (string, error) {
	key, err := cryptoutil.DeriveKey(secretKey, "asterrouter:plugins:secret-encryption:v2")
	if err != nil {
		return "", err
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
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(value), nil)
	return "v2:" + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptSecret(secretKey string, encoded string) (string, error) {
	var key []byte
	if strings.HasPrefix(encoded, "v2:") {
		encoded = strings.TrimPrefix(encoded, "v2:")
		derivedKey, err := cryptoutil.DeriveKey(secretKey, "asterrouter:plugins:secret-encryption:v2")
		if err != nil {
			return "", err
		}
		key = derivedKey
	} else {
		key = cryptoutil.LegacySHA256Key(secretKey)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
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
		return "", errors.New("encrypted plugin secret is invalid")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		return strings.Repeat("*", len(value))
	}
	return value[:6] + "..." + value[len(value)-4:]
}
