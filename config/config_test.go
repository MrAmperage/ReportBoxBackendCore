package config

import (
	"strings"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	env := map[string]string{"LOG_LEVEL": "DEBUG", "LOG_ADD_SOURCE": "true", "SERVICE_START_TIMEOUT": "bad-secret"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	if _, err := resolve(lookup, Overrides{}); err == nil || strings.Contains(err.Error(), "bad-secret") {
		t.Fatalf("expected sanitized config error: %v", err)
	}
	off := false
	timeout := time.Second
	cfg, err := resolve(lookup, Overrides{LogAddSource: &off, StartTimeout: &timeout})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogAddSource || cfg.LogLevel != "debug" || cfg.StartTimeout != timeout || cfg.StopTimeout != 30*time.Second {
		t.Fatalf("unexpected config %+v", cfg)
	}
	env["LOG_FORMAT"] = ""
	if _, err := resolve(lookup, Overrides{StartTimeout: &timeout}); err == nil {
		t.Fatal("explicit empty accepted")
	}
}
func TestDefaults(t *testing.T) {
	c, err := resolve(func(string) (string, bool) { return "", false }, Overrides{})
	if err != nil || c != Defaults() {
		t.Fatalf("%+v %v", c, err)
	}
}
