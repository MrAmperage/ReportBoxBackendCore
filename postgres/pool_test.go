package postgres

import (
	"crypto/tls"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
)

func TestEffectiveDriverConfiguration(t *testing.T) {
	// The parser may load user TLS defaults. Isolate them and the libpq options
	// that could otherwise affect this test on a developer machine.
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"PGSERVICE", "PGOPTIONS", "PGCONNECT_TIMEOUT", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGSSLNEGOTIATION", "PGTARGETSESSIONATTRS", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGCHANNELBINDING", "PGREQUIREAUTH"} {
		t.Setenv(key, "")
	}
	t.Setenv("PGSSLCERTMODE", "allow")
	t.Setenv("PGPORT", "6432")
	t.Setenv("PGTZ", "Asia/Tokyo")
	t.Setenv("PGAPPNAME", "ambient-name")
	c := DefaultConfig()
	c.DSN = "postgres://u:p@db.example/db?sslmode=verify-full"
	c.ConnectTimeout = 1234 * time.Millisecond
	reg := health.New(func() health.Phase { return health.Idle }, time.Second)
	p, err := newPool(c, true, app.Info{Name: "explicit-name"}, slog.New(slog.NewTextHandler(io.Discard, nil)), reg)
	if err != nil {
		t.Fatal(err)
	}
	cc := p.cfg.ConnConfig
	if cc.Port != 5432 || cc.Host != "db.example" || cc.ConnectTimeout != c.ConnectTimeout {
		t.Fatal("unexpected effective endpoint or timeout")
	}
	if cc.TLSConfig == nil || cc.TLSConfig.InsecureSkipVerify || cc.TLSConfig.MinVersion != tls.VersionTLS12 || cc.TLSConfig.ServerName != "db.example" {
		t.Fatal("unexpected TLS policy")
	}
	if cc.RuntimeParams["timezone"] != "UTC" || cc.RuntimeParams["application_name"] != "explicit-name" {
		t.Fatal("ambient settings changed the session policy")
	}
	if _, err := p.Native(); err == nil {
		t.Fatal("pool opened during graph construction")
	}
}
