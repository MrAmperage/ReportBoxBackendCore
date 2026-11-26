package management

import (
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Address != "127.0.0.1:7613" || c.Validate() != nil {
		t.Fatal("bad defaults")
	}
	for _, addr := range []string{"", "localhost:7613", ":7613", "127.0.0.1:x", "127.0.0.1:-1", "127.0.0.1:65536", "0.0.0.0:7613", "[::]:7613"} {
		c := DefaultConfig()
		c.Address = addr
		if c.Validate() == nil {
			t.Errorf("accepted %q", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:7613", "0.0.0.0:7613", "[::]:7613"} {
		c := DefaultConfig()
		c.Address = addr
		c.AllowRemote = true
		if err := c.Validate(); err != nil {
			t.Errorf("rejected %q: %v", addr, err)
		}
	}
	c.MaxConcurrent = 0
	if c.Validate() == nil {
		t.Fatal("zero concurrency accepted")
	}
}
func TestLoadConfig(t *testing.T) {
	t.Setenv("MANAGEMENT_ADDRESS", "0.0.0.0:7613")
	t.Setenv("MANAGEMENT_ALLOW_REMOTE", "true")
	t.Setenv("MANAGEMENT_REQUEST_TIMEOUT", "3s")
	t.Setenv("MANAGEMENT_MAX_CONCURRENT", "4")
	c, e := LoadConfig()
	if e != nil || c.RequestTimeout != 3*time.Second || c.MaxConcurrent != 4 {
		t.Fatalf("unexpected config: %+v %v", c, e)
	}
	t.Setenv("MANAGEMENT_ALLOW_REMOTE", "false")
	if _, e = LoadConfig(); e == nil {
		t.Fatal("remote accepted without permission")
	}
	t.Setenv("MANAGEMENT_ALLOW_REMOTE", "true")
	t.Setenv("MANAGEMENT_REQUEST_TIMEOUT", "")
	if _, e = LoadConfig(); e == nil {
		t.Fatal("empty timeout accepted")
	}
}
