package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// newTestService builds a MagicAuthService with a fixed secret so tests
// are deterministic and do not depend on process environment.
func newTestService(secret string) *MagicAuthService {
	return &MagicAuthService{secret: secret}
}

// decodePayload extracts the JSON payload from a token for assertions.
func decodePayload(t *testing.T, token string) magicPayload {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) < 1 {
		t.Fatal("token has no payload part")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("payload base64 decode: %v", err)
	}
	var p magicPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("payload json decode: %v", err)
	}
	return p
}

// ---- Token creation / signing ----

func TestMagic_CreateTokenFormat(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, expiresAt := svc.CreateToken("inst1", now)

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts separated by '.', got %d", len(parts))
	}
	if parts[0] == "" || parts[1] == "" {
		t.Fatal("payload and signature must be non-empty")
	}

	// Expiry is 60 seconds after now.
	expected := now.Add(tokenTTLSeconds * time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expiry %v, got %v", expected, expiresAt)
	}

	// Payload decodes and has expected fields.
	payload := decodePayload(t, token)
	if payload.InstanceKey != "inst1" {
		t.Fatalf("expected instanceKey inst1, got %q", payload.InstanceKey)
	}
	if payload.Exp != expected.UnixMilli() {
		t.Fatalf("expected exp %d, got %d", expected.UnixMilli(), payload.Exp)
	}
	if payload.Nonce == "" {
		t.Fatal("expected non-empty nonce")
	}
}

func TestMagic_TokenSignatureIsHMAC(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)

	parts := strings.Split(token, ".")
	// Recompute the signature over the payload part and compare.
	expectedSig := svc.sign(parts[0])
	if parts[1] != expectedSig {
		t.Fatalf("signature mismatch: expected %q, got %q", expectedSig, parts[1])
	}
}

func TestMagic_NonceIsRandom(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token1, _ := svc.CreateToken("inst1", now)
	token2, _ := svc.CreateToken("inst1", now)
	if token1 == token2 {
		t.Fatal("expected different nonces to produce different tokens")
	}
}

// ---- Instance binding ----

func TestMagic_InstanceBinding(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	if !svc.ValidateToken(token, "inst1", now) {
		t.Fatal("expected token to validate for its own instance key")
	}
	if svc.ValidateToken(token, "inst2", now) {
		t.Fatal("expected token to fail for a different instance key")
	}
}

// ---- Expiry boundary ----

func TestMagic_ExpiryBoundary(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, expiresAt := svc.CreateToken("inst1", now)

	// Valid up to (but not including) the expiry instant.
	if !svc.ValidateToken(token, "inst1", expiresAt.Add(-1*time.Millisecond)) {
		t.Fatal("expected token valid 1ms before expiry")
	}
	if svc.ValidateToken(token, "inst1", expiresAt) {
		t.Fatal("expected token invalid at exact expiry (exp > now is strict)")
	}
	if svc.ValidateToken(token, "inst1", expiresAt.Add(1*time.Millisecond)) {
		t.Fatal("expected token invalid after expiry")
	}
}

// ---- Tampering ----

func TestMagic_TamperedPayload(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)

	parts := strings.Split(token, ".")
	// Flip a character in the payload.
	tampered := parts[0]
	if tampered[0] == 'A' {
		tampered = "B" + tampered[1:]
	} else {
		tampered = "A" + tampered[1:]
	}
	bad := tampered + "." + parts[1]
	if svc.ValidateToken(bad, "inst1", now) {
		t.Fatal("expected tampered payload to fail validation")
	}
}

func TestMagic_TamperedSignature(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)

	parts := strings.Split(token, ".")
	// Flip a character in the signature.
	tampered := parts[1]
	if tampered[0] == 'A' {
		tampered = "B" + tampered[1:]
	} else {
		tampered = "A" + tampered[1:]
	}
	bad := parts[0] + "." + tampered
	if svc.ValidateToken(bad, "inst1", now) {
		t.Fatal("expected tampered signature to fail validation")
	}
}

// ---- Malformed tokens ----

func TestMagic_EmptyToken(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if svc.ValidateToken("", "inst1", now) {
		t.Fatal("expected empty token to fail")
	}
}

func TestMagic_NoDot(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if svc.ValidateToken("nodothere", "inst1", now) {
		t.Fatal("expected token without dot to fail")
	}
}

func TestMagic_ExtraDots(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	// Append an extra ".extra" — JS split takes first two parts, so the
	// signature is the original second part and should still validate.
	// However the Bun reference destructures [payload, signature] from
	// split('.'), ignoring trailing parts. Match that: extra dots after
	// the signature are ignored and the token still validates.
	if !svc.ValidateToken(token+".extra", "inst1", now) {
		t.Fatal("expected extra trailing dot parts to be ignored (JS split semantics)")
	}
}

func TestMagic_InvalidBase64Payload(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// "!!!" is not valid base64url; signature is irrelevant but provide one.
	sig := svc.sign("!!!")
	if svc.ValidateToken("!!!."+sig, "inst1", now) {
		t.Fatal("expected invalid base64 payload to fail")
	}
}

func TestMagic_InvalidJSONPayload(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Valid base64url but not valid JSON.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	sig := svc.sign(encoded)
	if svc.ValidateToken(encoded+"."+sig, "inst1", now) {
		t.Fatal("expected invalid JSON payload to fail")
	}
}

// ---- Cookie name ----

func TestMagic_CookieName(t *testing.T) {
	svc := newTestService("test-secret")
	if got := svc.CookieName("inst1"); got != "gowa_admin_auth_inst1" {
		t.Fatalf("expected gowa_admin_auth_inst1, got %q", got)
	}
}

// ---- Cookie attributes ----

func TestMagic_CreateCookieAttributes(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	cookie := svc.CreateCookie("inst1", token, "http://localhost/app/inst1/", 0)

	if !strings.HasPrefix(cookie, "gowa_admin_auth_inst1=") {
		t.Fatalf("cookie must start with name=, got %q", cookie)
	}
	if !strings.Contains(cookie, "Path=/app/inst1") {
		t.Fatalf("expected Path=/app/inst1, got %q", cookie)
	}
	if !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("expected HttpOnly, got %q", cookie)
	}
	if !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("expected SameSite=Lax, got %q", cookie)
	}
	if strings.Contains(cookie, "Secure") {
		t.Fatalf("expected no Secure over http, got %q", cookie)
	}
}

func TestMagic_CreateCookieSecureOverHttps(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	cookie := svc.CreateCookie("inst1", token, "https://example.com/app/inst1/", 0)
	if !strings.Contains(cookie, "Secure") {
		t.Fatalf("expected Secure over https, got %q", cookie)
	}
}

func TestMagic_CreateCookieMaxAge(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	cookie := svc.CreateCookie("inst1", token, "http://localhost/app/inst1/", cookieTTLSeconds)
	if !strings.Contains(cookie, "Max-Age=900") {
		t.Fatalf("expected Max-Age=900, got %q", cookie)
	}
}

func TestMagic_CreateCookieDefaultMaxAge(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	// maxAgeSeconds <= 0 should default to cookieTTLSeconds.
	cookie := svc.CreateCookie("inst1", token, "http://localhost/app/inst1/", 0)
	if !strings.Contains(cookie, "Max-Age=900") {
		t.Fatalf("expected default Max-Age=900, got %q", cookie)
	}
}

func TestMagic_CreateCookieValueURLEncoded(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	cookie := svc.CreateCookie("inst1", token, "http://localhost/app/inst1/", 0)
	// The token contains a '.' which is safe in cookies, but the value
	// must be URL-encoded (matching Bun's encodeURIComponent).
	prefix := "gowa_admin_auth_inst1="
	value := strings.TrimPrefix(cookie, prefix)
	// Cut at the first ';' to isolate the cookie value.
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = value[:idx]
	}
	if value != token {
		t.Fatalf("expected cookie value to equal token, got %q vs %q", value, token)
	}
}

// ---- Clear cookie ----

func TestMagic_ClearCookieAttributes(t *testing.T) {
	svc := newTestService("test-secret")
	cookie := svc.ClearCookie("inst1", "http://localhost/app/inst1/")
	if !strings.HasPrefix(cookie, "gowa_admin_auth_inst1=;") {
		t.Fatalf("expected clear cookie to set empty value, got %q", cookie)
	}
	if !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("expected Max-Age=0, got %q", cookie)
	}
	if !strings.Contains(cookie, "Path=/app/inst1") {
		t.Fatalf("expected Path=/app/inst1, got %q", cookie)
	}
	if !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("expected HttpOnly, got %q", cookie)
	}
	if !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("expected SameSite=Lax, got %q", cookie)
	}
	if strings.Contains(cookie, "Secure") {
		t.Fatalf("expected no Secure over http, got %q", cookie)
	}
}

func TestMagic_ClearCookieSecureOverHttps(t *testing.T) {
	svc := newTestService("test-secret")
	cookie := svc.ClearCookie("inst1", "https://example.com/app/inst1/")
	if !strings.Contains(cookie, "Secure") {
		t.Fatalf("expected Secure over https, got %q", cookie)
	}
}

// ---- Cookie parsing ----

func TestMagic_ParseCookiesBasic(t *testing.T) {
	cookies := parseCookies("a=1; b=2; c=3")
	if cookies["a"] != "1" || cookies["b"] != "2" || cookies["c"] != "3" {
		t.Fatalf("unexpected cookies: %v", cookies)
	}
}

func TestMagic_ParseCookiesEmpty(t *testing.T) {
	cookies := parseCookies("")
	if len(cookies) != 0 {
		t.Fatalf("expected empty map, got %v", cookies)
	}
}

func TestMagic_ParseCookiesNil(t *testing.T) {
	cookies := parseCookies("")
	if len(cookies) != 0 {
		t.Fatalf("expected empty map for empty header, got %v", cookies)
	}
}

func TestMagic_ParseCookiesNoEquals(t *testing.T) {
	cookies := parseCookies("justaname; a=1")
	if _, ok := cookies["justaname"]; ok {
		t.Fatal("expected part without '=' to be skipped")
	}
	if cookies["a"] != "1" {
		t.Fatalf("expected a=1, got %v", cookies)
	}
}

func TestMagic_ParseCookiesValueWithEquals(t *testing.T) {
	// Value may contain '=' — only split on the first '='.
	cookies := parseCookies("token=a.b.c==")
	if cookies["token"] != "a.b.c==" {
		t.Fatalf("expected value to retain '=' chars, got %q", cookies["token"])
	}
}

func TestMagic_ParseCookiesURLDecoded(t *testing.T) {
	cookies := parseCookies("name=%E4%B8%AD%E6%96%87")
	if cookies["name"] != "中文" {
		t.Fatalf("expected URL-decoded value, got %q", cookies["name"])
	}
}

func TestMagic_ParseCookiesExtraSpaces(t *testing.T) {
	cookies := parseCookies("  a=1  ;  b = 2  ")
	if cookies["a"] != "1" {
		t.Fatalf("expected a=1, got %q", cookies["a"])
	}
	// Bun: part.trim() = "b = 2", split('=') = ["b ", " 2"].
	// The name keeps its trailing space and the value keeps its leading
	// space because only the part is trimmed, not the split results.
	if cookies["b "] != " 2" {
		t.Fatalf("expected key %q = %q (Bun does not trim split results), got map %v", "b ", " 2", cookies)
	}
}

// ---- HasValidCookie ----

func TestMagic_HasValidCookieValid(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	header := "gowa_admin_auth_inst1=" + token
	if !svc.HasValidCookie(header, "inst1", now) {
		t.Fatal("expected valid cookie to pass")
	}
}

func TestMagic_HasValidCookieMissing(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if svc.HasValidCookie("", "inst1", now) {
		t.Fatal("expected empty header to fail")
	}
}

func TestMagic_HasValidCookieWrongName(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := svc.CreateToken("inst1", now)
	header := "gowa_admin_auth_other=" + token
	if svc.HasValidCookie(header, "inst1", now) {
		t.Fatal("expected cookie with wrong name to fail")
	}
}

func TestMagic_HasValidCookieExpired(t *testing.T) {
	svc := newTestService("test-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, expiresAt := svc.CreateToken("inst1", now)
	header := "gowa_admin_auth_inst1=" + token
	if svc.HasValidCookie(header, "inst1", expiresAt.Add(time.Second)) {
		t.Fatal("expected expired cookie to fail")
	}
}

// ---- Key rotation ----

func TestMagic_KeyRotationInvalidatesOldToken(t *testing.T) {
	oldSvc := newTestService("old-secret")
	newSvc := newTestService("new-secret")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := oldSvc.CreateToken("inst1", now)

	// Token valid with old secret.
	if !oldSvc.ValidateToken(token, "inst1", now) {
		t.Fatal("expected token valid with old secret")
	}
	// Token invalid with new secret (key rotation).
	if newSvc.ValidateToken(token, "inst1", now) {
		t.Fatal("expected token invalid after key rotation")
	}
}

func TestMagic_DifferentSecretsProduceDifferentSignatures(t *testing.T) {
	svcA := newTestService("secret-a")
	svcB := newTestService("secret-b")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tokenA, _ := svcA.CreateToken("inst1", now)
	tokenB, _ := svcB.CreateToken("inst1", now)
	partsA := strings.Split(tokenA, ".")
	partsB := strings.Split(tokenB, ".")
	if partsA[1] == partsB[1] {
		t.Fatal("expected different secrets to produce different signatures")
	}
}

// ---- isHttps ----

func TestMagic_IsHttps(t *testing.T) {
	cases := []struct {
		url    string
		expect bool
	}{
		{"https://example.com/app/inst1/", true},
		{"http://example.com/app/inst1/", false},
		{"http://localhost:8080/app/inst1/", false},
		{"https://localhost:8443/app/inst1/", true},
		{":::not-a-url", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isHTTPS(c.url); got != c.expect {
			t.Errorf("isHTTPS(%q) = %v, want %v", c.url, got, c.expect)
		}
	}
}

// ---- Fuzz tests ----

func FuzzMagicValidateToken(f *testing.F) {
	f.Add("inst1")
	f.Add("")
	f.Add("a.b")
	f.Add("a.b.c.d")
	f.Add("!!!.sig")
	f.Add("payload.")
	f.Add(".signature")
	f.Fuzz(func(t *testing.T, token string) {
		svc := newTestService("fuzz-secret")
		now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		// Must never panic.
		_ = svc.ValidateToken(token, "inst1", now)
	})
}

func FuzzMagicParseCookies(f *testing.F) {
	f.Add("a=1; b=2")
	f.Add("")
	f.Add("noequals")
	f.Add("a==; b=c=d")
	f.Add(";;;")
	f.Add("name=%E4%B8%AD")
	f.Fuzz(func(t *testing.T, header string) {
		// Must never panic.
		_ = parseCookies(header)
	})
}

func FuzzMagicHasValidCookie(f *testing.F) {
	f.Add("gowa_admin_auth_inst1=abc.def")
	f.Add("")
	f.Add("garbage")
	f.Add("a=1; b=2; c=3")
	f.Fuzz(func(t *testing.T, header string) {
		svc := newTestService("fuzz-secret")
		now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		// Must never panic.
		_ = svc.HasValidCookie(header, "inst1", now)
	})
}
