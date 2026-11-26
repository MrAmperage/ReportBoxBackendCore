package orm

import (
	"bytes"
	"context"
	"errors"
	gormlog "gorm.io/gorm/logger"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoggerDoesNotExposeSQLOrErrors(t *testing.T) {
	var b bytes.Buffer
	l := safeLogger{log: slog.New(slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})), level: gormlog.Info, slow: time.Second}
	ctx := context.Background()
	l.Info(ctx, "secret %v", "password")
	l.Error(ctx, "secret", errors.New("private"))
	l.Trace(ctx, time.Now(), func() (string, int64) { t.Fatal("SQL formatter called"); return "secret", 1 }, errors.New("private"))
	s := b.String()
	for _, v := range []string{"secret", "password", "private"} {
		if strings.Contains(s, v) {
			t.Fatal("leaked sensitive value")
		}
	}
	if !strings.Contains(s, "GORM query failed") {
		t.Fatal("missing error log")
	}
}
