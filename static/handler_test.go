package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaticHandler(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"index.html": "<h1>shell</h1>", "license": "license text", "existing/child.txt": "child", "assets/main.js": "1234567890", "assets/main.js.map": "private map", ".env": "secret", "nested/.secret/x": "secret", "assets/test.wasm": "wasm", "assets/main.css": "body{}", "assets/info.json": "{}", "assets/remote.mjs": "export{}", "microfrontends/auth/v1/remote.js": "remote"} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h := handler{files: os.DirFS(root), spa: true}
	for _, tc := range []struct {
		method, url, accept, mode string
		code                      int
		body, mime                string
	}{
		{"GET", "/", "", "", 200, "<h1>shell</h1>", "text/html"},
		{"GET", "/dashboard/settings/", "text/html", "navigate", 200, "<h1>shell</h1>", "text/html"},
		{"GET", "/dashboard", "text/html;q=0", "", 404, "", ""},
		{"GET", "/license/", "text/html", "navigate", 404, "", ""},
		{"GET", "/existing", "text/html", "navigate", 404, "", ""},
		{"GET", "/existing/", "text/html", "navigate", 404, "", ""},
		{"GET", "/dashboard", "*/*", "", 404, "", ""},
		{"GET", "/dashboard", "text/html", "cors", 404, "", ""},
		{"HEAD", "/dashboard", "text/html", "", 200, "", "text/html"},
		{"GET", "/assets/main.js", "", "", 200, "1234567890", "text/javascript"},
		{"GET", "/assets/remote.mjs", "", "", 200, "export{}", "text/javascript"},
		{"GET", "/assets/main.css", "", "", 200, "body{}", "text/css"},
		{"GET", "/assets/info.json", "", "", 200, "{}", "application/json"},
		{"GET", "/assets/test.wasm", "", "", 200, "wasm", "application/wasm"},
		{"GET", "/microfrontends/auth/v1/remote.js", "", "", 200, "remote", "text/javascript"},
		{"GET", "/assets/missing.js", "text/html", "", 404, "", ""},
		{"GET", "/assets", "text/html", "", 404, "", ""},
		{"GET", "/assets/missing", "text/html", "", 404, "", ""},
		{"GET", "/microfrontends/auth/v1/missing", "text/html", "", 404, "", ""},
		{"GET", "/api/login", "text/html", "", 404, "", ""},
		{"GET", "/reportbox.auth.v1.Auth/Login", "text/html", "", 404, "", ""},
		{"GET", "/metrics", "text/html", "", 404, "", ""},
		{"GET", "/assets/main.js/", "text/html", "", 404, "", ""},
		{"GET", "/assets/main.js.map", "text/html", "", 404, "", ""},
		{"GET", "/assets/main.js.map%20", "text/html", "", 404, "", ""},
		{"GET", "/assets/main.js:secret", "text/html", "", 404, "", ""},
		{"GET", "/.env", "text/html", "", 404, "", ""},
		{"GET", "/nested/.secret/x", "text/html", "", 404, "", ""},
		{"GET", "/assets/../index.html", "text/html", "", 404, "", ""},
		{"GET", "/%2e%2e/index.html", "text/html", "", 404, "", ""},
		{"GET", "/%252e%252e/index.html", "text/html", "", 404, "", ""},
		{"GET", "/assets%5cmain.js", "text/html", "", 404, "", ""},
		{"GET", "//index.html", "text/html", "", 404, "", ""},
		{"POST", "/", "text/html", "", 405, "", ""},
	} {
		t.Run(tc.method+tc.url+tc.accept+tc.mode, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			req.Header.Set("Accept", tc.accept)
			if tc.mode != "" {
				req.Header.Set("Sec-Fetch-Mode", tc.mode)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.code {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if tc.code == 200 && rr.Body.String() != tc.body {
				t.Fatalf("body=%q", rr.Body.String())
			}
			if tc.mime != "" && !strings.HasPrefix(rr.Header().Get("Content-Type"), tc.mime) {
				t.Fatalf("mime=%q", rr.Header().Get("Content-Type"))
			}
			if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
			if tc.code == 200 && rr.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("cache policy")
			}
			if rr.Header().Get("Vary") != "Accept, Sec-Fetch-Mode" {
				t.Fatal("missing Vary")
			}
			if tc.code == 404 && strings.Contains(rr.Body.String(), "shell") {
				t.Fatal("unexpected fallback")
			}
		})
	}
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Accept", "text/html")
	rr := httptest.NewRecorder()
	(handler{files: os.DirFS(root)}).ServeHTTP(rr, req)
	if rr.Code != 404 {
		t.Fatal("SPA disabled")
	}
	req = httptest.NewRequest("GET", "/", strings.NewReader("body"))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatal("body accepted")
	}
	req = httptest.NewRequest("GET", "/assets/main.js", nil)
	req.Header.Set("Range", "bytes=2-4")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 206 || rr.Body.String() != "345" {
		t.Fatal("range", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest("GET", "/assets/main.js", nil)
	req.Header.Set("If-Modified-Since", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 304 {
		t.Fatal("conditional", rr.Code)
	}
}

func TestConflictingNavigationHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Add("Sec-Fetch-Mode", "navigate")
	req.Header.Add("Sec-Fetch-Mode", "cors")
	if navigation(req, "dashboard") {
		t.Fatal("ambiguous navigation accepted")
	}
}
