// Package config 提供应用配置加载功能
//
// 基于 viper，从 configs/ 目录加载 YAML 配置文件，
// 支持多环境（dev/test/prod），环境变量覆盖，以及 .env 文件。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"my_project/pkg/utils"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

var (
	MConfig *Config
	once    sync.Once
)

// Config 应用总配置
type Config struct {
	App    AppConfig    `mapstructure:"app" yaml:"app"`
	Logger LoggerConfig `mapstructure:"logger" yaml:"logger"`
	DB     DBConfig     `mapstructure:"db" yaml:"db"`
	Queue  QueueConfig  `mapstructure:"queue" yaml:"queue"`
	Cache  CacheConfig  `mapstructure:"cache" yaml:"cache"`
	AI     AIConfig     `mapstructure:"ai" yaml:"ai"`
}

// AppConfig 应用基础配置
type AppConfig struct {
	Name string `mapstructure:"name" yaml:"name"`
	Env  string `mapstructure:"env" yaml:"env"`
	Port int    `mapstructure:"port" yaml:"port"`
}

// LoggerConfig 日志配置，可转换为 logger.Config
type LoggerConfig struct {
	Level     string         `mapstructure:"level" yaml:"level"`
	Format    string         `mapstructure:"format" yaml:"format"`
	AddSource bool           `mapstructure:"add_source" yaml:"add_source"`
	Rotation  RotationConfig `mapstructure:"rotation" yaml:"rotation"`
}

// RotationConfig 日志轮转配置
type RotationConfig struct {
	Filename   string `mapstructure:"filename" yaml:"filename"`
	MaxSize    int    `mapstructure:"max_size" yaml:"max_size"`
	MaxAge     int    `mapstructure:"max_age" yaml:"max_age"`
	MaxBackups int    `mapstructure:"max_backups" yaml:"max_backups"`
	Compress   bool   `mapstructure:"compress" yaml:"compress"`
}

// DBConfig 数据库配置
type DBConfig struct {
	DSN          string `mapstructure:"dsn" yaml:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
}

// QueueConfig 任务队列配置
type QueueConfig struct {
	MaxWorkers  int `mapstructure:"max_workers" yaml:"max_workers"`
	PollTime    int `mapstructure:"poll_time" yaml:"poll_time"`
	MaxAttempts int `mapstructure:"max_attempts" yaml:"max_attempts"`
	JobTimeout  int `mapstructure:"job_timeout" yaml:"job_timeout"`
}

type AIConfig struct {
	Retry     RetryConfig      `json:"retry" mapstructure:"retry" yaml:"retry"`
	Endpoints []EndpointConfig `json:"endpoints"  mapstructure:"endpoints" yaml:"endpoints"`
}

type RetryConfig struct {
	MaxAttempts int `json:"max_attempts" mapstructure:"max_attempts" yaml:"max_attempts"`
	BaseDelayMS int `json:"base_delay_ms" mapstructure:"base_delay_ms" yaml:"base_delay_ms"`
	MaxDelayMS  int `json:"max_delay_ms" mapstructure:"max_delay_ms" yaml:"max_delay_ms"`
}

type EndpointConfig struct {
	Name        string          `json:"name" mapstructure:"name" yaml:"name"`                         // 注册名（如 "openai" / "openai-azure"）
	Label       string          `json:"label,omitempty" mapstructure:"label" yaml:"label"`            // 可选：同名多实例时的区分标签
	Weight      int             `json:"weight" mapstructure:"weight" yaml:"weight"`                   // 权重，<=0 视为 1
	MaxInflight int             `json:"max_inflight" mapstructure:"max_inflight" yaml:"max_inflight"` // 并发上限，<=0 表示不限
	Models      []string        `json:"models" mapstructure:"models" yaml:"models"`                   // 路由模型；空=兜底
	Config      json.RawMessage `json:"config" mapstructure:"config" yaml:"config"`                   // 厂商私有配置，原样透传给 Builder
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Addr     string `mapstructure:"addr" yaml:"addr"`
	Password string `mapstructure:"password" yaml:"password"`
	DB       int    `mapstructure:"db" yaml:"db"`
}

// load 加载配置
func load(configDir string) (*Config, error) {
	v := viper.New()
	env := utils.GetEnv()

	// 1. 环境配置覆盖
	v.SetConfigName("config." + env)
	v.SetConfigType("yaml")
	if configDir == "" {
		if root := os.Getenv("ROOT_PATH"); root != "" {
			configDir = filepath.Join(root, "configs")
		} else {
			configDir = filepath.Join("./", "configs")
		}
	}
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: merge env config (%s): %w", env, err)
	}

	var cfg Config
	decoderConfig := &mapstructure.DecoderConfig{
		Result:  &cfg,
		TagName: "mapstructure",
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			RawMessageHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	}
	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return nil, err
	}

	if err := decoder.Decode(v.AllSettings()); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	return &cfg, nil
}

// Init 加载配置，失败则 panic（适合启动阶段）
func Init(configDir string) {
	once.Do(func() {
		cfg, err := load(configDir)
		if err != nil {
			panic(fmt.Sprintf("config: load failed: %v", err))
		}
		MConfig = cfg
	})
}

func RawMessageHookFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{},
	) (interface{}, error) {
		if t != reflect.TypeOf(json.RawMessage{}) {
			return data, nil
		}

		// 如果是 map，转换为 JSON
		switch v := data.(type) {
		case map[string]interface{}:
			jsonData, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(jsonData), nil
		case []interface{}:
			jsonData, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(jsonData), nil
		case string:
			return json.RawMessage(v), nil
		default:
			return data, nil
		}
	}
}
