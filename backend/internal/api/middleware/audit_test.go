package middleware

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeRecorder 捕获审计条目。
type fakeRecorder struct{ entries []AuditEntry }

func (f *fakeRecorder) Record(c *gin.Context, e AuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}

// TestResourceTypeOf 表驱动验证 path → 友好资源类型映射(BUG-07)。
func TestResourceTypeOf(t *testing.T) {
	cases := []struct {
		path     string
		resource string // :resource 路径参数(集群资源路由)
		want     string
	}{
		{"/api/v1/auth/login", "", "auth"},
		{"/api/v1/auth/refresh", "", "auth"},
		{"/api/v1/auth/logout", "", "auth"},
		{"/api/v1/users", "", "user"},
		{"/api/v1/users/:username/reset-password", "", "user"},
		{"/api/v1/audit-logs", "", "audit-log"},
		{"/api/v1/enroll-tokens/:id", "", "enroll-token"},
		{"/api/v1/kubeconfigs/:id", "", "kubeconfig"},
		{"/api/v1/clusters/:cluster", "", "cluster"},
		{"/api/v1/clusters/:cluster/kubeconfigs", "", "kubeconfig"},
		{"/api/v1/clusters/:cluster/:resource/:name/restart", "pods", "pod"},
		{"/api/v1/clusters/:cluster/:resource/:name/scale", "deployments", "deployment"},
		{"/api/v1/clusters/:cluster/:resource/:name", "secrets", "secret"},
		{"/api/v1/clusters/:cluster/:resource", "pvcs", "persistent-volume-claim"},
		{"/api/v1/clusters/:cluster/:resource", "pvs", "persistent-volume"},
		{"/api/v1/clusters/:cluster/:resource", "crds", "crd"},
		// 未知路径保留原始 path。
		{"/api/v1/unknown/thing", "", "/api/v1/unknown/thing"},
		{"/api/v1/clusters/:cluster/:resource/:name", "widgets", "/api/v1/clusters/:cluster/:resource/:name"},
	}
	for _, tc := range cases {
		if got := ResourceTypeOf(tc.path, tc.resource); got != tc.want {
			t.Errorf("ResourceTypeOf(%q, %q) = %q, want %q", tc.path, tc.resource, got, tc.want)
		}
	}
}

// TestActionOf 表驱动验证 HTTP 方法 + 路径 → 语义化 action(BUG-07)。
func TestActionOf(t *testing.T) {
	cases := []struct {
		method, path string
		want         string
	}{
		{"POST", "/api/v1/auth/login", "login"},
		{"POST", "/api/v1/auth/refresh", "refresh"},
		{"POST", "/api/v1/auth/logout", "logout"},
		{"POST", "/api/v1/clusters/:cluster/pods/:name/restart", "restart"},
		{"PUT", "/api/v1/clusters/:cluster/deployments/:name/scale", "scale"},
		{"DELETE", "/api/v1/clusters/:cluster", "delete"},
		{"DELETE", "/api/v1/users/:username", "delete"},
		{"POST", "/api/v1/users", "write"},
		{"PUT", "/api/v1/clusters/:cluster/kubeconfig", "write"},
	}
	for _, tc := range cases {
		if got := ActionOf(tc.method, tc.path); got != tc.want {
			t.Errorf("ActionOf(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// newAuditRouter 构造挂载 Audit 中间件的测试路由。
func newAuditRouter(rec Recorder, loginStatus int, respBody string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("requestId", "test-req"); c.Next() })
	r.Use(Audit(rec))
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.JSON(loginStatus, gin.H{"code": 0, "data": gin.H{"accessToken": "unused"}})
	})
	r.POST("/api/v1/auth/refresh", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"accessToken": respBody}})
	})
	r.DELETE("/api/v1/clusters/:cluster/:resource/:name", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})
	r.POST("/api/v1/clusters/:cluster/deployments/:name/restart", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})
	return r
}

// TestAuditLoginUsernameAndDeny 验证匿名登录端点审计携带提交的用户名;
// 登录失败(4xx/5xx)记 result=deny(BUG-07)。
func TestAuditLoginUsernameAndDeny(t *testing.T) {
	rec := &fakeRecorder{}
	r := newAuditRouter(rec, http.StatusUnauthorized, "")
	body := `{"username":"alice","password":"bad"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody
	r.ServeHTTP(httptest.NewRecorder(), reqWithBody(req, body))
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.Username != "alice" || e.Action != "login" || e.ResourceType != "auth" || e.Result != "deny" {
		t.Fatalf("entry = %+v, want username=alice action=login resourceType=auth result=deny", e)
	}
	if e.Resource != "/api/v1/auth/login" {
		t.Fatalf("resource = %q, want raw path", e.Resource)
	}
}

// TestAuditRefreshUsernameFromJWT 验证 refresh 成功后从响应 JWT 解析操作者。
func TestAuditRefreshUsernameFromJWT(t *testing.T) {
	rec := &fakeRecorder{}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"username":"carol","userId":7}`))
	token := header + "." + payload + ".sig"
	r := newAuditRouter(rec, http.StatusOK, token)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	r.ServeHTTP(httptest.NewRecorder(), reqWithBody(req, `{"refreshToken":"opaque"}`))
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.Username != "carol" || e.Action != "refresh" || e.ResourceType != "auth" || e.Result != "allow" {
		t.Fatalf("entry = %+v, want username=carol action=refresh resourceType=auth result=allow", e)
	}
}

// TestAuditClusterResourceFields 验证 delete/restart 动作与 cluster/name/namespace 拆分。
func TestAuditClusterResourceFields(t *testing.T) {
	rec := &fakeRecorder{}
	r := newAuditRouter(rec, http.StatusOK, "")
	// DELETE /api/v1/clusters/c1/secrets/app?namespace=ns1
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/c1/secrets/app?namespace=ns1", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	e := rec.entries[len(rec.entries)-1]
	if e.Action != "delete" || e.ResourceType != "secret" || e.Cluster != "c1" || e.Name != "app" || e.Namespace != "ns1" {
		t.Fatalf("entry = %+v, want delete/secret/c1/app/ns1", e)
	}
	// POST .../deployments/web/restart
	req = httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/deployments/web/restart", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	e = rec.entries[len(rec.entries)-1]
	if e.Action != "restart" || e.ResourceType != "deployment" || e.Name != "web" {
		t.Fatalf("entry = %+v, want restart/deployment/web", e)
	}
}

// reqWithBody 构造带 JSON 请求体的请求。
func reqWithBody(req *http.Request, body string) *http.Request {
	req.Body = io.NopCloser(strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
