// Package auth provides credential parsing and validation for the
// manager's HTTP Basic Auth middleware. The functions in this package
// are pure (no HTTP coupling) so they can be tested independently.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

// ValidateBasicAuth parses an RFC 7617 "Basic" Authorization header
// value and checks the supplied credentials against the expected
// username and password. It returns true only when the header is
// well-formed and both the username and password match.
//
// The comparison is performed on SHA-256 hashes of the values so that
// the execution time does not leak the length of either operand. This
// mirrors the Bun reference implementation, which calls
// timingSafeEqual on equal-length buffers to reduce timing
// differences.
//
// Splitting rules (matching the Bun behaviour):
//   - The header is split on spaces; the first token must be exactly
//     "Basic" (case-sensitive) and the second token is the base64
//     payload.
//   - The decoded payload is split on the FIRST colon only, so
//     passwords may contain colons.
func ValidateBasicAuth(headerValue, expectedUsername, expectedPassword string) bool {
	if headerValue == "" {
		return false
	}
	parts := strings.Split(headerValue, " ")
	if len(parts) < 2 {
		return false
	}
	scheme := parts[0]
	encoded := parts[1]
	if scheme != "Basic" || encoded == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	idx := strings.IndexByte(string(decoded), ':')
	if idx == -1 {
		return false
	}
	username := string(decoded[:idx])
	password := string(decoded[idx+1:])
	return constantTimeEqual(username, expectedUsername) &&
		constantTimeEqual(password, expectedPassword)
}

// constantTimeEqual compares two strings in constant time by hashing
// both to a fixed-length buffer (SHA-256) and then using
// subtle.ConstantTimeCompare. This avoids length-based timing leaks
// regardless of the input lengths.
func constantTimeEqual(a, b string) bool {
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1
}
