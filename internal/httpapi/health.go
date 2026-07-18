package httpapi

import "net/http"

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
		return
	}
	writeRawJSON(w, http.StatusOK, []byte(`{"message":"GOWA Manager API is running","success":true}`))
}
