// Package response 实现统一响应体 {code,message,data} 与错误映射出口。
package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
)

// Body 统一响应体。
type Body struct {
	Code    int    `json:"code"`    // 0 成功;非 0 见 errcode/codes.go
	Message string `json:"message"` // code=0 时固定 "ok"
	Data    any    `json:"data"`    // 失败时为 null
}

// OK 输出成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

// Fail 输出错误响应:*errcode.Error 按错误码映射;未知错误对外一律 50000。
func Fail(c *gin.Context, err error) {
	var ec *errcode.Error
	if errors.As(err, &ec) {
		c.JSON(errcode.HTTPStatusOf(ec.Code), Body{Code: ec.Code, Message: ec.Message})
		return
	}
	logx.Error(c.Request.Context(), "unhandled error", "err", err,
		"request_id", c.GetString("requestId"))
	c.JSON(http.StatusInternalServerError, Body{Code: errcode.InternalError, Message: "internal error"})
}
