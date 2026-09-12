// Package logx 是 log/slog 的平台统一封装,所有日志只经由此包输出。
package logx

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// L 是全局 logger,仅在 main 启动时通过 Init 赋值一次。
var L *slog.Logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Init 初始化全局 logger;level 为 debug|info|warn|error,format 为 json|text。
func Init(level, format string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	var h slog.Handler
	opts := &slog.HandlerOptions{Level: lvl}
	if strings.EqualFold(format, "text") {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	L = slog.New(h)
}

// Debug 记录 debug 级别日志。
func Debug(ctx context.Context, msg string, args ...any) { L.DebugContext(ctx, msg, args...) }

// Info 记录 info 级别日志。
func Info(ctx context.Context, msg string, args ...any) { L.InfoContext(ctx, msg, args...) }

// Warn 记录 warn 级别日志。
func Warn(ctx context.Context, msg string, args ...any) { L.WarnContext(ctx, msg, args...) }

// Error 记录 error 级别日志。
func Error(ctx context.Context, msg string, args ...any) { L.ErrorContext(ctx, msg, args...) }
