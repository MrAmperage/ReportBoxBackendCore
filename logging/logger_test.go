package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
)

func TestLogger(t *testing.T) {
	var b bytes.Buffer
	before := slog.Default()
	l, err := New(&b, app.Info{Name: "example"}, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	l.Debug("hidden")
	l.Info("hello")
	var m map[string]any
	if err = json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["service"] != "example" || m["version"] != "dev" || m["commit"] != "unknown" || m["msg"] != "hello" || slog.Default() != before {
		t.Fatalf("%v", m)
	}
}
