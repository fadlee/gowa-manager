package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/users"
)

const sessionCookieName = "gowa_session"

// principal is the authenticated caller attached to the request context
// by the auth middleware.
type principal struct {
	User         users.User
	ViaAPIKey    bool
	SessionToken string
}

type principalKey struct{}

func currentPrincipal(r *http.Request) (principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(principal)
	return p, ok
}

type authHandler struct {
	users   *users.Service
	limiter *users.LoginLimiter
}

func newAuthHandler(svc *users.Service) *authHandler {
	return &authHandler{users: svc, limiter: users.NewLoginLimiter(10, 15*time.Minute)}
}

// authenticate resolves the caller from, in order: an API key
// (Authorization: Bearer or X-API-Key), HTTP Basic credentials of a
// manager user, or the session cookie. Credentials are never logged.
func (h *authHandler) authenticate(w http.ResponseWriter, r *http.Request) (principal, bool) {
	ctx := r.Context()
	if key := apiKeyFromRequest(r); key != "" {
		user, err := h.users.ResolveAPIKey(ctx, key)
		return principal{User: user, ViaAPIKey: true}, err == nil
	}
	if username, password, ok := r.BasicAuth(); ok {
		user, ok := h.checkPassword(r, username, password)
		return principal{User: user}, ok
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return principal{}, false
	}
	user, refreshedUntil, err := h.users.ResolveSession(ctx, cookie.Value)
	if err != nil {
		return principal{}, false
	}
	if !refreshedUntil.IsZero() {
		setSessionCookie(w, r, cookie.Value, refreshedUntil)
	}
	return principal{User: user, SessionToken: cookie.Value}, true
}

// checkPassword validates credentials subject to the failed-login limiter.
func (h *authHandler) checkPassword(r *http.Request, username, password string) (users.User, bool) {
	key := clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(username))
	if !h.limiter.Allowed(key) {
		return users.User{}, false
	}
	user, err := h.users.Authenticate(r.Context(), username, password)
	if err != nil {
		h.limiter.Fail(key)
		return users.User{}, false
	}
	h.limiter.Reset(key)
	return user, true
}

func (h *authHandler) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.authenticate(w, r)
		if !ok {
			writeUnauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// requireAccount rejects API-key callers: managing accounts and keys
// needs an interactive login (session or Basic credentials).
func requireAccount(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p, ok := currentPrincipal(r); !ok || p.ViaAPIKey {
			writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "API keys cannot manage accounts"})
			return
		}
		next(w, r)
	}
}

func requireSuperadmin(next http.HandlerFunc) http.HandlerFunc {
	return requireAccount(func(w http.ResponseWriter, r *http.Request) {
		if p, _ := currentPrincipal(r); !p.User.IsSuperadmin() {
			writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "Superadmin access required"})
			return
		}
		next(w, r)
	})
}

// writeUnauthorized sends 401. The Basic challenge is only sent to
// callers that attempted Basic auth, so the browser UI never shows a
// native credentials prompt when its session expires.
func writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
		w.Header().Set("WWW-Authenticate", `Basic realm="GOWA Manager"`)
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Unauthorized"})
}

func apiKeyFromRequest(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	if scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(value)
	}
	return ""
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
}

// ---- Public auth routes: /api/auth/{status,setup,login,logout} ----

func (h *authHandler) registerPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/status", h.status)
	mux.HandleFunc("POST /api/auth/setup", h.setup)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
}

func (h *authHandler) status(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := h.users.NeedsSetup(r.Context())
	if err != nil {
		writeUserError(w, err)
		return
	}
	var user *users.User
	if p, ok := h.authenticate(w, r); ok {
		user = &p.User
	}
	writeJSON(w, http.StatusOK, map[string]any{"needsSetup": needsSetup, "user": user})
}

type credentialsRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	SetupToken string `json:"setupToken,omitempty"`
}

func (h *authHandler) setup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	limitKey := "setup|" + clientIP(r)
	if !h.limiter.Allowed(limitKey) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "error": "Too many attempts, try again later"})
		return
	}
	user, err := h.users.Setup(r.Context(), req.Username, req.Password, req.SetupToken)
	if err != nil {
		if errors.Is(err, users.ErrInvalidSetupToken) {
			h.limiter.Fail(limitKey)
		}
		writeUserError(w, err)
		return
	}
	h.startSession(w, r, user)
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	user, ok := h.checkPassword(r, req.Username, req.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Invalid username or password"})
		return
	}
	h.startSession(w, r, user)
}

func (h *authHandler) startSession(w http.ResponseWriter, r *http.Request, user users.User) {
	token, expiresAt, err := h.users.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeUserError(w, err)
		return
	}
	setSessionCookie(w, r, token, expiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "user": user})
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if err := h.users.DeleteSession(r.Context(), cookie.Value); err != nil {
			writeUserError(w, err)
			return
		}
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Logout successful"})
}

func writeUserError(w http.ResponseWriter, err error) {
	var validation *users.ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": validation.Message})
	case errors.Is(err, users.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
	case errors.Is(err, users.ErrInvalidSetupToken):
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "Invalid setup token"})
	case errors.Is(err, users.ErrUsernameTaken), errors.Is(err, users.ErrLastSuperadmin), errors.Is(err, users.ErrSetupCompleted):
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "error": capitalize(err.Error())})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": "Internal server error"})
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
