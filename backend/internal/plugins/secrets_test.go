package plugins

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/cryptoutil"
)

func TestPluginSecretEncryptionUsesVersionedKDFAndReadsLegacyCiphertext(t *testing.T) {
	const secretKey = "test-master-secret"
	const plaintext = "plugin-secret"

	ciphertext, err := encryptSecret(secretKey, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ciphertext, "v2:") {
		t.Fatalf("ciphertext is not versioned: %q", ciphertext)
	}
	decrypted, err := decryptSecret(secretKey, ciphertext)
	if err != nil || decrypted != plaintext {
		t.Fatalf("decryptSecret(v2) = %q, %v", decrypted, err)
	}

	legacy := sealLegacyPluginSecret(t, secretKey, plaintext)
	decrypted, err = decryptSecret(secretKey, legacy)
	if err != nil || decrypted != plaintext {
		t.Fatalf("decryptSecret(legacy) = %q, %v", decrypted, err)
	}
}

func sealLegacyPluginSecret(t *testing.T, secretKey, plaintext string) string {
	t.Helper()
	block, err := aes.NewCipher(cryptoutil.LegacySHA256Key(secretKey))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed)
}
