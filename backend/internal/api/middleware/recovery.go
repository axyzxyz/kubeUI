package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
)

// Recovery 捕获 handler panic,记录日志并返回 50000 统一错误。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logx.Error(c.Request.Context(), "panic recovered", "request_id", c.GetString(requestIDKey))
		c.AbortWithStatusJSON(http.StatusInternalServerError, response.Body{
			Code:    errcode.InternalError,
			Message: "internal error",
		})
	})
}
