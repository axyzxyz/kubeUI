package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/crypto"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// fakeAPIServer 返回一个模拟 K8s API Server 的 httptest 服务,仅实现 /version。
func fakeAPIServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"major":      "1",
				"minor":      "29",
				"gitVersion": version,
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// kubeconfigYAML 生成指向给定 server 的单/多 context kubeconfig 原文。
func kubeconfigYAML(server, current string, extraContexts ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
apiVersion: v1
kind: Config
current-context: %s
contexts:
  - name: ctx-a
    context:
      cluster: c-a
      user: u-a
`, current)
	for _, name := range extraContexts {
		fmt.Fprintf(&b, `  - name: %s
    context:
      cluster: c-%s
      user: u-%s
`, name, name, name)
	}
	fmt.Fprintf(&b, `
clusters:
  - name: c-a
    cluster:
      server: %s
      insecure-skip-tls-verify: true
`, server)
	for _, name := range extraContexts {
		fmt.Fprintf(&b, `  - name: c-%s
    cluster:
      server: %s
      insecure-skip-tls-verify: true
`, name, server)
	}
	b.WriteString(`
users:
  - name: u-a
    user:
      token: test-token
`)
	for _, name := range extraContexts {
		fmt.Fprintf(&b, `  - name: u-%s
    user:
      token: test-token
`, name)
	}
	return b.String()
}

func newTestClusterReg(t *testing.T) (*ClusterRegService, ClusterRegistry) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cipher, err := crypto.NewCipher(cryptoTestKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	reg := NewMemRegistry()
	return NewClusterRegService(store.NewClusterRepo(db), cipher, reg), reg
}

const cryptoTestKey = "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=" // 32 字节 base64

func TestClusterRegRegisterSuccess(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	ctx := context.Background()

	info, err := svc.Register(ctx, RegisterRequest{
		Name:       "dev-1",
		Kubeconfig: kubeconfigYAML(srv.URL, "ctx-a"),
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if info.Status != model.ClusterStatusReady || info.Version != "v1.29.3" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.AccessMode != model.AccessModeDirect {
		t.Fatalf("accessMode = %q, want direct", info.AccessMode)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "dev-1" {
		t.Fatalf("List = %+v err = %v", list, err)
	}

	st, err := svc.Status(ctx, "dev-1")
	if err != nil || st.Status != model.ClusterStatusReady {
		t.Fatalf("Status = %+v err = %v", st, err)
	}
}

func TestClusterRegRegisterFailures(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	dead := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close() // 端口已释放,保证不可达
	ctx := context.Background()

	tests := []struct {
		name     string
		req      RegisterRequest
		wantCode int
	}{
		{
			name:     "missing name",
			req:      RegisterRequest{Kubeconfig: kubeconfigYAML(srv.URL, "ctx-a")},
			wantCode: errcode.ParamInvalid,
		},
		{
			name:     "invalid kubeconfig",
			req:      RegisterRequest{Name: "c1", Kubeconfig: "not: [valid, yaml"},
			wantCode: errcode.ClusterKubeconfigBad,
		},
		{
			name: "multi context without explicit name",
			req: RegisterRequest{
				Name:       "c2",
				Kubeconfig: kubeconfigYAML(srv.URL, "ctx-a", "ctx-b"),
			},
			wantCode: errcode.ClusterKubeconfigMultiContext,
		},
		{
			name: "explicit context not found",
			req: RegisterRequest{
				Name:        "c3",
				Kubeconfig:  kubeconfigYAML(srv.URL, "ctx-a"),
				ContextName: "nope",
			},
			wantCode: errcode.ClusterKubeconfigBad,
		},
		{
			name:     "apiserver unreachable",
			req:      RegisterRequest{Name: "c4", Kubeconfig: kubeconfigYAML(dead.URL, "ctx-a")},
			wantCode: errcode.ClusterUnreachable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Register(ctx, tt.req)
			ec := errcode.From(err)
			if ec == nil || ec.Code != tt.wantCode {
				t.Fatalf("err = %v, want code %d", err, tt.wantCode)
			}
		})
	}
}

func TestClusterRegMultiContextWithExplicitName(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	info, err := svc.Register(context.Background(), RegisterRequest{
		Name:        "multi",
		Kubeconfig:  kubeconfigYAML(srv.URL, "ctx-a", "ctx-b"),
		ContextName: "ctx-b",
	})
	if err != nil {
		t.Fatalf("Register with explicit context: %v", err)
	}
	if info.Name != "multi" || info.Status != model.ClusterStatusReady {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestClusterRegDelete(t *testing.T) {
	svc, reg := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	ctx := context.Background()
	if _, err := svc.Register(ctx, RegisterRequest{Name: "gone", Kubeconfig: kubeconfigYAML(srv.URL, "ctx-a")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := svc.Delete(ctx, "gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, "gone"); errcode.From(err) == nil || errcode.From(err).Code != errcode.ClusterNotFound {
		t.Fatalf("second delete must be 40401, got %v", err)
	}
	if _, err := reg.Get("gone"); err == nil {
		t.Fatal("registry must remove entry on delete")
	}
	if _, err := svc.Status(ctx, "gone"); errcode.From(err) == nil || errcode.From(err).Code != errcode.ClusterNotFound {
		t.Fatalf("status after delete must be 40401, got %v", err)
	}
}

func TestClusterRegKubeconfigEncryptedAtRest(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	ctx := context.Background()
	src := kubeconfigYAML(srv.URL, "ctx-a")
	if _, err := svc.Register(ctx, RegisterRequest{Name: "enc", Kubeconfig: src}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	rec, err := svc.repo.GetClusterByName(ctx, "enc")
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if rec.KubeconfigEncrypted == "" || strings.Contains(rec.KubeconfigEncrypted, "test-token") {
		t.Fatal("kubeconfig must be stored encrypted, never in plaintext")
	}
	if !strings.HasPrefix(rec.KubeconfigEncrypted, "v1:") {
		t.Fatal("ciphertext must carry v1 prefix")
	}
}

func TestClusterRegRotateKubeconfig(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	srv := fakeAPIServer(t, "v1.29.3")
	ctx := context.Background()
	if _, err := svc.Register(ctx, RegisterRequest{Name: "rot", Kubeconfig: kubeconfigYAML(srv.URL, "ctx-a")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	info, err := svc.RotateKubeconfig(ctx, "rot", kubeconfigYAML(srv.URL, "ctx-a"), "")
	if err != nil {
		t.Fatalf("RotateKubeconfig: %v", err)
	}
	if info.Name != "rot" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

// agent 模式注册:跳过 /version 拨测,状态 offline 等待 Agent 反连;轮转保持 agent 模式。
func TestClusterRegRegisterAgentMode(t *testing.T) {
	svc, reg := newTestClusterReg(t)
	// 指向不可达地址,验证 agent 模式不拨测
	unreachable := "https://127.0.0.1:1"
	ctx := context.Background()
	info, err := svc.Register(ctx, RegisterRequest{
		Name:       "agent-only",
		Kubeconfig: kubeconfigYAML(unreachable, "ctx-a"),
		AccessMode: model.AccessModeAgent,
	})
	if err != nil {
		t.Fatalf("Register agent mode: %v", err)
	}
	if info.AccessMode != model.AccessModeAgent {
		t.Fatalf("accessMode = %q, want agent", info.AccessMode)
	}
	if info.Status != model.ClusterStatusOffline {
		t.Fatalf("status = %q, want offline", info.Status)
	}
	rt, err := reg.Get("agent-only")
	if err != nil {
		t.Fatalf("runtime missing: %v", err)
	}
	if rt.AccessMode != model.AccessModeAgent {
		t.Fatalf("runtime accessMode = %q, want agent", rt.AccessMode)
	}
	// 轮转不得把 agent 模式重置回 direct(此时仍不拨测)
	info2, err := svc.RotateKubeconfig(ctx, "agent-only", kubeconfigYAML(unreachable, "ctx-a"), "")
	if err != nil {
		t.Fatalf("RotateKubeconfig: %v", err)
	}
	if info2.AccessMode != model.AccessModeAgent {
		t.Fatalf("rotated accessMode = %q, want agent preserved", info2.AccessMode)
	}
}

// 非法 accessMode 必须被拒绝。
func TestClusterRegRegisterInvalidMode(t *testing.T) {
	svc, _ := newTestClusterReg(t)
	_, err := svc.Register(context.Background(), RegisterRequest{
		Name:       "bad",
		Kubeconfig: kubeconfigYAML("https://127.0.0.1:1", "ctx-a"),
		AccessMode: "telnet",
	})
	if err == nil || !strings.Contains(err.Error(), "accessMode") {
		t.Fatalf("want accessMode param error, got %v", err)
	}
}

func TestMemRegistry(t *testing.T) {
	var (
		reg = NewMemRegistry()
		wg  sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			reg.Put(ClusterRuntime{Name: "a", Status: model.ClusterStatusReady})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, _ = reg.Get("a")
			reg.Names()
		}
	}()
	wg.Wait()
	if len(reg.Names()) != 1 {
		t.Fatalf("names = %v", reg.Names())
	}
	e, err := reg.Get("a")
	if err != nil || e.Name != "a" {
		t.Fatalf("Get = %+v err = %v", e, err)
	}
	if _, err := reg.Get("missing"); err == nil {
		t.Fatal("missing cluster must return error")
	}
	reg.Remove("a")
	if _, err := reg.Get("a"); err == nil {
		t.Fatal("removed cluster must be gone")
	}
}
