// Package logger 提供全局结构化日志记录器
//
// 基于 log/slog + lumberjack，支持 JSON/Text 两种输出格式，
// 支持按大小自动轮转日志文件，可在运行时动态调整日志级别。
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"

	"my_project/pkg/utils"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level 定义日志级别，映射到 slog.Level
type Level = slog.Level

// 预定义的日志级别
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Format 日志输出格式
type Format string

const (
	// FormatJSON 输出 JSON 格式日志，适合生产环境
	FormatJSON Format = "json"
	// FormatText 输出控制台文本格式日志，适合开发环境
	FormatText Format = "text"
)

// RotationConfig 日志轮转配置（基于 lumberjack）
type RotationConfig struct {
	// Filename 日志文件路径，例如 "logs/app.log"
	Filename string
	// MaxSize 单个日志文件最大大小（MB），超过后自动轮转
	MaxSize int
	// MaxAge 日志文件最大保留天数（0 表示不限制）
	MaxAge int
	// MaxBackups 最大保留的旧日志文件数（0 表示保留所有）
	MaxBackups int
	// Compress 是否使用 gzip 压缩旧日志文件
	Compress bool
}

// Config 日志配置
type Config struct {
	// Level 最低输出级别，低于此级别的日志将被忽略
	Level Level
	// Format 日志输出格式
	Format Format
	// Output 日志输出目标，优先级最高
	// 如果为 nil 且 Rotation.Filename 为空，默认使用 os.Stdout
	Output io.Writer
	// AddSource 是否在日志中添加调用位置（文件:行号），开发环境建议开启
	AddSource bool
	// Attrs 全局附加字段，每条日志都会携带
	Attrs []slog.Attr
	// Rotation 日志轮转配置，Filename 非空时生效
	Rotation RotationConfig
}

// DefaultConfig 返回适合开发环境的默认配置
func DefaultConfig() Config {
	return Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		AddSource: true,
		Rotation: RotationConfig{
			Filename:   "logs/app.log",
			MaxSize:    1,
			MaxAge:     10,
			MaxBackups: 50,
			Compress:   true,
		},
	}
}

// ProdConfig 返回适合生产环境的配置
func ProdConfig() Config {
	return Config{
		Level:  LevelInfo,
		Format: FormatJSON,
	}
}

// ── 全局状态 ──────────────────────────────────────────

var (
	globalLogger atomic.Pointer[slog.Logger]
	globalLevel  *slog.LevelVar
	globalWriter io.Writer
)

func init() {
	if utils.GetEnv() != utils.PROD {
		SetDefault(DefaultConfig())
	} else {
		SetDefault(ProdConfig())
	}
}

// SetDefault 使用指定配置替换全局 logger
//
// 应在应用启动阶段调用，并发安全。
func SetDefault(cfg Config) {
	lv := &slog.LevelVar{}
	lv.Set(cfg.Level)

	writer := buildWriter(cfg)
	handler := buildHandler(cfg, lv, writer)

	logger := slog.New(handler)
	if len(cfg.Attrs) > 0 {
		logger = logger.With(attrsToArgs(cfg.Attrs)...)
	}

	globalLevel = lv
	globalWriter = writer
	globalLogger.Store(logger)
}

// L 返回全局 logger 实例
func L() *slog.Logger { return globalLogger.Load() }

// ── 便捷函数 ──────────────────────────────────────────

// Debug 输出 Debug 级别日志
func Debug(msg string, attrs ...slog.Attr) { L().Debug(msg, attrsToArgs(attrs)...) }

// Info 输出 Info 级别日志
func Info(msg string, attrs ...slog.Attr) { L().Info(msg, attrsToArgs(attrs)...) }

// Warn 输出 Warn 级别日志
func Warn(msg string, attrs ...slog.Attr) { L().Warn(msg, attrsToArgs(attrs)...) }

// Error 输出 Error 级别日志
func Error(msg string, attrs ...slog.Attr) { L().Error(msg, attrsToArgs(attrs)...) }

// With 派生一个携带额外字段的 logger
func With(attrs ...slog.Attr) *slog.Logger { return L().With(attrsToArgs(attrs)...) }

// WithGroup 派生一个按名称分组的 logger
func WithGroup(name string) *slog.Logger { return L().WithGroup(name) }

// SetLevel 动态调整全局 logger 的最低输出级别
func SetLevel(level Level) {
	if globalLevel != nil {
		globalLevel.Set(level)
	}
}

// Sync 刷新缓冲区，确保日志写入完成
// 应用退出前应调用此函数
func Sync() {
	// slog 本身无 Sync 概念；若底层 writer 实现了 Sync（如 *os.File），则调用之。
	// lumberjack 内部不缓冲，无需 Sync。
	if w, ok := globalWriter.(interface{ Sync() error }); ok {
		if err := w.Sync(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "logger: sync failed: %v\n", err)
		}
	}
}

// ── 内部构造器 ────────────────────────────────────────

// buildHandler 根据格式创建对应的 slog.Handler
func buildHandler(cfg Config, level slog.Leveler, w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	if cfg.Format == FormatJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// buildWriter 根据配置创建写入目标
//
// 优先级：Output > Rotation.Filename (lumberjack) > os.Stdout
func buildWriter(cfg Config) io.Writer {
	// Priority 1: 自定义 io.Writer（测试兼容）
	if cfg.Output != nil {
		return cfg.Output
	}

	// Priority 2: lumberjack 文件轮转
	if cfg.Rotation.Filename != "" {
		return &lumberjack.Logger{
			Filename:   cfg.Rotation.Filename,
			MaxSize:    cfg.Rotation.MaxSize,
			MaxAge:     cfg.Rotation.MaxAge,
			MaxBackups: cfg.Rotation.MaxBackups,
			Compress:   cfg.Rotation.Compress,
			LocalTime:  true,
		}
	}

	// Priority 3: 默认标准输出
	return os.Stdout
}

// attrsToArgs 将 []slog.Attr 转为 slog.Logger 方法所需的可变参数
func attrsToArgs(attrs []slog.Attr) []any {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return args
}
