package rpc

import (
	"testing"
	"time"
)

func TestServerConfiguration(t *testing.T) {
	c := DefaultServerConfig()
	if err := c.Validate(false); err != nil {
		t.Fatal(err)
	}
	if c.Validate(true) == nil {
		t.Fatal("production plaintext must be explicit")
	}
	c.Address = ":9090"
	if c.Validate(false) == nil {
		t.Fatal("non-loopback plaintext must be explicit")
	}
	c.AllowPlaintext = true
	if err := c.Validate(true); err != nil {
		t.Fatal(err)
	}
	c.TLSCertFile = "cert.pem"
	if c.Validate(false) == nil {
		t.Fatal("incomplete TLS pair")
	}
	c.TLSKeyFile = "key.pem"
	if err := c.Validate(true); err != nil {
		t.Fatal(err)
	}
	c.CallTimeout = 0
	if c.Validate(true) == nil {
		t.Fatal("zero timeout")
	}
}
func TestClientConfiguration(t *testing.T) {
	c := DefaultClientConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.MaxMessageBytes = 0
	if c.Validate() == nil {
		t.Fatal("invalid limit")
	}
}
func TestServerEnvironment(t *testing.T) {
	t.Setenv("RPC_ADDRESS", "127.0.0.1:0")
	t.Setenv("RPC_CALL_TIMEOUT", "9s")
	c, err := LoadServerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.CallTimeout != 9*time.Second || c.Address != "127.0.0.1:0" {
		t.Fatalf("unexpected config: %+v", c)
	}
	t.Setenv("RPC_MAX_CONCURRENT", "")
	if _, err := LoadServerConfig(); err == nil {
		t.Fatal("explicit empty setting accepted")
	}
}
func TestTLSMissingFiles(t *testing.T) {
	c := DefaultServerConfig()
	c.TLSCertFile = t.TempDir() + "/no-cert"
	c.TLSKeyFile = c.TLSCertFile
	if _, err := serverTLS(c); err == nil {
		t.Fatal("missing key pair accepted")
	}
	client := DefaultClientConfig()
	client.CAFile = c.TLSCertFile
	if _, err := clientTLS(client); err == nil {
		t.Fatal("missing CA accepted")
	}
}

func TestHeaderLimitOverflow(t *testing.T) {
	c := DefaultServerConfig()
	c.MaxHeaderBytes = int(^uint(0) >> 1)
	if c.Validate(false) == nil {
		t.Fatal("overflowing header limit accepted")
	}
	c.MaxHeaderBytes = int(^uint(0)>>1) - 4096
	if err := c.Validate(false); err != nil {
		t.Fatal(err)
	}
}
