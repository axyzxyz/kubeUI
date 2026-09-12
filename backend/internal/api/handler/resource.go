package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/middleware"
	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/pkg/pagination"
	"github.com/v911/backend/internal/service"
)

// nsOf 取命名空间参数(rest.md §4:命名空间经 query 传递)。
func nsOf(c *gin.Context) string { return c.Query("namespace") }

// listResourceQuery 是资源列表的 query 参数集合。
type listResourceQuery struct {
	Namespace     string `form:"namespace"`
	LabelSelector string `form:"labelSelector"`
	FieldSelector string `form:"fieldSelector"`
	Keyword       string `form:"keyword"`
	SortBy        string `form:"sortBy"`
	Order         string `form:"order"`
}

// GET /api/v1/clusters/:cluster/:resource
func listResources(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var q listResourceQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid query"))
			return
		}
		p, err := pagination.Parse(c.Request.URL.Query())
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid pagination").WithCause(err))
			return
		}
		body, err := d.Resources.List(c.Request.Context(), c.Param("cluster"), c.Param("resource"), service.ListQuery{
			Namespace:     q.Namespace,
			LabelSelector: q.LabelSelector,
			FieldSelector: q.FieldSelector,
			SortBy:        q.SortBy,
			Order:         q.Order,
			Keyword:       q.Keyword,
			Page:          p.Page,
			Size:          p.Size,
		})
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, body)
	}
}

// GET /api/v1/clusters/:cluster/:resource/:name
// Secret 详情默认脱敏;?reveal=true 由 operator 及以上使用并走审计。
func getResource(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		reveal := c.Query("reveal") == "true"
		if reveal {
			if !operatorOrAbove(c) {
				response.Fail(c, errcode.New(errcode.Forbidden, "secret reveal requires operator privilege"))
				return
			}
			recordSecretRevealAudit(d, c)
		}
		if c.Query("decode") == "true" {
			if !reveal {
				response.Fail(c, errcode.New(errcode.ParamInvalid, "decode requires reveal=true"))
				return
			}
			data, err := d.Resources.SecretData(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Param("name"))
			if err != nil {
				response.Fail(c, err)
				return
			}
			response.OK(c, gin.H{"name": c.Param("name"), "namespace": nsOf(c), "data": data})
			return
		}
		obj, err := d.Resources.Get(c.Request.Context(), c.Param("cluster"), c.Param("resource"), nsOf(c), c.Param("name"), reveal)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, obj)
	}
}

// GET /api/v1/clusters/:cluster/:resource/:name/yaml
func getResourceYAML(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		text, err := d.Resources.GetYAML(c.Request.Context(), c.Param("cluster"), c.Param("resource"), nsOf(c), c.Param("name"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"yaml": text})
	}
}

// putResourceYAMLRequest 是 YAML 编辑请求体。
type putResourceYAMLRequest struct {
	YAML string `json:"yaml" binding:"required"`
}

// PUT /api/v1/clusters/:cluster/:resource/:name/yaml
func putResourceYAML(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req putResourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "yaml is required"))
			return
		}
		obj, err := d.Resources.UpdateYAML(c.Request.Context(), c.Param("cluster"), c.Param("resource"), nsOf(c), c.Param("name"), req.YAML)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, obj)
	}
}

// POST /api/v1/clusters/:cluster/:resource?namespace=
// 请求体为 K8s 资源 JSON 或 YAML 文本(按 Content-Type 区分,服务端均兼容);
// 危险操作,走审计(action=create)。namespace 优先级:body.metadata.namespace >
// query.namespace。
func createResource(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "read request body failed").WithCause(err))
			return
		}
		obj, err := d.Resources.CreateResource(c.Request.Context(), c.Param("cluster"), c.Param("resource"), nsOf(c), string(body))
		if err != nil {
			response.Fail(c, err)
			return
		}
		c.Set("createdResourceName", obj.GetName())
		c.Set("createdNamespace", obj.GetNamespace())
		response.OK(c, obj)
	}
}

// DELETE /api/v1/clusters/:cluster/:resource/:name(危险操作,v1 组审计中间件采集)
func deleteResource(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := d.Resources.Delete(c.Request.Context(), c.Param("cluster"), c.Param("resource"), nsOf(c), c.Param("name"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// POST /api/v1/clusters/:cluster/pods/:name/restart
func restartPod(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := d.Resources.RestartPod(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Param("name")); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// POST /api/v1/clusters/:cluster/deployments/:name/restart
func restartDeployment(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		obj, err := d.Resources.RestartDeployment(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Param("name"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, obj)
	}
}

// PUT /api/v1/clusters/:cluster/deployments/:name/scale
func scaleDeployment(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.ScaleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "replicas is required"))
			return
		}
		obj, err := d.Resources.ScaleDeployment(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Param("name"), req.Replicas)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, obj)
	}
}

// GET /api/v1/clusters/:cluster/pods/:name/logs
func podLogs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		q := service.PodLogsQuery{
			Container: c.Query("container"),
			Previous:  c.Query("previous") == "true",
		}
		if v := c.Query("tailLines"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid tailLines"))
				return
			}
			q.TailLines = n
		}
		if v := c.Query("sinceSeconds"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid sinceSeconds"))
				return
			}
			q.SinceSeconds = n
		}
		logs, err := d.Resources.PodLogs(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Param("name"), q)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, logs)
	}
}

// GET /api/v1/clusters/:cluster/events
func listEvents(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := pagination.Parse(c.Request.URL.Query())
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid pagination").WithCause(err))
			return
		}
		body, err := d.Resources.Events(c.Request.Context(), c.Param("cluster"), nsOf(c), c.Query("fieldSelector"), p)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, body)
	}
}

// GET /api/v1/clusters/:cluster/metrics/nodes
func nodeMetrics(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Resources.NodeMetrics(c.Request.Context(), c.Param("cluster"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"items": items})
	}
}

// GET /api/v1/clusters/:cluster/metrics/pods
func podMetrics(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Resources.PodMetrics(c.Request.Context(), c.Param("cluster"), nsOf(c))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"items": items})
	}
}

// operatorOrAbove 判断当前用户是否 operator/admin(Secret 明文查看门槛,01 §7.3)。
func operatorOrAbove(c *gin.Context) bool {
	ident, ok := middleware.IdentityOf(c)
	return ok && (ident.Role == "admin" || ident.Role == "operator")
}

// recordSecretRevealAudit 为 Secret 明文查看写一条显式审计。
func recordSecretRevealAudit(d Deps, c *gin.Context) {
	ident, _ := middleware.IdentityOf(c)
	entry := service.AuditEntry{
		RequestID:    c.GetString("requestId"),
		UserID:       ident.UserID,
		Username:     ident.Username,
		Action:       "reveal-secret",
		Resource:     "secrets",
		ResourceType: "secret",
		Cluster:      c.Param("cluster"),
		Namespace:    nsOf(c),
		Name:         c.Param("name"),
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		Result:       "allow",
	}
	if err := d.Audit.Record(c.Request.Context(), entry); err != nil {
		// 审计失败不阻断业务,但必须可见。
		logx.Warn(c.Request.Context(), "secret reveal audit record failed", "err", err)
	}
}
