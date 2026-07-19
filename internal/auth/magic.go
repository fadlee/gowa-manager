// Package auth provides credential parsing and validation utilities for
// the manager's HTTP authentication middleware. The magic admin link
// feature produces short-lived HMAC-signed tokens that allow opening a
// proxied GOWA instance's admin UI without a browser Basic Auth prompt.
//
// This is a Go port of the Bun reference implementation in
// src/modules/proxy/magic-auth.ts. The token format, cookie attributes,
// and secret derivation are preserved so that links behave identically
// during the cutover period.
//
// Tokens are intentionally short-lived (60 seconds) and do NOT survive a
// manager restart when the secret is the runtime default — the nonce is
// random and the secret is derived from env vars at construction time.
// Key rotation is performed by changing the ADMIN_LINK_SECRET (or
// ADMIN_PASSWORD) env var and restarting the manager; tokens signed with
// the previous secret will fail validation against the new secret.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	tokenTTLSeconds  = 60
	cookieTTLSeconds = 15 * 60
	cookiePrefix     = "gowa_admin_auth_"
	defaultSecret    = "gowa-manager-runtime-admin-link-secret"
)

// magicPayload is the JSON payload encoded inside a magic admin token.
// Field names match the Bun reference so the wire format is identical.
type magicPayload struct {
	InstanceKey string `json:"instanceKey"`
	Exp         int64  `json:"exp"`
	Nonce       string `json:"nonce"`
}

// MagicAuthService holds the signing secret and provides token/cookie
// utilities for the magic admin link feature. The secret is resolved
// once at construction time (via NewMagicAuthService or
// NewMagicAuthServiceWithSecret) so that key rotation requires creating
// a new service instance.
type MagicAuthService struct {
	secret string
}

// NewMagicAuthService creates a MagicAuthService whose secret is derived
// from the environment, matching the Bun getSecret() precedence:
// ADMIN_LINK_SECRET → ADMIN_PASSWORD → built-in default.
func NewMagicAuthService() *MagicAuthService {
	return &MagicAuthService{secret: resolveSecretFromEnv()}
}

// NewMagicAuthServiceWithSecret creates a MagicAuthService with an
// explicit secret. This is primarily useful for tests.
func NewMagicAuthServiceWithSecret(secret string) *MagicAuthService {
	return &MagicAuthService{secret: secret}
}

// resolveSecretFromEnv mirrors the Bun getSecret() precedence.
func resolveSecretFromEnv() string {
	if s := os.Getenv("ADMIN_LINK_SECRET"); s != "" {
		return s
	}
	if s := os.Getenv("ADMIN_PASSWORD"); s != "" {
		return s
	}
	return defaultSecret
}

// CookieName returns the cookie name used to persist a magic admin
// token for the given instance key.
func (s *MagicAuthService) CookieName(instanceKey string) string {
	return cookiePrefix + instanceKey
}

// sign computes the base64url-encoded HMAC-SHA256 signature of the
// given payload string using the service's secret.
func (s *MagicAuthService) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// safeEqual compares two strings in constant time. It returns false
// immediately when the lengths differ (matching the Bun behaviour of
// guarding timingSafeEqual behind a length check).
func safeEqual(a, b string) bool {
	if len(a) != len(b) {
		// Compare b against itself to keep the timing roughly
		// independent of the relationship between a and b.
		subtle.ConstantTimeCompare([]byte(b), []byte(b))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CreateToken builds a signed magic admin token for the given instance
// key. The token expires tokenTTLSeconds after now. The returned
// expiresAt is the absolute expiry instant.
func (s *MagicAuthService) CreateToken(instanceKey string, now time.Time) (token string, expiresAt time.Time) {
	expiresAt = now.Add(tokenTTLSeconds * time.Second)
	payload := magicPayload{
		InstanceKey: instanceKey,
		Exp:         expiresAt.UnixMilli(),
		Nonce:       randomNonce(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(encoded)
	return encodedPayload + "." + s.sign(string(encodedPayload)), expiresAt
}

// ValidateToken verifies a magic admin token against the given instance
// key at the supplied time. It returns true only when the signature is
// valid (constant-time comparison), the payload decodes, the instance
// key matches, and the token has not expired (exp > now, strict).
func (s *MagicAuthService) ValidateToken(token, instanceKey string, now time.Time) bool {
	if token == "" {
		return false
	}
	// Match Bun's token.split('.') destructuring: take the first two
	// parts and ignore any trailing parts.
	parts := strings.Split(token, ".")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	encodedPayload, signature := parts[0], parts[1]
	if !safeEqual(signature, s.sign(encodedPayload)) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return false
	}
	var payload magicPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return payload.InstanceKey == instanceKey && payload.Exp > now.UnixMilli()
}

// CreateCookie builds a Set-Cookie header value that persists the given
// token for the instance. When maxAgeSeconds is <= 0 the default
// cookieTTLSeconds is used. The Secure attribute is added only when the
// request URL scheme is https.
func (s *MagicAuthService) CreateCookie(instanceKey, token, requestURL string, maxAgeSeconds int) string {
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = cookieTTLSeconds
	}
	secure := ""
	if isHTTPS(requestURL) {
		secure = "; Secure"
	}
	return s.CookieName(instanceKey) + "=" + url.QueryEscape(token) +
		"; Max-Age=" + strconv.Itoa(maxAgeSeconds) +
		"; Path=/app/" + instanceKey +
		"; HttpOnly; SameSite=Lax" + secure
}

// ClearCookie builds a Set-Cookie header value that deletes the magic
// admin cookie for the given instance. The Secure attribute is added
// only when the request URL scheme is https.
func (s *MagicAuthService) ClearCookie(instanceKey, requestURL string) string {
	secure := ""
	if isHTTPS(requestURL) {
		secure = "; Secure"
	}
	return s.CookieName(instanceKey) + "=; Max-Age=0" +
		"; Path=/app/" + instanceKey +
		"; HttpOnly; SameSite=Lax" + secure
}

// HasValidCookie parses the cookie header and validates the magic admin
// cookie for the given instance key at the supplied time.
func (s *MagicAuthService) HasValidCookie(cookieHeader, instanceKey string, now time.Time) bool {
	cookies := parseCookies(cookieHeader)
	return s.ValidateToken(cookies[s.CookieName(instanceKey)], instanceKey, now)
}

// parseCookies parses a Cookie request header into a map. It splits on
// ';', trims each part, splits on the first '=', URL-decodes the value,
// and skips parts without an '='. This matches the Bun reference
// implementation exactly.
func parseCookies(cookieHeader string) map[string]string {
	cookies := make(map[string]string)
	if cookieHeader == "" {
		return cookies
	}
	for _, part := range strings.Split(cookieHeader, ";") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx == -1 {
			continue
		}
		name := trimmed[:idx]
		if name == "" {
			continue
		}
		rawValue := trimmed[idx+1:]
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			// Bun's decodeURIComponent would throw on invalid sequences;
			// the surrounding try/catch in the caller would then treat
			// the cookie as absent. Skip the malformed value here.
			continue
		}
		cookies[name] = value
	}
	return cookies
}

// isHTTPS reports whether the request URL uses the https scheme. It
// returns false on parse errors (matching the Bun reference).
func isHTTPS(requestURL string) bool {
	u, err := url.Parse(requestURL)
	if err != nil {
		return false
	}
	return u.Scheme == "https"
}

// randomNonce returns a base64url-encoded 12-byte random nonce. On the
// unlikely failure of the system PRNG it returns an empty nonce, which
// still yields a valid (if less random) token.
func randomNonce() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
