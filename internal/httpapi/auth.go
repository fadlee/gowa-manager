package httpapi

import (
	"net/http"

	"github.com/fadlee/gowa-manager/internal/auth"
)

// basicAuthMiddleware wraps an http.Handler with Basic Auth protection.
// If the request does not carry valid credentials the handler responds
// with 401 and a WWW-Authenticate challenge header; otherwise the
// wrapped handler is invoked.
//
// Credentials and the Authorization header are never logged.
func basicAuthMiddleware(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.ValidateBasicAuth(r.Header.Get("Authorization"), username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GOWA Manager"`)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// registerAuthRoutes registers the /api/auth/login and /api/auth/logout
// routes on the given mux. The login route is protected by Basic Auth;
// the logout route is not.
func registerAuthRoutes(mux *http.ServeMux, deps Dependencies) {
	mux.HandleFunc("/api/auth/login", loginHandler(deps.AdminUsername, deps.AdminPassword))
	mux.HandleFunc("/api/auth/logout", logoutHandler)
}

// loginHandler returns a handler for POST /api/auth/login. The route is
// protected by Basic Auth: valid credentials produce a 200 response
// with the username, while invalid or missing credentials produce a 401
// with a WWW-Authenticate challenge.
func loginHandler(username, password string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
			return
		}
		if !auth.ValidateBasicAuth(r.Header.Get("Authorization"), username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GOWA Manager"`)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Unauthorized"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Login successful", "user": username})
	}
}

// logoutHandler handles POST /api/auth/logout. It is not protected by
// Basic Auth and always succeeds.
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Logout successful"})
}
