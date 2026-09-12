package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/pagination"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// UserService 提供用户管理能力(admin 专属操作由 handler 层的 AdminOnly 中间件把守)。
type UserService struct {
	users *store.UserRepo
	auth  *AuthService
}

// NewUserService 构造 UserService。
func NewUserService(users *store.UserRepo, auth *AuthService) *UserService {
	return &UserService{users: users, auth: auth}
}

// CreateUser 创建用户;用户名重复返回 40412。
func (s *UserService) CreateUser(ctx context.Context, username, password, role string) (*model.User, error) {
	if role != model.RoleAdmin && role != model.RoleOperator && role != model.RoleViewer {
		return nil, errcode.New(errcode.ParamInvalid, "role must be admin, operator or viewer")
	}
	if len(password) < 8 {
		return nil, errcode.New(errcode.ParamInvalid, "password must be at least 8 characters")
	}
	if _, err := s.users.GetUserByUsername(ctx, username); err == nil {
		return nil, errcode.New(errcode.UserAlreadyExists, "username already exists")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &store.User{Username: username, PasswordHash: hash, Role: role}
	if err := s.users.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return toUserDTO(u), nil
}

// DisableUser 禁用用户并吊销其全部 refresh token。
func (s *UserService) DisableUser(ctx context.Context, username string, disabled bool) error {
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return err
	}
	u.Disabled = disabled
	if err := s.users.UpdateUser(ctx, u); err != nil {
		return err
	}
	if disabled {
		if err := s.auth.RevokeAllForUser(ctx, u.ID); err != nil {
			return fmt.Errorf("revoke tokens for %s: %w", username, err)
		}
	}
	return nil
}

// ChangePassword 校验旧密码后修改密码,并吊销全部已有会话。
func (s *UserService) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return errcode.New(errcode.ParamInvalid, "password must be at least 8 characters")
	}
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return err
	}
	if err := comparePassword(u.PasswordHash, oldPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	if err := s.users.UpdateUser(ctx, u); err != nil {
		return err
	}
	return s.auth.RevokeAllForUser(ctx, u.ID)
}

// ListUsers 分页列出用户。
func (s *UserService) ListUsers(ctx context.Context, p pagination.Pagination) (pagination.ResultBody[model.User], error) {
	users, total, err := s.users.ListUsers(ctx, p.Offset(), p.Size)
	if err != nil {
		return pagination.ResultBody[model.User]{}, err
	}
	items := make([]model.User, 0, len(users))
	for i := range users {
		items = append(items, *toUserDTO(&users[i]))
	}
	return pagination.Result(items, total, p), nil
}

func toUserDTO(u *store.User) *model.User {
	return &model.User{
		ID:        u.ID,
		Username:  u.Username,
		Role:      u.Role,
		Disabled:  u.Disabled,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// 权限点命名约定(<domain>:<action>),前端按列表做 UI 裁剪。
const (
	PermAll              = "*" // admin 专属:全部权限
	PermClustersRead     = "clusters:read"
	PermClustersWrite    = "clusters:write"
	PermUsersManage      = "users:manage"
	PermAuditRead        = "audit:read"
	PermEnrollManage     = "enroll-tokens:manage"
	PermResourcesRead    = "resources:read"
	PermResourcesWrite   = "resources:write"
	PermSecretsRead      = "secrets:read"        // Secret 明文(operator+)
	PermSecretsReadMask  = "secrets:read-masked" // Secret 仅脱敏视图(viewer)
	PermLogsRead         = "logs:read"
	PermTerminalUse      = "terminal:use"
	PermEventsRead       = "events:read"
	PermMetricsRead      = "metrics:read"
	PermCredentialsIssue = "credentials:issue"
	PermK8sProxyUse      = "k8s-proxy:use"
)

// operatorPermissions 是 operator 角色的权限集(读写 + 终端 + Secret 明文)。
var operatorPermissions = []string{
	PermClustersRead, PermResourcesRead, PermResourcesWrite, PermSecretsRead,
	PermLogsRead, PermTerminalUse, PermEventsRead, PermMetricsRead,
	PermCredentialsIssue, PermK8sProxyUse,
}

// viewerPermissions 是 viewer 角色的权限集(只读 + Secret 脱敏)。
var viewerPermissions = []string{
	PermClustersRead, PermResourcesRead, PermSecretsReadMask,
	PermLogsRead, PermEventsRead, PermMetricsRead,
	PermCredentialsIssue, PermK8sProxyUse,
}

// AllPermissionPoints 返回全部权限点枚举(不含通配符),供自定义角色校验与前端展示。
func AllPermissionPoints() []string {
	points := make([]string, 0, len(operatorPermissions)+len(viewerPermissions)+2)
	points = append(points, operatorPermissions...)
	points = append(points, viewerPermissions...)
	points = append(points, PermUsersManage, PermAuditRead, PermEnrollManage)
	return dedupStrings(points)
}

// validPermissionPoint 判断权限点是否合法(允许通配符 "*")。
func validPermissionPoint(p string) bool {
	if p == PermAll {
		return true
	}
	for _, known := range AllPermissionPoints() {
		if p == known {
			return true
		}
	}
	return false
}

// BuiltinRoleSeeds 返回三条内置角色的种子行(存在即跳过,由 store.SeedDefaults 执行)。
// 权限点取 operatorPermissions/viewerPermissions,避免 store 层重复维护。
func BuiltinRoleSeeds() []store.Role {
	return []store.Role{
		{Name: model.RoleAdmin, Description: "平台全部权限,含用户与集群管理", Builtin: true, Permissions: `["` + PermAll + `"]`},
		{Name: model.RoleOperator, Description: "指定集群内读写", Builtin: true, Permissions: encodePermissions(operatorPermissions)},
		{Name: model.RoleViewer, Description: "指定集群内只读", Builtin: true, Permissions: encodePermissions(viewerPermissions)},
	}
}

// dedupStrings 去重并保持原顺序。
func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// PermissionsFor 返回角色的权限列表;未知角色返回空集。
func PermissionsFor(role string) []string {
	switch role {
	case model.RoleAdmin:
		return []string{PermAll}
	case model.RoleOperator:
		return append([]string(nil), operatorPermissions...)
	case model.RoleViewer:
		return append([]string(nil), viewerPermissions...)
	default:
		return []string{}
	}
}

// UsernameByID 返回用户 ID 对应的用户名(不存在返回空串);供代理审计归因。
func (s *UserService) UsernameByID(ctx context.Context, id int64) string {
	u, err := s.users.GetUserByID(ctx, id)
	if err != nil {
		return ""
	}
	return u.Username
}

// GetByUsername 返回单个用户(供 /users/me)。
func (s *UserService) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return nil, err
	}
	return toUserDTO(u), nil
}

// Permissions 返回用户角色与其权限列表。
func (s *UserService) Permissions(ctx context.Context, username string) (*model.MePermissions, error) {
	u, err := s.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	return &model.MePermissions{Role: u.Role, Permissions: PermissionsFor(u.Role)}, nil
}

// generatePassword 生成 16 字节随机密码(经 base64,约 22 字符)。
func generatePassword() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ResetPassword 由 admin 重置任意用户密码为一次性随机密码,并吊销其全部会话。
func (s *UserService) ResetPassword(ctx context.Context, username string) (*model.ResetPasswordResult, error) {
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return nil, err
	}
	password, err := generatePassword()
	if err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = hash
	if err := s.users.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	if err := s.auth.RevokeAllForUser(ctx, u.ID); err != nil {
		return nil, fmt.Errorf("revoke tokens for %s: %w", username, err)
	}
	return &model.ResetPasswordResult{Username: username, Password: password}, nil
}

// DeleteUser 删除用户:禁止删除自己与内置 admin,删除前吊销其全部会话。
func (s *UserService) DeleteUser(ctx context.Context, operator, username string) error {
	if operator == username {
		return errcode.New(errcode.Forbidden, "cannot delete the current user")
	}
	if username == "admin" { // 内置管理员不可删
		return errcode.New(errcode.Forbidden, "cannot delete built-in admin user")
	}
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return err
	}
	if err := s.auth.RevokeAllForUser(ctx, u.ID); err != nil {
		return fmt.Errorf("revoke tokens for %s: %w", username, err)
	}
	return s.users.DeleteUser(ctx, username)
}
