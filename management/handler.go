package management

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
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

// validHost protects the default loopback listener from DNS rebinding.
// Remote mode deliberately permits service DNS names on a private network.
func (s *Server) validHost(authority string) bool {
	bound := s.Address()
	if bound == "" {
		bound = s.cfg.Address
	}
	host, port, err := net.SplitHostPort(bound)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if !ip.IsLoopback() {
		return s.cfg.AllowRemote
	}
	requestHost, requestPort, err := net.SplitHostPort(authority)
	if err != nil {
		// HTTP clients normally omit the default HTTP port from Host.
		if port != "80" {
			return false
		}
		requestHost, requestPort = authority, "80"
		if strings.HasPrefix(authority, "[") && strings.HasSuffix(authority, "]") {
			requestHost = authority[1 : len(authority)-1]
		}
	}
	if requestPort != port {
		return false
	}
	return strings.EqualFold(requestHost, "localhost") || ip.Equal(net.ParseIP(requestHost))
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !s.validHost(r.Host) {
		http.Error(w, "invalid host", http.StatusMisdirectedRequest)
		return
	}
	path := r.URL.Path
	// No redirects, aliases, debug endpoints or arbitrary application routes.
	if r.URL.IsAbs() || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || (path != "/livez" && path != "/readyz" && path != "/healthz" && path != "/metrics") || (path == "/metrics" && s.metrics == nil) {
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
