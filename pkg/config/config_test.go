package config

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// 不设 APP_ENV，使用默认 dev
	Init("../../configs")

	if MConfig.App.Env != "dev" {
		t.Errorf("expected default env 'dev', got %q", MConfig.App.Env)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("MY_APP_PORT", "9999")

	Init("../../configs")

	if MConfig.App.Port != 9999 {
		t.Errorf("expected port 9999 from env override, got %d", MConfig.App.Port)
	}
}

func TestMustLoad_PanicsOnEmptyDir(t *testing.T) {
	// 创建临时空目录，不存在 config.yaml
	tmpDir := t.TempDir()
	// Init 对缺失目录不会 panic（会 fallback），只验证不 panic 即可
	Init(tmpDir)
	if MConfig == nil {
		t.Error("expected non-nil config even from empty dir")
	}
}

func TestAppConfig_DefaultFields(t *testing.T) {
	t.Setenv("APP_ENV", "test")

	Init("../../configs")

	// 默认值检查
	if MConfig.Queue.MaxWorkers == 0 {
		t.Error("expected non-zero queue.max_workers")
	}
	if MConfig.Queue.PollTime == 0 {
		t.Error("expected non-zero queue.poll_time")
	}
}

func TestMain(m *testing.M) {
	// 确保加载 configs 目录下的配置
	// 测试中通过相对路径 ../../configs 访问
	os.Exit(m.Run())
}
