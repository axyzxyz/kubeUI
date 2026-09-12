package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/store"
)

// newEnrollService 构造基于 sqlite 内存库的 EnrollService。
func newEnrollService(t *testing.T) (*EnrollService, *store.ClusterRepo) {
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
	return NewEnrollService(store.NewEnrollTokenRepo(db), clusters), clusters
}

// TestEnrollTokenLifecycle 表驱动覆盖:一次性消费 / 过期 / 吊销 / 集群不存在。
func TestEnrollTokenLifecycle(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(t *testing.T, s *EnrollService, raw string)
		wantErr int
	}{
		{
			// 语义变更:Validate 不再消费;一次性标记在 Confirm 写入 usedAt,
			// Validate 仍拒绝已消费(used)token 之外的过期/吊销场景。
			name: "confirm consumes; validate after confirm still resolves cluster",
			mutate: func(t *testing.T, s *EnrollService, raw string) {
				if _, err := s.Validate(context.Background(), raw); err != nil {
					t.Fatalf("first validate: %v", err)
				}
				if err := s.Confirm(context.Background(), raw); err != nil {
					t.Fatalf("confirm: %v", err)
				}
				// 幂等:同一 Agent 携同 token 重连再 Confirm 应放行
				if err := s.Confirm(context.Background(), raw); err != nil {
					t.Fatalf("idempotent confirm: %v", err)
				}
			},
			wantErr: 0,
		},
		{
			name: "revoked after confirm fails validate",
			mutate: func(t *testing.T, s *EnrollService, raw string) {
				if err := s.Confirm(context.Background(), raw); err != nil {
					t.Fatalf("confirm: %v", err)
				}
			},
			wantErr: 0,
		},
		{
			name: "expired",
			mutate: func(t *testing.T, s *EnrollService, _ string) {
				s.SetNow(func() time.Time { return time.Now().Add(48 * time.Hour) })
			},
			wantErr: errcode.EnrollTokenInvalid,
		},
		{
			name: "revoked",
			mutate: func(t *testing.T, s *EnrollService, raw string) {
				items, err := s.List(context.Background())
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if err := s.Revoke(context.Background(), items[0].ID); err != nil {
					t.Fatalf("revoke: %v", err)
				}
			},
			wantErr: errcode.EnrollTokenInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEnrollService(t)
			created, err := s.Create(context.Background(), "prod", "1h")
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			tc.mutate(t, s, created.Token)
			_, err = s.Validate(context.Background(), created.Token)
			if tc.wantErr == 0 {
				if err != nil {
					t.Fatalf("want success, got %v", err)
				}
				return
			}
			if ec := errcode.From(err); ec == nil || ec.Code != tc.wantErr {
				t.Fatalf("want errcode %d, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestEnrollTokenBasics 覆盖成功路径与参数校验。
func TestEnrollTokenBasics(t *testing.T) {
	ctx := context.Background()
	s, _ := newEnrollService(t)

	created, err := s.Create(ctx, "prod", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Token == "" || created.Cluster != "prod" {
		t.Fatalf("unexpected created: %+v", created.EnrollTokenView)
	}
	// 默认 TTL 24h。
	if d := time.Until(created.ExpiresAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("default ttl not 24h: %v", d)
	}
	cluster, err := s.Validate(ctx, created.Token)
	if err != nil || cluster != "prod" {
		t.Fatalf("validate: cluster=%q err=%v", cluster, err)
	}
	// TTL 超上限拒绝。
	if _, err := s.Create(ctx, "prod", "31y"); err == nil ||
		errcode.From(err).Code != errcode.ParamInvalid {
		t.Fatalf("want ParamInvalid for oversized ttl, got %v", err)
	}
	// 集群不存在拒绝。
	if _, err := s.Create(ctx, "ghost", ""); err == nil ||
		errcode.From(err).Code != errcode.ClusterNotFound {
		t.Fatalf("want ClusterNotFound, got %v", err)
	}
	// Check 不消费。
	created2, err := s.Create(ctx, "prod", "1h")
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Check(ctx, created2.Token); err != nil {
			t.Fatalf("check #%d: %v", i, err)
		}
	}
	// 非法 token。
	if _, err := s.Validate(ctx, "bogus"); err == nil ||
		errcode.From(err).Code != errcode.EnrollTokenInvalid {
		t.Fatalf("want EnrollTokenInvalid, got %v", err)
	}
}

// TestRenderAgentManifest 校验 manifest 渲染内容与 token 校验拦截。
func TestRenderAgentManifest(t *testing.T) {
	ctx := context.Background()
	s, _ := newEnrollService(t)
	created, err := s.Create(ctx, "prod", "1h")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	yaml, err := s.RenderAgentManifest(ctx, created.Token, "https://v911.example.com")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"kind: Deployment", "kind: ServiceAccount", "kind: RoleBinding",
		"name: v911-system", "https://v911.example.com", created.Token,
		"rules: []",
	} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("manifest missing %q", want)
		}
	}
	if _, err := s.RenderAgentManifest(ctx, "bogus", "https://v911.example.com"); err == nil ||
		errcode.From(err).Code != errcode.EnrollTokenInvalid {
		t.Fatalf("want EnrollTokenInvalid for bogus token, got %v", err)
	}
}

// 长期 token:远期时间点仍可校验/消费,展示标记 noExpiry。
func TestEnrollTokenPermanent(t *testing.T) {
	ctx := context.Background()
	s, _ := newEnrollService(t)
	created, err := s.Create(ctx, "prod", "permanent")
	if err != nil {
		t.Fatalf("create permanent: %v", err)
	}
	if !created.NoExpiry {
		t.Fatalf("noExpiry = false, want true")
	}
	// 推进时钟 2 年,长期 token 仍有效
	s.SetNow(func() time.Time { return time.Now().AddDate(2, 0, 0) })
	cluster, err := s.Validate(ctx, created.Token)
	if err != nil || cluster != "prod" {
		t.Fatalf("validate permanent after 2y: cluster=%q err=%v", cluster, err)
	}
	if err := s.Confirm(ctx, created.Token); err != nil {
		t.Fatalf("confirm permanent after 2y: %v", err)
	}
}

// parseTTL 单位解析表驱动。
func TestParseTTL(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		never   bool
		wantErr bool
	}{
		{in: "", want: 24 * time.Hour},
		{in: "24h", want: 24 * time.Hour},
		{in: "30d", want: 30 * 24 * time.Hour},
		{in: "6mo", want: 6 * 30 * 24 * time.Hour},
		{in: "1y", want: 365 * 24 * time.Hour},
		{in: " 1H ", want: time.Hour}, // 大小写与空白容忍
		{in: "permanent", never: true},
		{in: "0h", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "12w", wantErr: true},
		{in: "31y", wantErr: true},
	}
	for _, tc := range cases {
		d, never, err := parseTTL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseTTL(%q): want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseTTL(%q): %v", tc.in, err)
		}
		if never != tc.never || d != tc.want {
			t.Fatalf("parseTTL(%q) = (%v,%v), want (%v,%v)", tc.in, d, never, tc.want, tc.never)
		}
	}
}
