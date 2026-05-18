package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: message, Code: code})
}

// parseIDParam extracts a positive int64 path param from a Go 1.22+ mux route.
// On failure it writes a 400 envelope and returns ok=false.
func parseIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "path parameter '"+name+"' must be a positive integer")
		return 0, false
	}
	return id, true
}
