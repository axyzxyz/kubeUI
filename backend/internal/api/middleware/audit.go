package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
)

// Recorder 记录审计事件,由 service.AuditService 实现。
type Recorder interface {
	Record(c *gin.Context, entry AuditEntry) error
}

// AuditEntry 是一条待落库的审计事件。
type AuditEntry struct {
	Action       string // login|refresh|logout|write|delete|restart|scale|reveal-secret...
	ResourceType string // 友好资源类型(auth/user/deployment...),未知路径为原始 path
	Resource     string // 原始路由 path(gin FullPath)
	Cluster      string
	Namespace    string
	Name         string
	Username     string // 匿名端点(登录)取自请求体,登录失败同样落操作者
	Result       string // allow|deny
}

// authPathLogin/Refresh/logout 是匿名认证端点的路由模板。
const (
	authLoginPath  = "/api/v1/auth/login"
	authRefresh    = "/api/v1/auth/refresh"
	authLogoutPath = "/api/v1/auth/logout"
)

// clusterResourceCollectionPath 是同构资源集合创建端点的路由模板
// (POST /api/v1/clusters/:cluster/:resource,action=create)。
const clusterResourceCollectionPath = "/api/v1/clusters/:cluster/:resource"

// resourceTypeMap 维护路由模板 → 友好资源类型映射(BUG-07)。
var resourceTypeMap = map[string]string{
	authLoginPath:                                    "auth",
	authRefresh:                                      "auth",
	authLogoutPath:                                   "auth",
	"/api/v1/users":                                  "user",
	"/api/v1/users/:username":                        "user",
	"/api/v1/users/me":                               "user",
	"/api/v1/users/me/password":                      "user",
	"/api/v1/users/me/permissions":                   "user",
	"/api/v1/users/:username/status":                 "user",
	"/api/v1/users/:username/reset-password":         "user",
	"/api/v1/roles":                                  "role",
	"/api/v1/roles/:id":                              "role",
	"/api/v1/role-groups":                            "role-group",
	"/api/v1/role-groups/:id":                        "role-group",
	"/api/v1/role-groups/:id/roles":                  "role-group",
	"/api/v1/role-groups/:id/roles/:roleId":          "role-group",
	"/api/v1/grants":                                 "grant",
	"/api/v1/grants/:id":                             "grant",
	"/api/v1/user-groups":                            "user-group",
	"/api/v1/user-groups/:id":                        "user-group",
	"/api/v1/user-groups/:id/members":                "user-group",
	"/api/v1/user-groups/:id/members/:userId":        "user-group",
	"/api/v1/audit-logs":                             "audit-log",
	"/api/v1/enroll-tokens":                          "enroll-token",
	"/api/v1/enroll-tokens/:id":                      "enroll-token",
	"/api/v1/kubeconfigs":                            "kubeconfig",
	"/api/v1/kubeconfigs/:id":                        "kubeconfig",
	"/api/v1/clusters":                               "cluster",
	"/api/v1/clusters/:cluster":                      "cluster",
	"/api/v1/clusters/:cluster/status":               "cluster",
	"/api/v1/clusters/:cluster/kubeconfig":           "kubeconfig",
	"/api/v1/clusters/:cluster/kubeconfigs":          "kubeconfig",
	"/api/v1/clusters/:cluster/kubeconfigs/download": "kubeconfig",
}

// pluralResourceMap 将集群资源集合名映射为友好资源类型(BUG-07)。
var pluralResourceMap = map[string]string{
	"deployments":  "deployment",
	"statefulsets": "statefulset",
	"daemonsets":   "daemonset",
	"pods":         "pod",
	"services":     "service",
	"ingresses":    "ingress",
	"configmaps":   "configmap",
	"secrets":      "secret",
	"pvcs":         "persistent-volume-claim",
	"pvs":          "persistent-volume",
	"nodes":        "node",
	"namespaces":   "namespace",
	"crds":         "crd",
	"events":       "event",
}

// ResourceTypeOf 将路由模板映射为友好资源类型;未知路径保留原始 path。
// 集群资源路径(/api/v1/clusters/:cluster/:resource/...)的集合名取实际
// resource 路径参数;专用路由(如 /deployments/:name/restart)从模板段解析。
func ResourceTypeOf(path, resourceParam string) string {
	if v, ok := resourceTypeMap[path]; ok {
		return v
	}
	if rest, ok := strings.CutPrefix(path, "/api/v1/clusters/:cluster/"); ok {
		seg := rest
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if seg == ":resource" {
			seg = resourceParam
		}
		if v, ok := pluralResourceMap[seg]; ok {
			return v
		}
	}
	return path
}

// ActionOf 按 HTTP 方法与路径推导语义化 action(BUG-07:不再只有 write)。
func ActionOf(method, path string) string {
	switch {
	case path == authLoginPath:
		return "login"
	case path == authRefresh:
		return "refresh"
	case path == authLogoutPath:
		return "logout"
	case strings.HasSuffix(path, "/restart"):
		return "restart"
	case strings.HasSuffix(path, "/scale"):
		return "scale"
	case path == clusterResourceCollectionPath:
		return "create"
	}
	switch method {
	case http.MethodDelete:
		return "delete"
	default:
		return "write"
	}
}

// Audit 对写操作(POST/PUT/DELETE/PATCH)统一采集审计事件。
// resource 取路由 path,resourceType 为映射后的友好类型;cluster/name 取路径参数,
// namespace 取查询参数;登录/refresh 匿名端点从请求体或响应 JWT 提取操作者。
func Audit(rec Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		default:
			c.Next()
			return
		}

		// 登录/refresh:先缓存请求体以提取用户名(登录),再捕获响应以解析 JWT(refresh)。
		username := ""
		var respBuf *bytes.Buffer
		switch path {
		case authLoginPath:
			username = usernameFromRequestBody(c)
		case authRefresh:
			bw := &bodyCaptureWriter{ResponseWriter: c.Writer}
			c.Writer = bw
			respBuf = &bw.buf
		}
		c.Next()

		entry := AuditEntry{
			Action:       ActionOf(c.Request.Method, path),
			ResourceType: ResourceTypeOf(path, c.Param("resource")),
			Resource:     path,
			Cluster:      c.Param("cluster"),
			Namespace:    c.Query("namespace"),
			Name:         c.Param("name"),
			Username:     username,
			Result:       "allow",
		}
		// 资源创建:资源名/命名空间可能在请求体而非路径/query,由 handler
		// 创建成功后回填(createdResourceName/createdNamespace)。
		if path == clusterResourceCollectionPath {
			if v := c.GetString("createdResourceName"); v != "" {
				entry.Name = v
			}
			if v := c.GetString("createdNamespace"); v != "" {
				entry.Namespace = v
			}
		}
		if entry.Username == "" {
			if ident, ok := IdentityOf(c); ok {
				entry.Username = ident.Username
			}
		}
		if path == authRefresh && respBuf != nil && c.Writer.Status() < http.StatusBadRequest {
			entry.Username = usernameFromAccessTokenJWT(respBuf.Bytes())
		}
		if c.Writer.Status() >= http.StatusBadRequest {
			entry.Result = "deny"
		}
		if err := rec.Record(c, entry); err != nil {
			// 审计失败不阻断业务,但必须可见。
			logx.Warn(c.Request.Context(), "audit record failed",
				"err", err, "request_id", c.GetString(requestIDKey),
				"duration_ms", 0)
		}
	}
}

// usernameFromRequestBody 读取并回填请求体,提取登录用户名;登录失败同样落审计。
func usernameFromRequestBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	var req struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return ""
	}
	return req.Username
}

// usernameFromAccessTokenJWT 从成功响应体的 accessToken(未校验签名,仅审计标注)
// 解出操作者用户名。
func usernameFromAccessTokenJWT(respBody []byte) string {
	var body struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &body); err != nil || body.Data.AccessToken == "" {
		return ""
	}
	parts := strings.Split(body.Data.AccessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Username
}

// bodyCaptureWriter 捕获响应体,供 refresh 成功后解析 JWT 操作者。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}
