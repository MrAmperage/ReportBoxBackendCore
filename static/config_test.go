package static

import "testing"

func TestConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Validate() == nil {
		t.Fatal("missing root accepted")
	}
	c.Root = "dist/browser"
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Config){func(c *Config) { c.Address = "localhost:8080" }, func(c *Config) { c.Address = "0.0.0.0:8080" }, func(c *Config) { c.Address = "127.0.0.1:65536" }, func(c *Config) { c.WriteTimeout = 0 }, func(c *Config) { c.MaxHeaderBytes = int(^uint(0) >> 1) }, func(c *Config) { c.MaxConcurrent = 0 }} {
		v := c
		edit(&v)
		if v.Validate() == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
	c.Address = "0.0.0.0:8080"
	c.AllowRemote = true
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestLoadConfig(t *testing.T) {
	t.Setenv("STATIC_ROOT", "dist")
	t.Setenv("STATIC_ADDRESS", "127.0.0.1:0")
	t.Setenv("STATIC_SPA", "true")
	c, e := LoadConfig()
	if e != nil || !c.SPA || c.Address != "127.0.0.1:0" {
		t.Fatal(c, e)
	}
	t.Setenv("STATIC_SPA", "invalid")
	if _, e := LoadConfig(); e == nil {
		t.Fatal("invalid bool accepted")
	}
}
