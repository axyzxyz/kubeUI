// Package middleware 提供平台 HTTP 中间件:requestid / logging / recovery / auth / audit。
package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDKey = "requestId"

// RequestID 为每个请求生成或透传 X-Request-Id,并写入 context。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-Id", id)
		// 让 logx 通过 ctx 拿到 request_id。
		ctx := context.WithValue(c.Request.Context(), requestIDCtxKey{}, id)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// FromContext 返回请求链路中的 request id。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

type requestIDCtxKey struct{}
