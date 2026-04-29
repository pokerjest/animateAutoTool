package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// RandomHex returns a hex-encoded cryptographically secure string with byteLen bytes of entropy.
func RandomHex(byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", fmt.Errorf("byteLen must be positive")
	}

	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

// RandomPassword returns a cryptographically secure password using an ASCII-only alphabet.
func RandomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be positive")
	}

	out := make([]byte, length)
	alphabetLen := byte(len(passwordAlphabet))
	maxValid := byte((256 / int(alphabetLen)) * int(alphabetLen))

	for i := 0; i < length; {
		var single [1]byte
		if _, err := rand.Read(single[:]); err != nil {
			return "", err
		}
		if single[0] >= maxValid {
			continue
		}
		out[i] = passwordAlphabet[int(single[0]%alphabetLen)]
		i++
	}

	return string(out), nil
}
