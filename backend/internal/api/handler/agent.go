package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/api/response"
	"github.com/axyzxyz/kubeui/backend/internal/config"
	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
)

// createEnrollTokenRequest 是创建 Enrollment Token 请求体。
type createEnrollTokenRequest struct {
	Cluster string `json:"cluster" binding:"required"`
	// TTL 有效期:""默认 24h;"permanent"长期;
	// 或 "<n><单位>":h(小时)/d(天)/mo(月=30d)/y(年=365d),上限 30y。
	TTL string `json:"ttl"`
	// TTLHours 旧字段,兼容保留:ttl 为空且 >0 时按小时换算。
	TTLHours int `json:"ttlHours"`
}

// GET /api/v1/agent/connect — Agent 反连信令通道(鉴权走 Enrollment Token 首帧)。
func agentConnect(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		d.AgentHub.ServeSignaling()(c.Writer, c.Request)
	}
}

// GET /api/v1/agent/tunnel?connId=&sessionKey= — Agent 反连数据通道。
func agentTunnel(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		d.AgentHub.ServeData()(c.Writer, c.Request)
	}
}

// GET /api/v1/enroll-tokens — 列出 Enrollment Token(脱敏)。
func listEnrollTokens(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Enroll.List(c.Request.Context())
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"items": items, "total": len(items)})
	}
}

// POST /api/v1/enroll-tokens — 创建一次性 Enrollment Token(admin)。
// token 原文只在本次响应中出现。
func createEnrollToken(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createEnrollTokenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "cluster is required"))
			return
		}
		ttl := req.TTL
		if ttl == "" && req.TTLHours > 0 {
			ttl = strconv.Itoa(req.TTLHours) + "h"
		}
		created, err := d.Enroll.Create(c.Request.Context(), req.Cluster, ttl)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, created)
	}
}

// DELETE /api/v1/enroll-tokens/:id — 吊销(admin,危险操作走审计)。
func deleteEnrollToken(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid token id"))
			return
		}
		if err := d.Enroll.Revoke(c.Request.Context(), id); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/agent/manifest?token= — 渲染 Agent 部署 YAML
// (kubectl apply -f -;响应包含 token 原文,由调用方妥善保管)。
func agentManifest(d Deps, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "token is required"))
			return
		}
		yaml, err := d.Enroll.RenderAgentManifest(c.Request.Context(), token, cfg.Server.ExternalURL)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, model.AgentManifest{YAML: yaml, ServerURL: cfg.Server.ExternalURL})
	}
}
