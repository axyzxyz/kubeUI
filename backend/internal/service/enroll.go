package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/store"
)

// Enrollment Token 约定(01-architecture §4.1):默认 24h 有效,一次性,
// 可吊销;token 原文只在创建响应中出现一次,落库只存 SHA-256 哈希。
// TTL 支持 h(小时)/d(天)/mo(月=30d)/y(年=365d)与 permanent(长期,
// 永不过期),上限 30y。
const (
	EnrollTokenDefaultTTL = 24 * time.Hour
	EnrollTokenMaxTTL     = 30 * 365 * 24 * time.Hour
	ttlPattern            = `^(\d+)(h|d|mo|y)$`
)

var ttlRE = regexp.MustCompile(ttlPattern)

// parseTTL 解析 TTL 字符串:"" → 默认 24h;"permanent" → 长期(never=true);
// "<n><unit>" → 对应时长。非法或超限返回错误。
func parseTTL(s string) (time.Duration, bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return EnrollTokenDefaultTTL, false, nil
	}
	if s == "permanent" {
		return 0, true, nil
	}
	m := ttlRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false, fmt.Errorf("ttl must be like 24h / 30d / 6mo / 2y or permanent")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false, fmt.Errorf("ttl number must be positive")
	}
	var d time.Duration
	switch m[2] {
	case "h":
		d = time.Duration(n) * time.Hour
	case "d":
		d = time.Duration(n) * 24 * time.Hour
	case "mo":
		d = time.Duration(n) * 30 * 24 * time.Hour
	case "y":
		d = time.Duration(n) * 365 * 24 * time.Hour
	}
	if d > EnrollTokenMaxTTL {
		return 0, false, fmt.Errorf("ttl exceeds maximum 30y")
	}
	return d, false, nil
}

// EnrollService 管理 Agent Enrollment Token 的生命周期与注册校验。
type EnrollService struct {
	tokens   *store.EnrollTokenRepo
	clusters *store.ClusterRepo
	nowFunc  func() time.Time // 可注入,便于测试
}

// NewEnrollService 构造 EnrollService。
func NewEnrollService(tokens *store.EnrollTokenRepo, clusters *store.ClusterRepo) *EnrollService {
	return &EnrollService{tokens: tokens, clusters: clusters, nowFunc: time.Now}
}

// SetNow 注入时钟(仅供测试)。
func (s *EnrollService) SetNow(f func() time.Time) { s.nowFunc = f }

// Create 为指定集群生成一次性 Enrollment Token;ttl 为空默认 24h,
// "permanent" 表示长期;返回的 token 原文仅此一次可见。
func (s *EnrollService) Create(ctx context.Context, cluster, ttl string) (*model.EnrollTokenCreated, error) {
	if _, err := s.clusters.GetClusterByName(ctx, cluster); err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
		}
		return nil, err
	}
	d, never, err := parseTTL(ttl)
	if err != nil {
		return nil, errcode.New(errcode.ParamInvalid, err.Error())
	}
	raw, err := newAgentSecret()
	if err != nil {
		return nil, err
	}
	now := s.nowFunc()
	rec := &store.EnrollToken{
		Cluster:   cluster,
		TokenHash: hashToken(raw),
		NoExpiry:  never,
		ExpiresAt: now.Add(d),
	}
	if never {
		// 落一个哨兵远期时间,校验由 NoExpiry 短路;库中时间仅供展示兜底。
		rec.ExpiresAt = now.AddDate(100, 0, 0)
	}
	if err := s.tokens.CreateEnrollToken(ctx, rec); err != nil {
		return nil, err
	}
	logx.Info(ctx, "enroll token created", "cluster", cluster, "expires_at", rec.ExpiresAt.Format(time.RFC3339))
	return &model.EnrollTokenCreated{
		EnrollTokenView: toEnrollTokenView(rec),
		Token:           raw,
	}, nil
}

// List 返回全部 Enrollment Token(脱敏)。
func (s *EnrollService) List(ctx context.Context) ([]model.EnrollTokenView, error) {
	recs, err := s.tokens.ListEnrollTokens(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.EnrollTokenView, 0, len(recs))
	for i := range recs {
		items = append(items, toEnrollTokenView(&recs[i]))
	}
	return items, nil
}

// Revoke 吊销指定 token(危险操作,handler 侧 admin + 审计)。
func (s *EnrollService) Revoke(ctx context.Context, id int64) error {
	if err := s.tokens.RevokeEnrollToken(ctx, id); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "enroll token not found").WithCause(err)
		}
		return err
	}
	logx.Info(ctx, "enroll token revoked", "token_id", id)
	return nil
}

// Validate 校验 Enrollment Token,返回绑定的集群名;不消费。
// 消费由 Confirm 在注册回执成功后执行:握手失败不烧 token,
// Agent 可原地重试;服务重启后同一 token 重连同样放行(01 §4.2)。
func (s *EnrollService) Validate(ctx context.Context, token string) (string, error) {
	return s.Check(ctx, token)
}

// Confirm 消费一次性 Enrollment Token(注册回执成功后调用);幂等。
// 吊销/过期仍拒绝(长期 token 不受过期约束)。usedAt 标记供列表展示"已使用"状态。
func (s *EnrollService) Confirm(ctx context.Context, token string) error {
	rec, err := s.tokens.GetEnrollTokenByHash(ctx, hashToken(token))
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.EnrollTokenInvalid, "invalid enrollment token")
		}
		return err
	}
	now := s.nowFunc()
	if rec.Revoked || (!rec.NoExpiry && now.After(rec.ExpiresAt)) {
		return errcode.New(errcode.EnrollTokenInvalid, "enrollment token is revoked or expired")
	}
	if err := s.tokens.ConsumeEnrollToken(ctx, rec.ID); err != nil {
		return err
	}
	return nil
}

// Check 校验但不消费(如 manifest 渲染场景);语义与 Validate 一致。
func (s *EnrollService) Check(ctx context.Context, token string) (string, error) {
	rec, err := s.tokens.GetEnrollTokenByHash(ctx, hashToken(token))
	if err != nil {
		if isNotFound(err) {
			return "", errcode.New(errcode.EnrollTokenInvalid, "invalid enrollment token")
		}
		return "", err
	}
	now := s.nowFunc()
	if rec.Revoked || (!rec.NoExpiry && now.After(rec.ExpiresAt)) {
		return "", errcode.New(errcode.EnrollTokenInvalid, "enrollment token is revoked or expired")
	}
	return rec.Cluster, nil
}

// CleanupExpired 清理过期 token(签发/列表时顺带执行)。
func (s *EnrollService) CleanupExpired(ctx context.Context) {
	if _, err := s.tokens.DeleteExpiredEnrollTokens(ctx, s.nowFunc()); err != nil {
		logx.Warn(ctx, "cleanup expired enroll tokens failed", "err", err)
	}
}

func toEnrollTokenView(rec *store.EnrollToken) model.EnrollTokenView {
	return model.EnrollTokenView{
		ID:        rec.ID,
		Cluster:   rec.Cluster,
		ExpiresAt: rec.ExpiresAt,
		NoExpiry:  rec.NoExpiry,
		Revoked:   rec.Revoked,
		CreatedAt: rec.CreatedAt,
	}
}

// newAgentSecret 生成 256bit base64url 随机串。
func newAgentSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
