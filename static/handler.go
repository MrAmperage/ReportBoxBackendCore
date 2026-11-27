package static

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode"
)

// handler only sees the confined filesystem created during Open.
type handler struct {
	files fs.FS
	spa   bool
}

func safeName(p string) (string, bool) {
	if p == "/" {
		return "index.html", true
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\%:") || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", false
	}
	// Allow a single trailing slash on navigation routes, not arbitrary path cleanup.
	name := strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/")
	if !fs.ValidPath(name) || name == "." {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.HasSuffix(strings.ToLower(part), ".map") {
			return "", false
		}
	}
	return name, true
}

func navigation(r *http.Request, name string) bool {
	// Never answer missing assets or API/technical routes with HTML.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.Contains(part, ".") {
			return false
		}
	}
	first, _, _ := strings.Cut(name, "/")
	switch strings.ToLower(first) {
	case "assets", "microfrontends", "api", "rpc", "management", "metrics", "livez", "readyz", "healthz", "static":
		return false
	}
	if modes := r.Header.Values("Sec-Fetch-Mode"); len(modes) > 1 || (len(modes) == 1 && modes[0] != "navigate") {
		return false
	}
	for _, value := range r.Header.Values("Accept") {
		for _, item := range strings.Split(value, ",") {
			med, params, err := mime.ParseMediaType(strings.TrimSpace(item))
			if err != nil || med != "text/html" {
				continue
			}
			if q, ok := params["q"]; ok {
				v, e := strconv.ParseFloat(q, 64)
				if e != nil || !(v > 0 && v <= 1) {
					continue
				}
			}
			return true
		}
	}
	return false
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// Vary applies to rejected navigation requests too, not just successful files.
	w.Header().Set("Vary", "Accept, Sec-Fetch-Mode")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.URL.IsAbs() {
		w.Header().Set("Connection", "close")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name, ok := safeName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Do not serve a file through a slash-suffixed URL.
	f, info, err := h.openFile(name)
	if err == nil && r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
		f.Close()
		err = fs.ErrPermission
	}
	if errors.Is(err, fs.ErrNotExist) && h.spa && navigation(r, name) {
		name = "index.html"
		f, info, err = h.openFile(name)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "static read failed", http.StatusInternalServerError)
		}
		return
	}
	defer f.Close()
	content, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "static read failed", 500)
		return
	}
	// Explicit browser-sensitive MIME types, independent of the host MIME database.
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".wasm":
		w.Header().Set("Content-Type", "application/wasm")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, path.Base(name), info.ModTime(), content)
}

// openFile returns an open regular file; it never returns an open file on error.
func (h handler) openFile(name string) (fs.File, fs.FileInfo, error) {
	f, err := h.files.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, fs.ErrPermission
	}
	return f, info, nil
}
