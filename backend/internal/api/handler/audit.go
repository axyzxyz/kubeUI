package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/pagination"
	"github.com/v911/backend/internal/service"
)

// GET /api/v1/audit-logs(管理员;审计记录只读,无删除接口)
func listAuditLogs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := pagination.Parse(c.Request.URL.Query())
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid pagination").WithCause(err))
			return
		}
		page, err := d.Audit.List(c.Request.Context(), service.AuditQuery{
			Username: c.Query("username"),
			Cluster:  c.Query("cluster"),
			Action:   c.Query("action"),
		}, p)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, page)
	}
}
