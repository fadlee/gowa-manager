package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func authHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestAuthRoutes_LoginSuccess(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", authHeader("admin", "password"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	want := `{"message":"Login successful","success":true,"user":"admin"}`
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestAuthRoutes_LoginNoCredentials(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="GOWA Manager"` {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, `Basic realm="GOWA Manager"`)
	}
}

func TestAuthRoutes_LoginWrongPassword(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", authHeader("admin", "wrong"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="GOWA Manager"` {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, `Basic realm="GOWA Manager"`)
	}
}

func TestAuthRoutes_LoginWrongUsername(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", authHeader("wrong", "password"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthRoutes_LoginMalformedScheme(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", "Bearer something")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthRoutes_LoginInvalidBase64(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", "Basic !!!notbase64!!!")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthRoutes_LoginPasswordWithColon(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "pass:word"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", authHeader("admin", "pass:word"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestAuthRoutes_LogoutSuccess(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	want := `{"message":"Logout successful","success":true}`
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestAuthRoutes_LogoutNoCredentialsRequired(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (logout is unprotected)", rec.Code)
	}
}

func TestAuthRoutes_HealthUnprotected(t *testing.T) {
	handler := New(Dependencies{AdminUsername: "admin", AdminPassword: "password"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (health is unprotected)", rec.Code)
	}
}

func TestAuthRoutes_ProtectedInstanceRouteRequiresAuth(t *testing.T) {
	handler := New(Dependencies{
		AdminUsername: "admin",
		AdminPassword: "password",
		Instances:     &fakeInstanceService{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="GOWA Manager"` {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, `Basic realm="GOWA Manager"`)
	}
}

func TestAuthRoutes_ProtectedInstanceRouteWithCredentials(t *testing.T) {
	handler := New(Dependencies{
		AdminUsername: "admin",
		AdminPassword: "password",
		Instances:     &fakeInstanceService{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	req.Header.Set("Authorization", authHeader("admin", "password"))
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("expected auth to pass, got 401")
	}
}

func TestAuthRoutes_ProtectedInstanceRouteWrongCredentials(t *testing.T) {
	handler := New(Dependencies{
		AdminUsername: "admin",
		AdminPassword: "password",
		Instances:     &fakeInstanceService{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	req.Header.Set("Authorization", authHeader("admin", "wrong"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthRoutes_ChallengeHeaderOnProtectedRoute(t *testing.T) {
	handler := New(Dependencies{
		AdminUsername: "admin",
		AdminPassword: "password",
		Instances:     &fakeInstanceService{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/instances/1", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="GOWA Manager"` {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, `Basic realm="GOWA Manager"`)
	}
}
