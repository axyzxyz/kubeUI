package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrNotFound 当目标记录不存在时返回。
var ErrNotFound = errors.New("record not found")

// UserRepo 用户表访问。
type UserRepo struct{ db *gorm.DB }

// NewUserRepo 构造 UserRepo。
func NewUserRepo(db *gorm.DB) *UserRepo { return &UserRepo{db: db} }

// CreateUser 创建用户。
func (r *UserRepo) CreateUser(ctx context.Context, u *User) error {
	if err := r.db.WithContext(ctx).Create(u).Error; err != nil {
		return fmt.Errorf("create user %s: %w", u.Username, err)
	}
	return nil
}

// GetUserByUsername 按用户名查询。
func (r *UserRepo) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, wrapNotFound(err, "user "+username)
	}
	return &u, nil
}

// GetUserByID 按 ID 查询。
func (r *UserRepo) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("user %d", id))
	}
	return &u, nil
}

// ListUsers 分页列出用户。
func (r *UserRepo) ListUsers(ctx context.Context, offset, limit int) ([]User, int64, error) {
	var (
		users []User
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&User{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	if err := r.db.WithContext(ctx).Order("id").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	return users, total, nil
}

// UpdateUser 保存用户变更(禁用/改密)。
func (r *UserRepo) UpdateUser(ctx context.Context, u *User) error {
	if err := r.db.WithContext(ctx).Save(u).Error; err != nil {
		return fmt.Errorf("update user %s: %w", u.Username, err)
	}
	return nil
}

// DeleteUser 删除用户记录(调用方负责先吊销其会话)。
func (r *UserRepo) DeleteUser(ctx context.Context, username string) error {
	res := r.db.WithContext(ctx).Where("username = ?", username).Delete(&User{})
	if res.Error != nil {
		return fmt.Errorf("delete user %s: %w", username, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delete user %s: %w", username, ErrNotFound)
	}
	return nil
}

// RefreshTokenRepo refresh_tokens 表访问。
type RefreshTokenRepo struct{ db *gorm.DB }

// NewRefreshTokenRepo 构造 RefreshTokenRepo。
func NewRefreshTokenRepo(db *gorm.DB) *RefreshTokenRepo { return &RefreshTokenRepo{db: db} }

// CreateRefreshToken 写入一条 refresh token 哈希。
func (r *RefreshTokenRepo) CreateRefreshToken(ctx context.Context, t *RefreshToken) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// GetRefreshTokenByHash 按哈希查询。
func (r *RefreshTokenRepo) GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var t RefreshToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&t).Error; err != nil {
		return nil, wrapNotFound(err, "refresh token")
	}
	return &t, nil
}

// RevokeRefreshToken 吊销指定哈希的 token。
func (r *RefreshTokenRepo) RevokeRefreshToken(ctx context.Context, hash string) error {
	if err := r.db.WithContext(ctx).Model(&RefreshToken{}).
		Where("token_hash = ?", hash).Update("revoked", true).Error; err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

// RevokeAllForUser 吊销某用户全部 refresh token。
func (r *RefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID int64) error {
	if err := r.db.WithContext(ctx).Model(&RefreshToken{}).
		Where("user_id = ?", userID).Update("revoked", true).Error; err != nil {
		return fmt.Errorf("revoke refresh tokens of user %d: %w", userID, err)
	}
	return nil
}

// ClusterRepo clusters 表访问。
type ClusterRepo struct{ db *gorm.DB }

// NewClusterRepo 构造 ClusterRepo。
func NewClusterRepo(db *gorm.DB) *ClusterRepo { return &ClusterRepo{db: db} }

// UpsertCluster 以名称为主键写入集群元数据。
func (r *ClusterRepo) UpsertCluster(ctx context.Context, c *Cluster) error {
	if err := r.db.WithContext(ctx).
		Where(Cluster{Name: c.Name}).
		Assign(Cluster{
			Description:         c.Description,
			KubeconfigEncrypted: c.KubeconfigEncrypted,
			AccessMode:          c.AccessMode,
			Status:              c.Status,
			Version:             c.Version,
			Message:             c.Message,
			LastTransitionTime:  c.LastTransitionTime,
		}).
		FirstOrCreate(c).Error; err != nil {
		return fmt.Errorf("upsert cluster %s: %w", c.Name, err)
	}
	// FirstOrCreate 命中已有记录时不会应用 Assign,需再保存一次。
	if err := r.db.WithContext(ctx).Save(c).Error; err != nil {
		return fmt.Errorf("save cluster %s: %w", c.Name, err)
	}
	return nil
}

// GetClusterByName 按名称查询。
func (r *ClusterRepo) GetClusterByName(ctx context.Context, name string) (*Cluster, error) {
	var c Cluster
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&c).Error; err != nil {
		return nil, wrapNotFound(err, "cluster "+name)
	}
	return &c, nil
}

// ListClusters 列出全部集群。
func (r *ClusterRepo) ListClusters(ctx context.Context) ([]Cluster, error) {
	var clusters []Cluster
	if err := r.db.WithContext(ctx).Order("name").Find(&clusters).Error; err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	return clusters, nil
}

// DeleteCluster 按名称删除。
func (r *ClusterRepo) DeleteCluster(ctx context.Context, name string) error {
	if err := r.db.WithContext(ctx).Where("name = ?", name).Delete(&Cluster{}).Error; err != nil {
		return fmt.Errorf("delete cluster %s: %w", name, err)
	}
	return nil
}

// UpdateClusterStatus 更新状态缓存字段。
func (r *ClusterRepo) UpdateClusterStatus(ctx context.Context, name, status, version, message string) error {
	if err := r.db.WithContext(ctx).Model(&Cluster{}).
		Where("name = ?", name).
		Updates(map[string]any{
			"status":               status,
			"version":              version,
			"message":              message,
			"last_transition_time": time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("update cluster status %s: %w", name, err)
	}
	return nil
}

// EnrollTokenRepo enroll_tokens 表访问(Agent 反连接入)。
type EnrollTokenRepo struct{ db *gorm.DB }

// NewEnrollTokenRepo 构造 EnrollTokenRepo。
func NewEnrollTokenRepo(db *gorm.DB) *EnrollTokenRepo { return &EnrollTokenRepo{db: db} }

// CreateEnrollToken 写入一条 enrollment token 哈希。
func (r *EnrollTokenRepo) CreateEnrollToken(ctx context.Context, t *EnrollToken) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("create enroll token: %w", err)
	}
	return nil
}

// GetEnrollTokenByHash 按哈希查询。
func (r *EnrollTokenRepo) GetEnrollTokenByHash(ctx context.Context, hash string) (*EnrollToken, error) {
	var t EnrollToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&t).Error; err != nil {
		return nil, wrapNotFound(err, "enroll token")
	}
	return &t, nil
}

// ListEnrollTokens 列出全部 enrollment token(按创建时间倒序)。
func (r *EnrollTokenRepo) ListEnrollTokens(ctx context.Context) ([]EnrollToken, error) {
	var tokens []EnrollToken
	if err := r.db.WithContext(ctx).Order("id desc").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("list enroll tokens: %w", err)
	}
	return tokens, nil
}

// RevokeEnrollToken 按 ID 吊销。
func (r *EnrollTokenRepo) RevokeEnrollToken(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&EnrollToken{}).Where("id = ?", id).Update("revoked", true)
	if res.Error != nil {
		return fmt.Errorf("revoke enroll token %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("enroll token %d: %w", id, ErrNotFound)
	}
	return nil
}

// ConsumeEnrollToken 将 token 标记为已使用(一次性语义:注册成功后写入 usedAt)。
// 幂等:重复调用不报错(同一 Agent 携同 token 重连属于合法场景)。
func (r *EnrollTokenRepo) ConsumeEnrollToken(ctx context.Context, id int64) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&EnrollToken{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", now)
	if res.Error != nil {
		return fmt.Errorf("consume enroll token %d: %w", id, res.Error)
	}
	return nil
}

// DeleteExpiredEnrollTokens 清理过期记录,返回删除条数。
func (r *EnrollTokenRepo) DeleteExpiredEnrollTokens(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("expires_at < ?", before).Delete(&EnrollToken{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete expired enroll tokens: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// IssuedKubeconfigRepo issued_kubeconfigs 表访问(客户端 kubeconfig 签发)。
type IssuedKubeconfigRepo struct{ db *gorm.DB }

// NewIssuedKubeconfigRepo 构造 IssuedKubeconfigRepo。
func NewIssuedKubeconfigRepo(db *gorm.DB) *IssuedKubeconfigRepo {
	return &IssuedKubeconfigRepo{db: db}
}

// CreateIssuedKubeconfig 写入一条签发记录(仅哈希)。
func (r *IssuedKubeconfigRepo) CreateIssuedKubeconfig(ctx context.Context, k *IssuedKubeconfig) error {
	if err := r.db.WithContext(ctx).Create(k).Error; err != nil {
		return fmt.Errorf("create issued kubeconfig: %w", err)
	}
	return nil
}

// GetIssuedKubeconfigByHash 按哈希查询。
func (r *IssuedKubeconfigRepo) GetIssuedKubeconfigByHash(ctx context.Context, hash string) (*IssuedKubeconfig, error) {
	var k IssuedKubeconfig
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&k).Error; err != nil {
		return nil, wrapNotFound(err, "issued kubeconfig")
	}
	return &k, nil
}

// GetIssuedKubeconfigByID 按 ID 查询。
func (r *IssuedKubeconfigRepo) GetIssuedKubeconfigByID(ctx context.Context, id int64) (*IssuedKubeconfig, error) {
	var k IssuedKubeconfig
	if err := r.db.WithContext(ctx).First(&k, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("issued kubeconfig %d", id))
	}
	return &k, nil
}

// ListIssuedKubeconfigsForUser 列出某用户签发的凭证。
func (r *IssuedKubeconfigRepo) ListIssuedKubeconfigsForUser(ctx context.Context, userID int64) ([]IssuedKubeconfig, error) {
	var items []IssuedKubeconfig
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id desc").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list issued kubeconfigs of user %d: %w", userID, err)
	}
	return items, nil
}

// RevokeIssuedKubeconfig 按 ID 吊销。
func (r *IssuedKubeconfigRepo) RevokeIssuedKubeconfig(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&IssuedKubeconfig{}).Where("id = ?", id).Update("revoked", true)
	if res.Error != nil {
		return fmt.Errorf("revoke issued kubeconfig %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("issued kubeconfig %d: %w", id, ErrNotFound)
	}
	return nil
}

// DeleteExpiredIssuedKubeconfigs 清理过期记录,返回删除条数。
func (r *IssuedKubeconfigRepo) DeleteExpiredIssuedKubeconfigs(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("expires_at < ?", before).Delete(&IssuedKubeconfig{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete expired issued kubeconfigs: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// UpdateClusterAccessMode 更新集群接入模式(direct|agent)。
func (r *ClusterRepo) UpdateClusterAccessMode(ctx context.Context, name, mode string) error {
	if err := r.db.WithContext(ctx).Model(&Cluster{}).
		Where("name = ?", name).Update("access_mode", mode).Error; err != nil {
		return fmt.Errorf("update cluster access mode %s: %w", name, err)
	}
	return nil
}

// AuditRepo audit_logs 表访问(只追加)。
type AuditRepo struct{ db *gorm.DB }

// NewAuditRepo 构造 AuditRepo。
func NewAuditRepo(db *gorm.DB) *AuditRepo { return &AuditRepo{db: db} }

// CreateAuditLog 追加一条审计记录;本表不提供更新与删除。
func (r *AuditRepo) CreateAuditLog(ctx context.Context, a *AuditLog) error {
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

// ListAuditLogs 按操作者/集群/动作过滤并分页。
func (r *AuditRepo) ListAuditLogs(ctx context.Context, f AuditFilter, offset, limit int) ([]AuditLog, int64, error) {
	q := r.db.WithContext(ctx).Model(&AuditLog{})
	if f.Username != "" {
		q = q.Where("username = ?", f.Username)
	}
	if f.Cluster != "" {
		q = q.Where("cluster = ?", f.Cluster)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}
	var logs []AuditLog
	if err := q.Order("id desc").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, fmt.Errorf("list audit logs: %w", err)
	}
	return logs, total, nil
}

// AuditFilter 审计查询过滤条件。
type AuditFilter struct {
	Username string
	Cluster  string
	Action   string
}

// SeedDefaults 写入种子数据(内置角色定义由调用方传入,权限点在 service 层维护),幂等。
func SeedDefaults(ctx context.Context, db *gorm.DB, roles []Role) error {
	for i := range roles {
		var existing Role
		err := db.WithContext(ctx).Where(Role{Name: roles[i].Name}).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := db.WithContext(ctx).Create(&roles[i]).Error; err != nil {
				return fmt.Errorf("seed role %s: %w", roles[i].Name, err)
			}
		case err != nil:
			return fmt.Errorf("seed role %s: %w", roles[i].Name, err)
		default:
			// 旧 schema 遗留的空权限行回填(FirstOrCreate 命中已有行不会
			// 覆盖 dest,必须显式查询判断),补齐为当前种子定义。
			if existing.Builtin && (existing.Permissions == "" || existing.Permissions == "[]" || existing.Permissions == "null") && roles[i].Permissions != "" {
				if err := db.WithContext(ctx).Model(&Role{}).Where("id = ?", existing.ID).
					Updates(map[string]any{"permissions": roles[i].Permissions}).Error; err != nil {
					return fmt.Errorf("backfill role %s: %w", roles[i].Name, err)
				}
			}
		}
	}
	return nil
}

func wrapNotFound(err error, what string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return fmt.Errorf("query %s: %w", what, err)
}
