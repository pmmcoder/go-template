package ai

import (
	"my_project/pkg/config"

	"my_project/internal/domain/task"
)

// Endpoint 是调度器持有的一个可调用单元（一个厂商实例 + 调度元数据）。
type Endpoint struct {
	Provider    task.AIProvider // 实际厂商实例
	Name        string          // 用于日志，默认取注册名；同一注册名多实例时由 Label 区分
	Models      []string        // 路由命中的模型集合；空表示兜底（任意模型）
	Weight      int             // 加权选择权重，<=0 视为 1
	MaxInflight int             // 该端点并发上限；<=0 表示不限
}

// BuildEndpoint 把配置实例化为可用 Endpoint。
func BuildEndpoint(ec config.EndpointConfig) (Endpoint, error) {
	p, err := Build(ec.Name, ec.Config)
	if err != nil {
		return Endpoint{}, err
	}
	w := ec.Weight
	if w <= 0 {
		w = 1
	}
	label := ec.Label
	if label == "" {
		label = ec.Name
	}
	return Endpoint{
		Provider:    p,
		Name:        label,
		Models:      ec.Models,
		Weight:      w,
		MaxInflight: ec.MaxInflight,
	}, nil
}
