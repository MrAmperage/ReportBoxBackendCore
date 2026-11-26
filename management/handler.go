package management

import (
	"context"
	"encoding/json"
	"net/http"
)

type statusResponse struct {
	Status string `json:"status"`
}
type checkResponse struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
}
type healthResponse struct {
	Status   string          `json:"status"`
	Complete bool            `json:"complete"`
	Checks   []checkResponse `json:"checks"`
}

func writeJSON(w http.ResponseWriter, r *http.Request, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func status(w http.ResponseWriter, r *http.Request, healthy bool) {
	if healthy {
		writeJSON(w, r, http.StatusOK, statusResponse{"ok"})
	} else {
		writeJSON(w, r, http.StatusServiceUnavailable, statusResponse{"unavailable"})
	}
}
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	path := r.URL.Path
	// No redirects, aliases, debug endpoints or arbitrary application routes.
	if r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || (path != "/livez" && path != "/readyz" && path != "/healthz" && path != "/metrics") || (path == "/metrics" && s.metrics == nil) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		w.Header().Set("Connection", "close")
		http.Error(w, "request body not allowed", http.StatusBadRequest)
		return
	}
	// Liveness never runs a dependency probe or waits for the expensive-request semaphore.
	if path == "/livez" {
		status(w, r, s.registry.Live())
		return
	}
	if !s.admit() {
		if path == "/healthz" {
			writeJSON(w, r, http.StatusServiceUnavailable, healthResponse{"unavailable", false, []checkResponse{}})
		} else {
			status(w, r, false)
		}
		return
	}
	defer s.release()
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
	defer cancel()
	switch path {
	case "/readyz":
		status(w, r, s.registry.Ready(ctx) && !s.stopping.Load())
	case "/healthz":
		results, complete := s.registry.Probe(ctx)
		complete = complete && !s.stopping.Load()
		resp := healthResponse{Status: "ok", Complete: complete, Checks: make([]checkResponse, 0, len(results))}
		ok := complete
		for _, v := range results {
			resp.Checks = append(resp.Checks, checkResponse{v.Name, v.Healthy})
			ok = ok && v.Healthy
		}
		code := http.StatusOK
		if !ok {
			code = http.StatusServiceUnavailable
			resp.Status = "unavailable"
		}
		writeJSON(w, r, code, resp)
	case "/metrics":
		s.metrics.ServeHTTP(w, r.WithContext(ctx))
	}
}
