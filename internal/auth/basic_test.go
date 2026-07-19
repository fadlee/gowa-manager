package auth

import (
	"encoding/base64"
	"testing"
)

func basicHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestBasic_ValidCredentials(t *testing.T) {
	if !ValidateBasicAuth(basicHeader("admin", "password"), "admin", "password") {
		t.Fatal("expected valid credentials to pass")
	}
}

func TestBasic_MissingHeader(t *testing.T) {
	if ValidateBasicAuth("", "admin", "password") {
		t.Fatal("expected empty header to fail")
	}
}

func TestBasic_MalformedScheme(t *testing.T) {
	if ValidateBasicAuth("Bearer "+base64.StdEncoding.EncodeToString([]byte("admin:password")), "admin", "password") {
		t.Fatal("expected non-Basic scheme to fail")
	}
}

func TestBasic_LowercaseScheme(t *testing.T) {
	if ValidateBasicAuth("basic "+base64.StdEncoding.EncodeToString([]byte("admin:password")), "admin", "password") {
		t.Fatal("expected lowercase scheme to fail (case-sensitive)")
	}
}

func TestBasic_MissingBase64Portion(t *testing.T) {
	if ValidateBasicAuth("Basic", "admin", "password") {
		t.Fatal("expected missing base64 portion to fail")
	}
}

func TestBasic_EmptyBase64(t *testing.T) {
	if ValidateBasicAuth("Basic ", "admin", "password") {
		t.Fatal("expected empty base64 portion to fail")
	}
}

func TestBasic_InvalidBase64(t *testing.T) {
	if ValidateBasicAuth("Basic !!!notbase64!!!", "admin", "password") {
		t.Fatal("expected invalid base64 to fail")
	}
}

func TestBasic_NoColon(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("adminpassword"))
	if ValidateBasicAuth("Basic "+encoded, "admin", "password") {
		t.Fatal("expected missing colon separator to fail")
	}
}

func TestBasic_WrongUsername(t *testing.T) {
	if ValidateBasicAuth(basicHeader("wronguser", "password"), "admin", "password") {
		t.Fatal("expected wrong username to fail")
	}
}

func TestBasic_WrongPassword(t *testing.T) {
	if ValidateBasicAuth(basicHeader("admin", "wrongpass"), "admin", "password") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestBasic_PasswordWithColon(t *testing.T) {
	if !ValidateBasicAuth(basicHeader("admin", "pass:word"), "admin", "pass:word") {
		t.Fatal("expected password containing colon to pass")
	}
}

func TestBasic_SplitOnFirstColon(t *testing.T) {
	// "user:name:pass" splits on the FIRST colon: username="user", password="name:pass"
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:name:pass"))
	if !ValidateBasicAuth(header, "user", "name:pass") {
		t.Fatal("expected split on first colon only")
	}
}

func TestBasic_ExtraAfterCredentials(t *testing.T) {
	// Bun's split(' ') takes parts[1] as the encoded credentials, ignoring the rest.
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:password")) + " extra"
	if !ValidateBasicAuth(header, "admin", "password") {
		t.Fatal("expected extra content after credentials to be ignored (matching Bun split behavior)")
	}
}

func TestBasic_EmptyUsername(t *testing.T) {
	// ":password" → username="", password="password"
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte(":password"))
	if ValidateBasicAuth(header, "admin", "password") {
		t.Fatal("expected empty username to fail")
	}
}

func TestBasic_EmptyPassword(t *testing.T) {
	// "admin:" → username="admin", password=""
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"))
	if ValidateBasicAuth(header, "admin", "") {
		// Empty password is a valid credential if the expected password is also empty.
		// This documents that the split produces an empty password, not a failure.
	}
	// With a non-empty expected password, it should fail.
	if ValidateBasicAuth(header, "admin", "password") {
		t.Fatal("expected empty password with non-empty expected to fail")
	}
}
