package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fadlee/gowa-manager/internal/testutil"
	"github.com/fadlee/gowa-manager/internal/users"
)

func authHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// newTestUsers returns a users service backed by a temp database. When
// username is non-empty, it is seeded as the first superadmin.
func newTestUsers(t *testing.T, username, password string) *users.Service {
	t.Helper()
	db, _ := testutil.TempDB(t)
	svc, err := users.NewService(context.Background(), db.SQL, users.Options{SeedUsername: username, SeedPassword: password})
	if err != nil {
		t.Fatalf("users.NewService: %v", err)
	}
	return svc
}

func doJSON(t *testing.T, handler http.Handler, method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func withHeader(key, value string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("session cookie flags: %+v", c)
			}
			return c
		}
	}
	t.Fatalf("no session cookie in response (status %d, body %s)", rec.Code, rec.Body.String())
	return nil
}

func TestAuth_SetupFlowCreatesSuperadminAndSession(t *testing.T) {
	svc := newTestUsers(t, "", "")
	handler := New(Dependencies{Users: svc, Instances: &fakeInstanceService{}})

	rec := doJSON(t, handler, http.MethodGet, "/api/auth/status", "")
	assertBodyFields(t, rec, map[string]any{"needsSetup": true, "user": nil})

	// Instances are protected even before setup.
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", ""), http.StatusUnauthorized)

	rec = doJSON(t, handler, http.MethodPost, "/api/auth/setup", `{"username":"root","password":"password123","setupToken":"nope"}`)
	assertStatus(t, rec, http.StatusForbidden)

	rec = doJSON(t, handler, http.MethodPost, "/api/auth/setup", `{"username":"root","password":"password123","setupToken":"`+svc.SetupToken()+`"}`)
	assertStatus(t, rec, http.StatusOK)
	cookie := sessionCookie(t, rec)

	rec = doJSON(t, handler, http.MethodGet, "/api/account", "", withCookie(cookie))
	assertStatus(t, rec, http.StatusOK)
	var me struct {
		User users.User `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.User.Username != "root" || me.User.Role != users.RoleSuperadmin {
		t.Fatalf("me = %+v", me.User)
	}
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", withCookie(cookie)), http.StatusOK)

	// Registration is closed after the first user.
	rec = doJSON(t, handler, http.MethodPost, "/api/auth/setup", `{"username":"x","password":"password123","setupToken":"anything"}`)
	assertStatus(t, rec, http.StatusConflict)
}

func TestAuth_LoginLogout(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123"), Instances: &fakeInstanceService{}})

	rec := doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"wrong"}`)
	assertStatus(t, rec, http.StatusUnauthorized)

	rec = doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"password123"}`)
	assertStatus(t, rec, http.StatusOK)
	cookie := sessionCookie(t, rec)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", withCookie(cookie)), http.StatusOK)

	assertStatus(t, doJSON(t, handler, http.MethodPost, "/api/auth/logout", "", withCookie(cookie)), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", withCookie(cookie)), http.StatusUnauthorized)
}

func TestAuth_LoginRateLimited(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123")})
	for i := 0; i < 10; i++ {
		doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"wrong"}`)
	}
	rec := doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"password123"}`)
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestAuth_BasicAuthAgainstUsers(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "pass:word1"), Instances: &fakeInstanceService{}})

	rec := doJSON(t, handler, http.MethodGet, "/api/instances", "", withHeader("Authorization", authHeader("admin", "pass:word1")))
	assertStatus(t, rec, http.StatusOK)

	rec = doJSON(t, handler, http.MethodGet, "/api/instances", "", withHeader("Authorization", authHeader("admin", "wrong")))
	assertStatus(t, rec, http.StatusUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="GOWA Manager"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}

	// No Basic attempt: no browser challenge.
	rec = doJSON(t, handler, http.MethodGet, "/api/instances", "")
	assertStatus(t, rec, http.StatusUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("unexpected challenge %q", got)
	}
}

func TestAuth_APIKeyLifecycle(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123"), Instances: &fakeInstanceService{}})
	basic := withHeader("Authorization", authHeader("admin", "password123"))

	rec := doJSON(t, handler, http.MethodPost, "/api/account/api-keys", `{"name":"ci"}`, basic)
	assertStatus(t, rec, http.StatusCreated)
	var created struct {
		APIKey users.APIKey `json:"apiKey"`
		Key    string       `json:"key"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Key, users.APIKeyPrefix) {
		t.Fatalf("key = %q", created.Key)
	}

	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", withHeader("Authorization", "Bearer "+created.Key)), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", withHeader("X-API-Key", created.Key)), http.StatusOK)

	// API keys cannot manage accounts, even for a superadmin.
	bearer := withHeader("Authorization", "Bearer "+created.Key)
	assertStatus(t, doJSON(t, handler, http.MethodPost, "/api/account/api-keys", `{"name":"x"}`, bearer), http.StatusForbidden)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/users", "", bearer), http.StatusForbidden)

	rec = doJSON(t, handler, http.MethodGet, "/api/account/api-keys", "", basic)
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), created.Key) {
		t.Fatal("key listing must not expose the plaintext key")
	}

	assertStatus(t, doJSON(t, handler, http.MethodDelete, "/api/account/api-keys/"+itoa(created.APIKey.ID), "", basic), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/instances", "", bearer), http.StatusUnauthorized)
}

func TestAuth_UserManagementRequiresSuperadmin(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123")})
	admin := withHeader("Authorization", authHeader("admin", "password123"))

	rec := doJSON(t, handler, http.MethodPost, "/api/users", `{"username":"member","password":"password123"}`, admin)
	assertStatus(t, rec, http.StatusCreated)
	var member users.User
	_ = json.Unmarshal(rec.Body.Bytes(), &member)
	if member.Role != users.RoleUser {
		t.Fatalf("default role = %q", member.Role)
	}
	assertStatus(t, doJSON(t, handler, http.MethodPost, "/api/users", `{"username":"MEMBER","password":"password123"}`, admin), http.StatusConflict)

	memberAuth := withHeader("Authorization", authHeader("member", "password123"))
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/users", "", memberAuth), http.StatusForbidden)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/account", "", memberAuth), http.StatusOK)

	// Superadmin resets the member's password and disables them.
	assertStatus(t, doJSON(t, handler, http.MethodPut, "/api/users/"+itoa(member.ID)+"/password", `{"password":"newpassword1"}`, admin), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/account", "", withHeader("Authorization", authHeader("member", "newpassword1"))), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodPatch, "/api/users/"+itoa(member.ID), `{"isActive":false}`, admin), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/account", "", withHeader("Authorization", authHeader("member", "newpassword1"))), http.StatusUnauthorized)

	// The last superadmin cannot demote themselves.
	rec = doJSON(t, handler, http.MethodGet, "/api/account", "", admin)
	var me struct {
		User users.User `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	assertStatus(t, doJSON(t, handler, http.MethodPatch, "/api/users/"+itoa(me.User.ID), `{"role":"user"}`, admin), http.StatusConflict)

	assertStatus(t, doJSON(t, handler, http.MethodDelete, "/api/users/"+itoa(member.ID), "", admin), http.StatusOK)
}

func TestAuth_ChangeOwnPassword(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123")})
	rec := doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"password123"}`)
	cookie := sessionCookie(t, rec)

	assertStatus(t, doJSON(t, handler, http.MethodPut, "/api/account/password", `{"currentPassword":"bad","newPassword":"newpassword1"}`, withCookie(cookie)), http.StatusBadRequest)
	assertStatus(t, doJSON(t, handler, http.MethodPut, "/api/account/password", `{"currentPassword":"password123","newPassword":"short"}`, withCookie(cookie)), http.StatusBadRequest)
	assertStatus(t, doJSON(t, handler, http.MethodPut, "/api/account/password", `{"currentPassword":"password123","newPassword":"newpassword1"}`, withCookie(cookie)), http.StatusOK)
	// The current session survives the change.
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/account", "", withCookie(cookie)), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"newpassword1"}`), http.StatusOK)
}

func TestAuth_HealthAndLogoutUnprotected(t *testing.T) {
	handler := New(Dependencies{Users: newTestUsers(t, "admin", "password123")})
	assertStatus(t, doJSON(t, handler, http.MethodGet, "/api/health", ""), http.StatusOK)
	assertStatus(t, doJSON(t, handler, http.MethodPost, "/api/auth/logout", ""), http.StatusOK)
}

func itoa(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
