package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/api/response"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/crypto"
	"github.com/axyzxyz/kubeui/backend/internal/service"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

const testMasterKey = "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY="

func newTestRouter(t *testing.T) (*gin.Engine, *store.AuditRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cipher, err := crypto.NewCipher(testMasterKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	userRepo := store.NewUserRepo(db)
	hash, err := service.HashPassword("admin123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := userRepo.CreateUser(context.Background(), &store.User{
		Username: "admin", PasswordHash: hash, Role: "admin",
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	authSvc := service.NewAuthService(userRepo, store.NewRefreshTokenRepo(db), "test-secret")
	auditRepo := store.NewAuditRepo(db)
	clusterSvc := service.NewClusterRegService(
		store.NewClusterRepo(db), cipher, service.NewMemRegistry())
	r := Routes(Deps{
		Auth:     authSvc,
		Users:    service.NewUserService(userRepo, authSvc),
		Audit:    service.NewAuditService(auditRepo),
		Clusters: clusterSvc,
	})
	return r, auditRepo
}

func doJSON(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) response.Body {
	t.Helper()
	var b response.Body
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return b
}

// fakeAPIServer 模拟 K8s API Server 的 /version 端点。
func fakeAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"gitVersion": "v1.29.0"})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func kubeconfig(server string) string {
	return fmt.Sprintf(`
apiVersion: v1
kind: Config
current-context: ctx-a
contexts:
  - name: ctx-a
    context:
      cluster: c-a
      user: u-a
clusters:
  - name: c-a
    cluster:
      server: %s
      insecure-skip-tls-verify: true
users:
  - name: u-a
    user:
      token: test-token
`, server)
}

func loginAs(t *testing.T, r http.Handler, username, password string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", w.Code, w.Body.String())
	}
	b := decodeBody(t, w)
	data, err := json.Marshal(b.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var pair service.TokenPair
	if err := json.Unmarshal(data, &pair); err != nil {
		t.Fatalf("unmarshal pair: %v", err)
	}
	return pair.AccessToken
}

func TestFullChainLoginRegisterDeleteAudit(t *testing.T) {
	r, auditRepo := newTestRouter(t)
	srv := fakeAPIServer(t)

	// 1. 登录默认 admin。
	token := loginAs(t, r, "admin", "admin123")

	// 2. 注册集群(POST,应产生审计)。
	w := doJSON(t, r, http.MethodPost, "/api/v1/clusters", token, map[string]string{
		"name":       "dev",
		"kubeconfig": kubeconfig(srv.URL),
	})
	if w.Code != http.StatusOK || decodeBody(t, w).Code != 0 {
		t.Fatalf("register failed: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "test-token") {
		t.Fatal("kubeconfig credentials must never appear in API responses")
	}

	// 3. 列表与状态。
	w = doJSON(t, r, http.MethodGet, "/api/v1/clusters", token, nil)
	if !strings.Contains(w.Body.String(), `"name":"dev"`) {
		t.Fatalf("list should contain cluster: %s", w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, "/api/v1/clusters/dev/status", token, nil)
	if !strings.Contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("status should be ready: %s", w.Body.String())
	}

	// 4. 删除(危险操作)。
	w = doJSON(t, r, http.MethodDelete, "/api/v1/clusters/dev", token, nil)
	if w.Code != http.StatusOK || decodeBody(t, w).Code != 0 {
		t.Fatalf("delete failed: %d %s", w.Code, w.Body.String())
	}

	// 5. 审计落库:注册与删除各一条 allow 记录。
	logs, total, err := auditRepo.ListAuditLogs(context.Background(), store.AuditFilter{}, 0, 100)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 audit records, got %d: %+v", total, logs)
	}
	foundDelete := false
	for _, l := range logs {
		if l.Resource == "/api/v1/clusters/:cluster" && l.Result == "allow" && l.Username == "admin" {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Fatalf("expected audited cluster mutation record, got %+v", logs)
	}
}

func TestAuthRequiredEndpoints(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doJSON(t, r, http.MethodGet, "/api/v1/clusters", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if decodeBody(t, w).Code != 40100 {
		t.Fatalf("code = %s, want 40100", w.Body.String())
	}
}
