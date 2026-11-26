package orm

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// safeLogger deliberately never evaluates GORM's SQL formatter and never logs
// its free-form messages or arguments (including database error strings).
type safeLogger struct {
	log   *slog.Logger
	level gormlog.LogLevel
	slow  time.Duration
}

var _ gormlog.Interface = safeLogger{}

func (l safeLogger) LogMode(level gormlog.LogLevel) gormlog.Interface { l.level = level; return l }
func (l safeLogger) Info(ctx context.Context, _ string, _ ...any) {
	if l.level >= gormlog.Info {
		l.log.InfoContext(ctx, "GORM information")
	}
}
func (l safeLogger) Warn(ctx context.Context, _ string, _ ...any) {
	if l.level >= gormlog.Warn {
		l.log.WarnContext(ctx, "GORM warning")
	}
}
func (l safeLogger) Error(ctx context.Context, _ string, _ ...any) {
	if l.level >= gormlog.Error {
		l.log.ErrorContext(ctx, "GORM error")
	}
}
func (l safeLogger) Trace(ctx context.Context, begin time.Time, _ func() (string, int64), err error) {
	duration := time.Since(begin)
	if l.level <= gormlog.Silent {
		return
	}
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		if l.level >= gormlog.Error {
			l.log.ErrorContext(ctx, "GORM query failed", "duration", duration)
		}
	case l.level >= gormlog.Warn && l.slow > 0 && duration > l.slow:
		l.log.WarnContext(ctx, "GORM slow query", "duration", duration)
	case l.level >= gormlog.Info:
		l.log.DebugContext(ctx, "GORM query completed", "duration", duration)
	}
}
