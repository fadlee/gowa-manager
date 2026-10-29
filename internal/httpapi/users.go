package httpapi

import (
	"net/http"

	"github.com/fadlee/gowa-manager/internal/users"
)

// registerAccountRoutes registers /api/account/* (every user, own
// account) and /api/users/* (superadmin only). They are mounted behind
// the auth middleware.
func (h *authHandler) registerAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", h.me)
	mux.HandleFunc("PUT /api/account/password", requireAccount(h.changeOwnPassword))
	mux.HandleFunc("GET /api/account/api-keys", requireAccount(h.listAPIKeys))
	mux.HandleFunc("POST /api/account/api-keys", requireAccount(h.createAPIKey))
	mux.HandleFunc("DELETE /api/account/api-keys/{id}", requireAccount(h.revokeAPIKey))

	mux.HandleFunc("GET /api/users", requireSuperadmin(h.listUsers))
	mux.HandleFunc("POST /api/users", requireSuperadmin(h.createUser))
	mux.HandleFunc("PATCH /api/users/{id}", requireSuperadmin(h.updateUser))
	mux.HandleFunc("DELETE /api/users/{id}", requireSuperadmin(h.deleteUser))
	mux.HandleFunc("PUT /api/users/{id}/password", requireSuperadmin(h.resetUserPassword))
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	p, _ := currentPrincipal(r)
	writeJSON(w, http.StatusOK, map[string]any{"user": p.User, "viaApiKey": p.ViaAPIKey})
}

func (h *authHandler) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	p, _ := currentPrincipal(r)
	if err := h.users.ChangeOwnPassword(r.Context(), p.User.ID, req.CurrentPassword, req.NewPassword, p.SessionToken); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *authHandler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	p, _ := currentPrincipal(r)
	keys, err := h.users.ListAPIKeys(r.Context(), p.User.ID)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *authHandler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	p, _ := currentPrincipal(r)
	key, plaintext, err := h.users.CreateAPIKey(r.Context(), p.User.ID, req.Name)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"apiKey": key, "key": plaintext})
}

func (h *authHandler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, _ := currentPrincipal(r)
	if err := h.users.RevokeAPIKey(r.Context(), p.User.ID, id); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *authHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := h.users.List(r.Context())
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *authHandler) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string     `json:"username"`
		Password string     `json:"password"`
		Role     users.Role `json:"role"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	if req.Role == "" {
		req.Role = users.RoleUser
	}
	user, err := h.users.Create(r.Context(), req.Username, req.Password, req.Role)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *authHandler) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Role     *users.Role `json:"role"`
		IsActive *bool       `json:"isActive"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	user, err := h.users.Update(r.Context(), id, users.UpdateRequest{Role: req.Role, IsActive: req.IsActive})
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *authHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.users.Delete(r.Context(), id); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *authHandler) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeRequest(w, r, &req) {
		return
	}
	// Keep the caller's own session when a superadmin resets their own password.
	keep := ""
	if p, _ := currentPrincipal(r); p.User.ID == id {
		keep = p.SessionToken
	}
	if err := h.users.SetPassword(r.Context(), id, req.Password, keep); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := decodeJSON(r, dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid request body"})
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid ID"})
		return 0, false
	}
	return id, true
}
