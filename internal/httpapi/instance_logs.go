package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fadlee/gowa-manager/internal/instancelogs"
	"github.com/fadlee/gowa-manager/internal/instances"
)

const (
	defaultLogTail = 200
	maxLogTail     = 500
)

type InstanceLogReader interface {
	Tail(instanceID int64, tail int) []instancelogs.Entry
}

type instanceLogsResponse struct {
	InstanceID int64              `json:"instanceId"`
	Tail       int                `json:"tail"`
	Entries    []instanceLogEntry `json:"entries"`
}

type instanceLogEntry struct {
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"`
	Line      string `json:"line"`
}

func (h *instanceHandler) logs(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := h.service.Get(r.Context(), id); err != nil {
		h.writeError(w, err, "Failed to get instance", false)
		return
	}
	if h.logReader == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Instance logs not ready", false)
		return
	}
	tail, err := parseLogTail(r)
	if err != nil {
		writeValidation(w, err.Error())
		return
	}
	entries := h.logReader.Tail(id, tail)
	out := make([]instanceLogEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, instanceLogEntry{Timestamp: entry.Timestamp.UTC().Format(time.RFC3339Nano), Stream: string(entry.Stream), Line: entry.Line})
	}
	writeJSON(w, http.StatusOK, instanceLogsResponse{InstanceID: id, Tail: tail, Entries: out})
}

func parseLogTail(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("tail")
	if raw == "" {
		return defaultLogTail, nil
	}
	tail, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("tail must be a number")
	}
	if tail <= 0 {
		return 0, errors.New("tail must be greater than 0")
	}
	if tail > maxLogTail {
		return maxLogTail, nil
	}
	return tail, nil
}
