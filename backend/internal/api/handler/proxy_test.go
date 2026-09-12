package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"

	"github.com/v911/backend/internal/config"
	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/service"
	"github.com/v911/backend/internal/store"
)

// newProxyEnv 构建反向代理测试环境:上游 httptest 服务 + 已注册集群 + 最小路由。
func newProxyEnv(t *testing.T, upstream *httptest.Server) (*gin.Engine, *service.KubeconfigService, *service.AuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
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
	auditSvc := service.NewAuditService(store.NewAuditRepo(db))
	clusterRepo := store.NewClusterRepo(db)
	if err := clusterRepo.UpsertCluster(context.Background(), &store.Cluster{
		Name: "prod", AccessMode: "direct", Status: "ready",
	}); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	kubeSvc := service.NewKubeconfigService(store.NewIssuedKubeconfigRepo(db), clusterRepo, "http://platform.test")

	mgr := k8s.NewManager(k8s.Options{HealthDisabled: true})
	cfg := &rest.Config{Host: upstream.URL, BearerToken: "cluster-secret-token"}
	if err := mgr.Register(context.Background(), "prod", "1.29", cfg); err != nil {
		t.Fatalf("register cluster: %v", err)
	}

	r := gin.New()
	r.Any("/k8s/:cluster/*path", k8sProxy(Deps{
		Auth:        authSvc,
		Audit:       auditSvc,
		Kubeconfigs: kubeSvc,
		Manager:     mgr,
		Config:      &config.Config{},
	}))
	return r, kubeSvc, authSvc
}

// TestProxyForwardsWithClusterCredential 覆盖 JWT 认证 → 路径改写 → 集群凭证注入。
func TestProxyForwardsWithClusterCredential(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList"}`))
	}))
	defer upstream.Close()
	r, _, authSvc := newProxyEnv(t, upstream)

	pair, err := authSvc.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	req := httptest.NewRequest("GET", "/k8s/prod/api/v1/namespaces/default/pods", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	req.Header.Set("X-Client-Noise", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/v1/namespaces/default/pods" {
		t.Fatalf("upstream path %q", gotPath)
	}
	if gotAuth != "Bearer cluster-secret-token" {
		t.Fatalf("upstream auth %q", gotAuth)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["kind"] != "PodList" {
		t.Fatalf("proxied body %s err %v", rec.Body.String(), err)
	}
}

// TestProxyShortTokenClusterBinding 覆盖短期 token 认证与集群绑定校验。
func TestProxyShortTokenClusterBinding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	r, kubeSvc, _ := newProxyEnv(t, upstream)

	created, err := kubeSvc.Issue(context.Background(), 1, "prod", time.Hour, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+created.Token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("/k8s/prod/api/v1/pods"); rec.Code != 200 {
		t.Fatalf("bound cluster status %d", rec.Code)
	}
	// 其他集群拒绝(403)。
	if rec := do("/k8s/other/api/v1/pods"); rec.Code != 403 {
		t.Fatalf("want 403 for other cluster, got %d", rec.Code)
	}
	// 无 token 401。
	req := httptest.NewRequest("GET", "/k8s/prod/api/v1/pods", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 without token, got %d", rec.Code)
	}
}

// TestProxyHijackUpgrade 覆盖升级(101 Switching Protocols)透传:
// 上游 hijack 后写原始字节,客户端经代理以原始 TCP 读取。
func TestProxyHijackUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "echo" {
			http.Error(w, "expected upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, buf, _ := hj.Hijack()
		defer conn.Close() //nolint:errcheck
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: echo\r\nConnection: Upgrade\r\n\r\necho-payload")
		_ = buf.Flush() //nolint:errcheck
	}))
	defer upstream.Close()
	r, _, authSvc := newProxyEnv(t, upstream)

	pair, err := authSvc.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// 代理服务器自身需要真监听:另起 httptest 承载 gin engine。
	proxySrv := httptest.NewServer(r)
	defer proxySrv.Close()

	raw, err := net.Dial("tcp", strings.TrimPrefix(proxySrv.URL, "http://"))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer raw.Close() //nolint:errcheck
	reqLine := fmt.Sprintf("GET /k8s/prod/api/v1/pods?watch=1 HTTP/1.1\r\n"+
		"Host: %s\r\nAuthorization: Bearer %s\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n",
		strings.TrimPrefix(proxySrv.URL, "http://"), pair.AccessToken)
	if _, err := raw.Write([]byte(reqLine)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	reader := bufio.NewReader(raw)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("want 101, got %q err %v", status, err)
	}
	// 读到 echo-payload(跳过剩余响应头)。
	deadline := time.Now().Add(5 * time.Second)
	_ = raw.SetReadDeadline(deadline) //nolint:errcheck
	var payload string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if line == "\r\n" {
			break
		}
	}
	restBytes := make([]byte, len("echo-payload"))
	if _, err := reader.Read(restBytes); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	payload = string(restBytes)
	if payload != "echo-payload" {
		t.Fatalf("payload %q", payload)
	}
}
