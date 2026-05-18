package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"status": "ok", "db": "ok"}
	w.Header().Set("Content-Type", "application/json")
	if err := s.DB.PingContext(r.Context()); err != nil {
		resp["status"] = "degraded"
		resp["db"] = "error: " + err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
