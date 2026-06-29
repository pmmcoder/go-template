// Package config 提供应用配置加载功能
//
// 基于 viper，从 configs/ 目录加载 YAML 配置文件，
// 支持多环境（dev/test/prod），环境变量覆盖，以及 .env 文件。
package config

import (
	"errors"
	"fmt"
	"sync"

	"my_project/pkg/utils"

	"github.com/spf13/viper"
)

var (
	MConfig *Config
	once    sync.Once
)

// Config 应用总配置
type Config struct {
	App    AppConfig    `mapstructure:"app"`
	Logger LoggerConfig `mapstructure:"logger"`
	DB     DBConfig     `mapstructure:"db"`
	Queue  QueueConfig  `mapstructure:"queue"`
	Cache  CacheConfig  `mapstructure:"cache"`
}

// AppConfig 应用基础配置
type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
	Port int    `mapstructure:"port"`
}

// LoggerConfig 日志配置，可转换为 logger.Config
type LoggerConfig struct {
	Level     string         `mapstructure:"level"`
	Format    string         `mapstructure:"format"`
	AddSource bool           `mapstructure:"add_source"`
	Rotation  RotationConfig `mapstructure:"rotation"`
}

// RotationConfig 日志轮转配置
type RotationConfig struct {
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxAge     int    `mapstructure:"max_age"`
	MaxBackups int    `mapstructure:"max_backups"`
	Compress   bool   `mapstructure:"compress"`
}

// DBConfig 数据库配置
type DBConfig struct {
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// QueueConfig 任务队列配置
type QueueConfig struct {
	MaxWorkers  int `mapstructure:"max_workers"`
	PollTime    int `mapstructure:"poll_time"`
	MaxAttempts int `mapstructure:"max_attempts"`
	JobTimeout  int `mapstructure:"job_timeout"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// load 加载配置
func load(configDir string) (*Config, error) {
	v := viper.New()
	env := utils.GetEnv()

	// 1. 环境配置覆盖
	v.SetConfigName("config." + env)
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("config: merge env config (%s): %w", env, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
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
