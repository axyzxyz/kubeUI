package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/store"
)

// newKubeconfigService 构造基于 sqlite 内存库的 KubeconfigService。
func newKubeconfigService(t *testing.T) *KubeconfigService {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	clusters := store.NewClusterRepo(db)
	if err := clusters.UpsertCluster(context.Background(), &store.Cluster{
		Name: "prod", AccessMode: "direct", Status: "ready",
	}); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	return NewKubeconfigService(store.NewIssuedKubeconfigRepo(db), clusters, "https://v911.example.com")
}

// TestKubeconfigIssueDownloadOnce 覆盖签发 → 一次性下载 → 二次下载拒绝。
func TestKubeconfigIssueDownloadOnce(t *testing.T) {
	ctx := context.Background()
	s := newKubeconfigService(t)

	created, err := s.Issue(ctx, 7, "prod", 0, "lens 接入")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if created.Token == "" || created.Cluster != "prod" {
		t.Fatalf("unexpected created: %+v", created.IssuedKubeconfigView)
	}
	if !strings.Contains(created.DownloadURL, "/kubeconfigs/download?code=") {
		t.Fatalf("unexpected download url: %s", created.DownloadURL)
	}

	yaml, err := s.Download(ctx, extractCode(t, created.DownloadURL))
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	for _, want := range []string{
		"server: https://v911.example.com/k8s/prod", "token: " + created.Token,
		"current-context:", "insecure-skip-tls-verify: true",
	} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("kubeconfig missing %q", want)
		}
	}
	// 一次性:同一 code 二次下载必须失败。
	if _, err := s.Download(ctx, extractCode(t, created.DownloadURL)); err == nil ||
		errcode.From(err).Code != errcode.DownloadCodeInvalid {
		t.Fatalf("want DownloadCodeInvalid on second download, got %v", err)
	}
}

// TestKubeconfigRevokeAndVerify 覆盖撤销后 VerifyShortToken 失败。
func TestKubeconfigRevokeAndVerify(t *testing.T) {
	ctx := context.Background()
	s := newKubeconfigService(t)

	created, err := s.Issue(ctx, 7, "prod", time.Hour, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rec, err := s.VerifyShortToken(ctx, created.Token)
	if err != nil || rec.Cluster != "prod" {
		t.Fatalf("verify: rec=%+v err=%v", rec, err)
	}
	// 非属主非 admin 不可撤销。
	if err := s.Revoke(ctx, rec.ID, 42, false); err == nil ||
		errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("want Forbidden, got %v", err)
	}
	if err := s.Revoke(ctx, rec.ID, 7, false); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.VerifyShortToken(ctx, created.Token); err == nil ||
		errcode.From(err).Code != errcode.IssuedTokenInvalid {
		t.Fatalf("want IssuedTokenInvalid after revoke, got %v", err)
	}
	// 集群绑定校验由代理层比对;此处覆盖过期:拨快时钟后校验失败。
	rec2, err := s.Issue(ctx, 7, "prod", time.Hour, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	s.SetNow(func() time.Time { return time.Now().Add(2 * time.Hour) })
	if _, err := s.VerifyShortToken(ctx, rec2.Token); err == nil ||
		errcode.From(err).Code != errcode.IssuedTokenInvalid {
		t.Fatalf("want IssuedTokenInvalid for expired, got %v", err)
	}
	// 过期后签发会触发清理,不报错即可(覆盖 cleanupExpired 路径)。
	s.SetNow(time.Now)
	if _, err := s.Issue(ctx, 7, "prod", 0, ""); err != nil {
		t.Fatalf("issue after cleanup: %v", err)
	}
}

// TestKubeconfigTTLLimits 校验 TTL 上下限与未知集群。
func TestKubeconfigTTLLimits(t *testing.T) {
	ctx := context.Background()
	s := newKubeconfigService(t)
	cases := []struct {
		name    string
		cluster string
		ttl     time.Duration
		wantErr int
	}{
		{"oversized ttl", "prod", 8 * 24 * time.Hour, errcode.ParamInvalid},
		{"unknown cluster", "ghost", 0, errcode.ClusterNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Issue(ctx, 1, tc.cluster, tc.ttl, ""); err == nil ||
				errcode.From(err).Code != tc.wantErr {
				t.Fatalf("want %d, got %v", tc.wantErr, err)
			}
		})
	}
	// 默认 TTL 24h。
	created, err := s.Issue(ctx, 1, "prod", 0, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if d := time.Until(created.ExpiresAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("default ttl not 24h: %v", d)
	}
}

// extractCode 从下载 URL 中解析 code 参数。
func extractCode(t *testing.T, url string) string {
	t.Helper()
	const marker = "code="
	i := strings.Index(url, marker)
	if i < 0 {
		t.Fatalf("no code in url %s", url)
	}
	return url[i+len(marker):]
}
