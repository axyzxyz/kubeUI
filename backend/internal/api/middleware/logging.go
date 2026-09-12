package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/pkg/logx"
)

// Logging 输出结构化访问日志(method、path、status、耗时、request_id)。
func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logx.Info(c.Request.Context(), "http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString(requestIDKey),
			"sourceIp", c.ClientIP(),
		)
	}
}
