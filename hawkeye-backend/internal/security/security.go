package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

const passwordPrefix = "sha256"

func NewToken(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func HashPassword(password string) string {
	salt := NewToken(16)
	sum := passwordDigest(password, salt)
	return fmt.Sprintf("%s$%s$%s", passwordPrefix, salt, sum)
}

func VerifyPassword(stored string, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != passwordPrefix {
		return subtle.ConstantTimeCompare([]byte(stored), []byte(password)) == 1
	}
	sum := passwordDigest(password, parts[1])
	return subtle.ConstantTimeCompare([]byte(sum), []byte(parts[2])) == 1
}

func IsHashedPassword(value string) bool {
	return strings.HasPrefix(value, passwordPrefix+"$")
}

func passwordDigest(password string, salt string) string {
	data := []byte(salt + ":" + password)
	for i := 0; i < 120000; i++ {
		sum := sha256.Sum256(data)
		data = sum[:]
	}
	return hex.EncodeToString(data)
}
