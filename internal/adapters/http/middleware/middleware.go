// Package middleware 提供基于 gin 的 HTTP 中间件。
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"my_project/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Recover 兜底 panic：写 500 并记日志，避免 goroutine 崩溃。
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rv := recover(); rv != nil {
				logger.Error("http: panic recovered",
					slog.Any("panic", rv),
					slog.String("path", c.Request.URL.Path),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

// Logging 请求耗时日志。
func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("http",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("elapsed", time.Since(start)),
		)
	}
}

// Auth 占位：真实项目按鉴权方案补齐（JWT / Session / API Key 等）。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: 校验 Authorization / Cookie
		c.Next()
	}
}
