package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/api/response"
	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
)

// issueKubeconfigRequest 是签发客户端 kubeconfig 请求体。
type issueKubeconfigRequest struct {
	// TTL 有效期,Go duration 字符串(如 "24h");默认 24h,上限 7d。
	TTL         string `json:"ttl"`
	Description string `json:"description"`
}

// POST /api/v1/clusters/:cluster/kubeconfigs — 签发短期凭证(危险操作,走审计)。
// 响应包含 token 原文与一次性下载链接,只此一次。
func issueKubeconfig(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		var req issueKubeconfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// 允许空 body:全部字段可省。
			req = issueKubeconfigRequest{}
		}
		ttl, err := parseTTL(req.TTL)
		if err != nil {
			response.Fail(c, err)
			return
		}
		created, err := d.Kubeconfigs.Issue(c.Request.Context(), ident.UserID, c.Param("cluster"), ttl, req.Description)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, created)
	}
}

// GET /api/v1/kubeconfigs — 当前用户的凭证列表(脱敏)。
func listMyKubeconfigs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		items, err := d.Kubeconfigs.ListForUser(c.Request.Context(), ident.UserID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"items": items, "total": len(items)})
	}
}

// DELETE /api/v1/kubeconfigs/:id — 撤销凭证(属主或 admin;走审计)。
func revokeKubeconfig(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid credential id"))
			return
		}
		if err := d.Kubeconfigs.Revoke(c.Request.Context(), id, ident.UserID, ident.Role == model.RoleAdmin); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/clusters/:cluster/kubeconfigs/download?code= — 一次性换取
// kubeconfig 文件;code 即鉴权,端点匿名可达。
func downloadKubeconfig(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		yaml, err := d.Kubeconfigs.Download(c.Request.Context(), c.Query("code"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		c.Header("Content-Disposition", "application/octet-stream; filename=kubeconfig")
		c.Data(200, "application/yaml", []byte(yaml))
	}
}

// parseTTL 解析 ttl 字符串;空串返回 0(由 service 填默认 24h)。
func parseTTL(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errcode.New(errcode.ParamInvalid, "invalid ttl, expect Go duration like 24h")
	}
	return d, nil
}
