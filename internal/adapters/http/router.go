// Package http 装配 HTTP 路由树 + 中间件栈（gin 版本）。
//
// 各聚合的路由分散在子包（如 ./task、./user）里各自实现 Register(r, svc)，
// 本文件只做「按聚合调用 Register」的组合工作——新增聚合只加两处：
//  1. Services 结构体加一个字段
//  2. New() 内加一行 <聚合>.Register(r, svcs.<字段>)
//
// 命名说明：包目录为 http，与标准库 net/http 同名。使用者建议 alias：
//
//	import httpadapter "my_project/internal/adapters/http"
package http

import (
	"my_project/internal/adapters/http/middleware"
	taskroute "my_project/internal/adapters/http/task"
	apptask "my_project/internal/application/task"

	"github.com/gin-gonic/gin"
)

// Services 汇总 HTTP 层依赖的所有 application service。
// 新增聚合就在这里加字段，New 的签名保持稳定。
type Services struct {
	Task *apptask.Service
	// User *appuser.Service
	// Rule *apprule.Service
}

// New 装配整个 HTTP 路由树 + 中间件栈，返回 *gin.Engine（实现 http.Handler）。
func New(svcs Services) *gin.Engine {
	r := gin.New()
	r.Use(
		middleware.Recover(),
		middleware.Logging(),
		middleware.Auth(),
	)

	taskroute.Register(r, svcs.Task)
	// userroute.Register(r, svcs.User)
	// ruleroute.Register(r, svcs.Rule)

	return r
}
