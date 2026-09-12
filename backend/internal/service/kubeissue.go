package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/store"
)

// 客户端 kubeconfig 签发约定(01-architecture §5):
//   - 短期 Bearer token:TTL 默认 24h、上限 7d,落库仅存 SHA-256 哈希,可撤销;
//   - 一次性下载 code:10 分钟有效、换取一次即失效;下载链接整体 24h 过期;
//   - token 原文只在签发响应与 kubeconfig 文件中出现,不落库、不落日志。
const (
	IssuedDefaultTTL = 24 * time.Hour
	IssuedMaxTTL     = 7 * 24 * time.Hour
	IssuedMinTTL     = time.Minute
	DownloadCodeTTL  = 10 * time.Minute
	DownloadLinkTTL  = 24 * time.Hour
)

// downloadEntry 是一次性下载 code 的内存登记项。
//
// token 明文仅缓存在内存中(进程重启即失效,等效于撤销),换取一次即删除;
// 持久层只保留哈希。
type downloadEntry struct {
	cluster    string
	tokenPlain string
	expiresAt  time.Time // code 自身有效期(10 分钟)
	linkExpiry time.Time // 下载链接整体有效期(24 小时)
}

// KubeconfigService 签发与管理面向客户端(kubectl/Lens 等)的短期凭证。
type KubeconfigService struct {
	issued      *store.IssuedKubeconfigRepo
	clusters    *store.ClusterRepo
	externalURL string // 平台对外地址,kubeconfig server 指向 {externalURL}/k8s/{cluster}

	mu      sync.Mutex
	codes   map[string]*downloadEntry
	nowFunc func() time.Time // 可注入,便于测试
}

// NewKubeconfigService 构造 KubeconfigService;externalURL 形如 https://v911.example.com。
func NewKubeconfigService(issued *store.IssuedKubeconfigRepo, clusters *store.ClusterRepo, externalURL string) *KubeconfigService {
	return &KubeconfigService{
		issued:      issued,
		clusters:    clusters,
		externalURL: externalURL,
		codes:       make(map[string]*downloadEntry),
		nowFunc:     time.Now,
	}
}

// SetNow 注入时钟(仅供测试)。
func (s *KubeconfigService) SetNow(f func() time.Time) { s.nowFunc = f }

// Issue 为用户签发指定集群的短期 Bearer token 并登记一次性下载 code。
// token 原文与下载链接只在本响应中出现一次。
func (s *KubeconfigService) Issue(ctx context.Context, userID int64, cluster string, ttl time.Duration, description string) (*model.IssuedKubeconfigCreated, error) {
	if _, err := s.clusters.GetClusterByName(ctx, cluster); err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
		}
		return nil, err
	}
	switch {
	case ttl <= 0:
		ttl = IssuedDefaultTTL
	case ttl > IssuedMaxTTL:
		return nil, errcode.New(errcode.ParamInvalid, "ttl exceeds maximum 7d")
	case ttl < IssuedMinTTL:
		return nil, errcode.New(errcode.ParamInvalid, "ttl too short")
	}
	s.cleanupExpired(ctx)

	raw, err := newAgentSecret()
	if err != nil {
		return nil, err
	}
	now := s.nowFunc()
	rec := &store.IssuedKubeconfig{
		UserID:      userID,
		Cluster:     cluster,
		TokenHash:   hashToken(raw),
		Description: description,
		ExpiresAt:   now.Add(ttl),
	}
	if err := s.issued.CreateIssuedKubeconfig(ctx, rec); err != nil {
		return nil, err
	}
	code, err := newAgentSecret()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.codes[code] = &downloadEntry{
		cluster:    cluster,
		tokenPlain: raw,
		expiresAt:  now.Add(DownloadCodeTTL),
		linkExpiry: now.Add(DownloadLinkTTL),
	}
	s.mu.Unlock()

	logx.Info(ctx, "kubeconfig issued", "cluster", cluster, "user_id", userID,
		"expires_at", rec.ExpiresAt.Format(time.RFC3339))
	return &model.IssuedKubeconfigCreated{
		IssuedKubeconfigView: toIssuedView(rec),
		Token:                raw,
		DownloadURL:          fmt.Sprintf("%s/api/v1/clusters/%s/kubeconfigs/download?code=%s", s.externalURL, cluster, code),
		LinkExpiresAt:        now.Add(DownloadLinkTTL),
	}, nil
}

// Download 消费一次性 code 并渲染 kubeconfig YAML(server 指向平台代理
// /k8s/{cluster});code 10 分钟有效且只能使用一次,链接整体 24h 过期。
func (s *KubeconfigService) Download(ctx context.Context, code string) (string, error) {
	if code == "" {
		return "", errcode.New(errcode.ParamInvalid, "code is required")
	}
	now := s.nowFunc()
	s.mu.Lock()
	entry, ok := s.codes[code]
	if ok {
		delete(s.codes, code) // 一次性:读取即消费
	}
	s.mu.Unlock()
	if !ok || now.After(entry.expiresAt) || now.After(entry.linkExpiry) {
		return "", errcode.New(errcode.DownloadCodeInvalid, "download code is invalid, used or expired")
	}
	// 二次校验:code 有效但凭证已被撤销/过期时拒绝。
	rec, err := s.issued.GetIssuedKubeconfigByHash(ctx, hashToken(entry.tokenPlain))
	if err != nil {
		if isNotFound(err) {
			return "", errcode.New(errcode.DownloadCodeInvalid, "download code is invalid, used or expired")
		}
		return "", err
	}
	if rec.Revoked || now.After(rec.ExpiresAt) {
		return "", errcode.New(errcode.IssuedTokenInvalid, "issued kubeconfig is revoked or expired")
	}
	yaml, err := BuildKubeconfigYAML(entry.cluster, entry.tokenPlain, s.externalURL)
	if err != nil {
		return "", err
	}
	logx.Info(ctx, "kubeconfig downloaded", "cluster", entry.cluster)
	return yaml, nil
}

// ListForUser 返回某用户签发的凭证(脱敏)。
func (s *KubeconfigService) ListForUser(ctx context.Context, userID int64) ([]model.IssuedKubeconfigView, error) {
	recs, err := s.issued.ListIssuedKubeconfigsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]model.IssuedKubeconfigView, 0, len(recs))
	for i := range recs {
		items = append(items, toIssuedView(&recs[i]))
	}
	return items, nil
}

// Revoke 撤销凭证:仅属主本人或 admin;危险操作,handler 侧挂审计。
func (s *KubeconfigService) Revoke(ctx context.Context, id, requesterID int64, admin bool) error {
	rec, err := s.issued.GetIssuedKubeconfigByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "issued kubeconfig not found").WithCause(err)
		}
		return err
	}
	if !admin && rec.UserID != requesterID {
		return errcode.New(errcode.Forbidden, "not the owner of this credential")
	}
	if err := s.issued.RevokeIssuedKubeconfig(ctx, id); err != nil {
		return err
	}
	logx.Info(ctx, "kubeconfig revoked", "credential_id", id, "cluster", rec.Cluster)
	return nil
}

// VerifyShortToken 校验代理访问用的短期 Bearer token,返回签发记录;
// 无效/过期/已撤销返回 40102。
func (s *KubeconfigService) VerifyShortToken(ctx context.Context, token string) (*store.IssuedKubeconfig, error) {
	rec, err := s.issued.GetIssuedKubeconfigByHash(ctx, hashToken(token))
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.IssuedTokenInvalid, "invalid credential token")
		}
		return nil, err
	}
	if rec.Revoked || s.nowFunc().After(rec.ExpiresAt) {
		return nil, errcode.New(errcode.IssuedTokenInvalid, "credential token is revoked or expired")
	}
	return rec, nil
}

// cleanupExpired 清理过期落库记录与超期 code。
func (s *KubeconfigService) cleanupExpired(ctx context.Context) {
	if _, err := s.issued.DeleteExpiredIssuedKubeconfigs(ctx, s.nowFunc()); err != nil {
		logx.Warn(ctx, "cleanup expired issued kubeconfigs failed", "err", err)
	}
	now := s.nowFunc()
	s.mu.Lock()
	for code, e := range s.codes {
		if now.After(e.linkExpiry) {
			delete(s.codes, code)
		}
	}
	s.mu.Unlock()
}

// BuildKubeconfigYAML 渲染指向平台代理的 kubeconfig:
// server = {externalURL}/k8s/{cluster},user.token = 短期 Bearer token。
// token 原文仅进入返回的 YAML,禁止落日志。
func BuildKubeconfigYAML(cluster, token, externalURL string) (string, error) {
	cfg := clientcmdapi.NewConfig()
	clusterName := "v911-" + cluster
	cfg.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server: fmt.Sprintf("%s/k8s/%s", externalURL, cluster),
		// 平台对外证书不可预知,客户端 kubeconfig 默认跳过校验;
		// 生产环境建议替换为平台 CA 并关闭该开关。
		InsecureSkipTLSVerify: true,
	}
	userName := "v911-" + cluster
	cfg.AuthInfos[userName] = &clientcmdapi.AuthInfo{Token: token}
	ctxName := clusterName
	cfg.Contexts[ctxName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: userName,
	}
	cfg.CurrentContext = ctxName
	raw, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", fmt.Errorf("write kubeconfig: %w", err)
	}
	return string(raw), nil
}

func toIssuedView(rec *store.IssuedKubeconfig) model.IssuedKubeconfigView {
	return model.IssuedKubeconfigView{
		ID:          rec.ID,
		UserID:      rec.UserID,
		Cluster:     rec.Cluster,
		Description: rec.Description,
		ExpiresAt:   rec.ExpiresAt,
		Revoked:     rec.Revoked,
		CreatedAt:   rec.CreatedAt,
	}
}
