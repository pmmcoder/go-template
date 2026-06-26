package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Level != LevelDebug {
		t.Errorf("expected LevelDebug, got %v", cfg.Level)
	}
	if cfg.Format != FormatText {
		t.Errorf("expected FormatText, got %v", cfg.Format)
	}
	if !cfg.AddSource {
		t.Error("expected AddSource to be true")
	}
	if cfg.Rotation.Filename != "" {
		t.Errorf("expected no Filename in DefaultConfig, got %v", cfg.Rotation.Filename)
	}
}

func TestProdConfig(t *testing.T) {
	cfg := ProdConfig()
	if cfg.Level != LevelInfo {
		t.Errorf("expected LevelInfo, got %v", cfg.Level)
	}
	if cfg.Format != FormatJSON {
		t.Errorf("expected FormatJSON, got %v", cfg.Format)
	}
	if cfg.AddSource {
		t.Error("expected AddSource to be false")
	}
	if cfg.Rotation.Filename != "logs/app.log" {
		t.Errorf("expected Filename 'logs/app.log', got %v", cfg.Rotation.Filename)
	}
	if cfg.Rotation.MaxSize != 100 {
		t.Errorf("expected MaxSize 100, got %d", cfg.Rotation.MaxSize)
	}
	if cfg.Rotation.MaxAge != 30 {
		t.Errorf("expected MaxAge 30, got %d", cfg.Rotation.MaxAge)
	}
	if cfg.Rotation.MaxBackups != 90 {
		t.Errorf("expected MaxBackups 90, got %d", cfg.Rotation.MaxBackups)
	}
	if !cfg.Rotation.Compress {
		t.Error("expected Compress to be true")
	}
}

func TestSetDefault_JSON(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	Info("hello", slog.String("key", "value"))
	output := buf.String()

	var m map[string]any
	if err := json.Unmarshal([]byte(output), &m); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if m["msg"] != "hello" {
		t.Errorf("expected msg 'hello', got %v", m["msg"])
	}
	if m["key"] != "value" {
		t.Errorf("expected key 'value', got %v", m["key"])
	}
	if m["level"] != "info" {
		t.Errorf("expected level 'info', got %v", m["level"])
	}
}

func TestSetDefault_Text(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatText,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	Info("hello text")
	output := buf.String()
	if !strings.Contains(output, "hello text") {
		t.Errorf("expected output to contain 'hello text', got: %s", output)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelWarn,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	Debug("should be filtered")
	Info("should be filtered")
	Warn("should appear")

	output := buf.String()

	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("expected valid JSON, got error: %v\nline: %s", err, line)
		}
		lines = append(lines, m)
	}

	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
	if lines[0]["msg"] != "should appear" {
		t.Errorf("expected msg 'should appear', got %v", lines[0]["msg"])
	}
}

func TestGlobalAttrs(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:  LevelDebug,
		Format: FormatJSON,
		Output: &buf,
		Attrs:  []slog.Attr{slog.String("service", "myapp"), slog.String("env", "test")},
	}
	SetDefault(cfg)

	Info("with attrs")
	output := buf.String()

	var m map[string]any
	if err := json.Unmarshal([]byte(output), &m); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if m["service"] != "myapp" {
		t.Errorf("expected service 'myapp', got %v", m["service"])
	}
	if m["env"] != "test" {
		t.Errorf("expected env 'test', got %v", m["env"])
	}
}

func TestConvenienceFuncs(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	Debug("debug msg")
	Info("info msg")
	Warn("warn msg")
	Error("error msg")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines, got %d", len(lines))
	}
}

func TestWith(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	log := With(slog.String("component", "cache"))
	log.Info("cache miss")

	output := buf.String()
	var m map[string]any
	if err := json.Unmarshal([]byte(output), &m); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if m["component"] != "cache" {
		t.Errorf("expected component 'cache', got %v", m["component"])
	}
}

func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	Info("before level change")
	SetLevel(LevelError)
	Info("should be filtered")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
}

func TestL_ReturnsLogger(t *testing.T) {
	SetDefault(DefaultConfig())
	log := L()
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithGroup(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:     LevelDebug,
		Format:    FormatJSON,
		Output:    &buf,
		AddSource: false,
	}
	SetDefault(cfg)

	log := WithGroup("db")
	log.Info("query", slog.String("iteration", "10"))

	output := buf.String()
	var m map[string]any
	if err := json.Unmarshal([]byte(output), &m); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, output)
	}
	if m["logger"] != "db" {
		t.Errorf("expected logger 'db', got %v", m["logger"])
	}
	if m["msg"] != "query" {
		t.Errorf("expected msg 'query', got %v", m["msg"])
	}
}

func TestOutFile(t *testing.T) {
	for i := 0; i < 10000; i++ {
		Info("query", slog.String("iteration", fmt.Sprintf("%d", i)))
	}
	time.Sleep(1 * time.Second) // 等待日志写入完成
}
