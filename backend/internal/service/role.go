package service

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/store"
)

// RoleService 提供 RBAC 管理:角色 / 用户组 / 角色组 / 授权(grants)CRUD
// 与授权解析(EffectivePermissions / Authorize,经 middleware.SetAuthorizer 注入中间件)。
type RoleService struct {
	roles      *store.RoleRepo
	groups     *store.UserGroupRepo
	users      *store.UserRepo
	roleGroups *store.RoleGroupRepo
	grants     *store.GrantRepo
}

// NewRoleService 构造 RoleService。
func NewRoleService(roles *store.RoleRepo, groups *store.UserGroupRepo, users *store.UserRepo,
	roleGroups *store.RoleGroupRepo, grants *store.GrantRepo) *RoleService {
	return &RoleService{roles: roles, groups: groups, users: users, roleGroups: roleGroups, grants: grants}
}

// ---- 角色管理 ----

// ListRoles 列出全部角色(内置 + 自定义)。
func (s *RoleService) ListRoles(ctx context.Context) ([]model.RoleItem, error) {
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.RoleItem, 0, len(roles))
	for i := range roles {
		items = append(items, roleItemOf(&roles[i]))
	}
	return items, nil
}

// CreateRole 创建自定义角色:名称必填且唯一,权限点须合法。
func (s *RoleService) CreateRole(ctx context.Context, name, description string, permissions []string) (*model.RoleItem, error) {
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "role name is required")
	}
	if err := validatePermissions(permissions); err != nil {
		return nil, err
	}
	if _, err := s.roles.GetRoleByName(ctx, name); err == nil {
		return nil, errcode.New(errcode.RoleAlreadyExists, "role name already exists")
	}
	role := &store.Role{
		Name:        name,
		Description: description,
		Builtin:     false,
		Permissions: encodePermissions(permissions),
	}
	if err := s.roles.CreateRole(ctx, role); err != nil {
		return nil, err
	}
	item := roleItemOf(role)
	return &item, nil
}

// UpdateRole 更新角色;内置角色不可修改(40300)。
func (s *RoleService) UpdateRole(ctx context.Context, id int64, name, description string, permissions []string) (*model.RoleItem, error) {
	role, err := s.roles.GetRoleByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.RoleNotFound, "role not found").WithCause(err)
		}
		return nil, err
	}
	if role.Builtin {
		return nil, errcode.New(errcode.Forbidden, "built-in role is immutable")
	}
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "role name is required")
	}
	if err := validatePermissions(permissions); err != nil {
		return nil, err
	}
	if name != role.Name {
		if _, err := s.roles.GetRoleByName(ctx, name); err == nil {
			return nil, errcode.New(errcode.RoleAlreadyExists, "role name already exists")
		}
	}
	role.Name = name
	role.Description = description
	role.Permissions = encodePermissions(permissions)
	if err := s.roles.UpdateRole(ctx, role); err != nil {
		return nil, err
	}
	item := roleItemOf(role)
	return &item, nil
}

// DeleteRole 删除自定义角色及其绑定;内置角色不可删除(40300)。
func (s *RoleService) DeleteRole(ctx context.Context, id int64) error {
	role, err := s.roles.GetRoleByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.RoleNotFound, "role not found").WithCause(err)
		}
		return err
	}
	if role.Builtin {
		return errcode.New(errcode.Forbidden, "built-in role cannot be deleted")
	}
	return s.roles.DeleteRole(ctx, id)
}

// ---- 用户组管理 ----

// ListUserGroups 列出全部用户组。
func (s *RoleService) ListUserGroups(ctx context.Context) ([]model.UserGroup, error) {
	groups, err := s.groups.ListUserGroups(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.UserGroup, 0, len(groups))
	for i := range groups {
		items = append(items, model.UserGroup{
			ID:          groups[i].ID,
			Name:        groups[i].Name,
			Description: groups[i].Description,
			CreatedAt:   groups[i].CreatedAt,
			UpdatedAt:   groups[i].UpdatedAt,
		})
	}
	return items, nil
}

// CreateUserGroup 创建用户组:名称必填且唯一。
func (s *RoleService) CreateUserGroup(ctx context.Context, name, description string) (*model.UserGroup, error) {
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "group name is required")
	}
	if _, err := s.groups.GetUserGroupByName(ctx, name); err == nil {
		return nil, errcode.New(errcode.UserGroupAlreadyExists, "user group name already exists")
	}
	g := &store.UserGroup{Name: name, Description: description}
	if err := s.groups.CreateUserGroup(ctx, g); err != nil {
		return nil, err
	}
	return &model.UserGroup{ID: g.ID, Name: g.Name, Description: g.Description,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}, nil
}

// UpdateUserGroup 更新用户组。
func (s *RoleService) UpdateUserGroup(ctx context.Context, id int64, name, description string) (*model.UserGroup, error) {
	g, err := s.groups.GetUserGroupByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.UserGroupNotFound, "user group not found").WithCause(err)
		}
		return nil, err
	}
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "group name is required")
	}
	if name != g.Name {
		if _, err := s.groups.GetUserGroupByName(ctx, name); err == nil {
			return nil, errcode.New(errcode.UserGroupAlreadyExists, "user group name already exists")
		}
	}
	g.Name = name
	g.Description = description
	if err := s.groups.UpdateUserGroup(ctx, g); err != nil {
		return nil, err
	}
	return &model.UserGroup{ID: g.ID, Name: g.Name, Description: g.Description,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}, nil
}

// DeleteUserGroup 删除用户组(级联清理成员与组级绑定)。
func (s *RoleService) DeleteUserGroup(ctx context.Context, id int64) error {
	if _, err := s.groups.GetUserGroupByID(ctx, id); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserGroupNotFound, "user group not found").WithCause(err)
		}
		return err
	}
	return s.groups.DeleteUserGroup(ctx, id)
}

// ListGroupMembers 列出用户组成员(用户列表)。
func (s *RoleService) ListGroupMembers(ctx context.Context, groupID int64) ([]model.User, error) {
	if _, err := s.groups.GetUserGroupByID(ctx, groupID); err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.UserGroupNotFound, "user group not found").WithCause(err)
		}
		return nil, err
	}
	users, err := s.groups.ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	items := make([]model.User, 0, len(users))
	for i := range users {
		items = append(items, *toUserDTO(&users[i]))
	}
	return items, nil
}

// AddGroupMember 将用户加入用户组(幂等)。
func (s *RoleService) AddGroupMember(ctx context.Context, groupID, userID int64) error {
	if _, err := s.groups.GetUserGroupByID(ctx, groupID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserGroupNotFound, "user group not found").WithCause(err)
		}
		return err
	}
	if _, err := s.users.GetUserByID(ctx, userID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
		}
		return err
	}
	return s.groups.AddGroupMember(ctx, groupID, userID)
}

// RemoveGroupMember 将用户移出用户组。
func (s *RoleService) RemoveGroupMember(ctx context.Context, groupID, userID int64) error {
	if err := s.groups.RemoveGroupMember(ctx, groupID, userID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "user is not a member of the group").WithCause(err)
		}
		return err
	}
	return nil
}

// ---- 角色组管理 ----

// ListRoleGroups 列出全部角色组。
func (s *RoleService) ListRoleGroups(ctx context.Context) ([]model.RoleGroup, error) {
	groups, err := s.roleGroups.ListRoleGroups(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.RoleGroup, 0, len(groups))
	for i := range groups {
		items = append(items, model.RoleGroup{
			ID:          groups[i].ID,
			Name:        groups[i].Name,
			Description: groups[i].Description,
			CreatedAt:   groups[i].CreatedAt,
			UpdatedAt:   groups[i].UpdatedAt,
		})
	}
	return items, nil
}

// CreateRoleGroup 创建角色组:名称必填且唯一(40413)。
func (s *RoleService) CreateRoleGroup(ctx context.Context, name, description string) (*model.RoleGroup, error) {
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "role group name is required")
	}
	if _, err := s.roleGroups.GetRoleGroupByName(ctx, name); err == nil {
		return nil, errcode.New(errcode.RoleAlreadyExists, "role group name already exists")
	}
	g := &store.RoleGroup{Name: name, Description: description}
	if err := s.roleGroups.CreateRoleGroup(ctx, g); err != nil {
		return nil, err
	}
	return &model.RoleGroup{ID: g.ID, Name: g.Name, Description: g.Description,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}, nil
}

// UpdateRoleGroup 更新角色组;名称唯一(40413)。
func (s *RoleService) UpdateRoleGroup(ctx context.Context, id int64, name, description string) (*model.RoleGroup, error) {
	g, err := s.roleGroups.GetRoleGroupByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.NotFound, "role group not found").WithCause(err)
		}
		return nil, err
	}
	if name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "role group name is required")
	}
	if name != g.Name {
		if _, err := s.roleGroups.GetRoleGroupByName(ctx, name); err == nil {
			return nil, errcode.New(errcode.RoleAlreadyExists, "role group name already exists")
		}
	}
	g.Name = name
	g.Description = description
	if err := s.roleGroups.UpdateRoleGroup(ctx, g); err != nil {
		return nil, err
	}
	return &model.RoleGroup{ID: g.ID, Name: g.Name, Description: g.Description,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}, nil
}

// DeleteRoleGroup 删除角色组(级联清理组内角色关联与以该组为对象的授权)。
func (s *RoleService) DeleteRoleGroup(ctx context.Context, id int64) error {
	if _, err := s.roleGroups.GetRoleGroupByID(ctx, id); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "role group not found").WithCause(err)
		}
		return err
	}
	return s.roleGroups.DeleteRoleGroup(ctx, id)
}

// ListRoleGroupRoles 列出角色组内角色(RoleItem[])。
func (s *RoleService) ListRoleGroupRoles(ctx context.Context, groupID int64) ([]model.RoleItem, error) {
	if _, err := s.roleGroups.GetRoleGroupByID(ctx, groupID); err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.NotFound, "role group not found").WithCause(err)
		}
		return nil, err
	}
	roles, err := s.roleGroups.ListGroupRoles(ctx, groupID)
	if err != nil {
		return nil, err
	}
	items := make([]model.RoleItem, 0, len(roles))
	for i := range roles {
		items = append(items, roleItemOf(&roles[i]))
	}
	return items, nil
}

// AddRoleGroupRole 将角色加入角色组(幂等);角色组与角色都必须存在。
func (s *RoleService) AddRoleGroupRole(ctx context.Context, groupID, roleID int64) error {
	if _, err := s.roleGroups.GetRoleGroupByID(ctx, groupID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "role group not found").WithCause(err)
		}
		return err
	}
	if _, err := s.roles.GetRoleByID(ctx, roleID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.RoleNotFound, "role not found").WithCause(err)
		}
		return err
	}
	return s.roleGroups.AddGroupRole(ctx, groupID, roleID)
}

// RemoveRoleGroupRole 将角色移出角色组。
func (s *RoleService) RemoveRoleGroupRole(ctx context.Context, groupID, roleID int64) error {
	if _, err := s.roleGroups.GetRoleGroupByID(ctx, groupID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "role group not found").WithCause(err)
		}
		return err
	}
	if err := s.roleGroups.RemoveGroupRole(ctx, groupID, roleID); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "role is not a member of the role group").WithCause(err)
		}
		return err
	}
	return nil
}

// ---- 授权(grants)管理 ----

// ListGrants 按过滤条件列出授权(subject/object 四个参数均可选、可组合,
// 支持按主体反向查看与按对象反向查看);subjectName/objectName 实时解析。
func (s *RoleService) ListGrants(ctx context.Context, subjectType string, subjectID int64,
	objectType string, objectID int64) ([]model.GrantItem, error) {
	if err := validateSubjectType(subjectType); err != nil {
		return nil, err
	}
	if err := validateObjectType(objectType); err != nil {
		return nil, err
	}
	grants, err := s.grants.ListGrants(ctx, store.GrantFilter{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		ObjectType:  objectType,
		ObjectID:    objectID,
	})
	if err != nil {
		return nil, err
	}
	items := make([]model.GrantItem, 0, len(grants))
	for i := range grants {
		item, err := s.grantItemOf(ctx, &grants[i])
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

// CreateGrant 创建授权:主体与对象必须存在,scope 至少一条,
// cluster="*" 时 namespace 必须为 "*",重复授权返回 40406。
func (s *RoleService) CreateGrant(ctx context.Context, subjectType string, subjectID int64,
	objectType string, objectID int64, scopes []model.Scope) (*model.GrantItem, error) {
	if err := validateSubjectType(subjectType); err != nil {
		return nil, err
	}
	if err := validateObjectType(objectType); err != nil {
		return nil, err
	}
	if err := s.checkSubjectExists(ctx, subjectType, subjectID); err != nil {
		return nil, err
	}
	objectName, err := s.objectName(ctx, objectType, objectID)
	if err != nil {
		return nil, err
	}
	if err := validateScopes(scopes); err != nil {
		return nil, err
	}
	if _, err := s.grants.GetGrant(ctx, subjectType, subjectID, objectType, objectID); err == nil {
		return nil, errcode.New(errcode.ResourceConflict, "grant already exists")
	}
	subjectName, err := s.subjectName(ctx, subjectType, subjectID)
	if err != nil {
		return nil, err
	}
	g := &store.Grant{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		ObjectType:  objectType,
		ObjectID:    objectID,
	}
	for _, sc := range scopes {
		g.Scopes = append(g.Scopes, store.GrantScope{Cluster: sc.Cluster, Namespace: sc.Namespace})
	}
	if err := s.grants.CreateGrant(ctx, g); err != nil {
		return nil, err
	}
	return &model.GrantItem{
		ID:          g.ID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		SubjectName: subjectName,
		ObjectType:  objectType,
		ObjectID:    objectID,
		ObjectName:  objectName,
		Scopes:      scopes,
		CreatedAt:   g.CreatedAt,
	}, nil
}

// DeleteGrant 删除授权及其 scope。
func (s *RoleService) DeleteGrant(ctx context.Context, id int64) error {
	if err := s.grants.DeleteGrant(ctx, id); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.NotFound, "grant not found").WithCause(err)
		}
		return err
	}
	return nil
}

// ---- 授权解析 ----

// EffectivePermissions 计算用户有效权限:身份 role 列的内置权限
// ∪ 所有命中 grant 展开的权限点(object_type=role → 该角色权限点;
// object_type=role_group → 该组内全部角色权限点的并集),命中部分按
// (cluster, namespace) scope 过滤;内置 admin 恒为 ["*"]。
// cluster/namespace 为空表示全局查询(不按 scope 过滤)。
func (s *RoleService) EffectivePermissions(ctx context.Context, userID int64, builtinRole, cluster, namespace string) ([]string, error) {
	if builtinRole == model.RoleAdmin {
		return []string{PermAll}, nil
	}
	set := map[string]struct{}{}
	for _, p := range PermissionsFor(builtinRole) {
		set[p] = struct{}{}
	}
	grants, err := s.grants.GrantsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range grants {
		if !scopeMatches(grants[i].Scopes, cluster, namespace) {
			continue
		}
		perms, err := s.permissionsForObject(ctx, grants[i].ObjectType, grants[i].ObjectID)
		if err != nil {
			return nil, err
		}
		for _, p := range perms {
			set[p] = struct{}{}
		}
	}
	perms := make([]string, 0, len(set))
	for p := range set {
		perms = append(perms, p)
	}
	sort.Strings(perms)
	return perms, nil
}

// Authorize 判定用户是否持有指定权限点(带 scope),供 middleware.SetAuthorizer 注入。
func (s *RoleService) Authorize(userID int64, builtinRole, perm, cluster, namespace string) bool {
	perms, err := s.EffectivePermissions(context.Background(), userID, builtinRole, cluster, namespace)
	if err != nil {
		return false
	}
	for _, p := range perms {
		if p == PermAll || p == perm {
			return true
		}
	}
	return false
}

// ---- 内部辅助 ----

// permissionsForObject 展开 grant 对象的权限点:
// role → 该角色权限点;role_group → 组内全部角色权限点的并集。
// 对象已被删除时返回空集(grant 由级联清理兜底,此处防御性跳过)。
func (s *RoleService) permissionsForObject(ctx context.Context, objectType string, objectID int64) ([]string, error) {
	switch objectType {
	case model.ObjectTypeRole:
		role, err := s.roles.GetRoleByID(ctx, objectID)
		if err != nil {
			if isNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return decodePermissions(role.Permissions), nil
	case model.ObjectTypeRoleGroup:
		roles, err := s.roleGroups.ListGroupRoles(ctx, objectID)
		if err != nil {
			return nil, err
		}
		var perms []string
		for i := range roles {
			perms = append(perms, decodePermissions(roles[i].Permissions)...)
		}
		return perms, nil
	}
	return nil, nil
}

// scopeMatches 判断授权 scope 是否覆盖 (cluster, namespace):
// scope.cluster 为 "*" 或相等,且 scope.namespace 为 "*" 或相等即命中;
// 查询侧为空表示全局(不做 scope 过滤)。
func scopeMatches(scopes []store.GrantScope, cluster, namespace string) bool {
	if cluster == "" && namespace == "" {
		return true
	}
	for _, sc := range scopes {
		clusterOK := cluster == "" || sc.Cluster == "*" || sc.Cluster == cluster
		nsOK := namespace == "" || sc.Namespace == "*" || sc.Namespace == namespace
		if clusterOK && nsOK {
			return true
		}
	}
	return false
}

// validateScopes 校验授权 scope:至少一条、cluster/namespace 非空、
// cluster="*" 时 namespace 必须为 "*"。
func validateScopes(scopes []model.Scope) error {
	if len(scopes) == 0 {
		return errcode.New(errcode.ParamInvalid, "at least one scope is required")
	}
	for _, sc := range scopes {
		if sc.Cluster == "" || sc.Namespace == "" {
			return errcode.New(errcode.ParamInvalid, "scope cluster and namespace are required")
		}
		if sc.Cluster == "*" && sc.Namespace != "*" {
			return errcode.New(errcode.ParamInvalid, `namespace must be "*" when cluster is "*"`)
		}
	}
	return nil
}

// validateSubjectType 校验 subjectType 合法;空串视为不过滤。
func validateSubjectType(subjectType string) error {
	if subjectType == "" || subjectType == model.SubjectUser || subjectType == model.SubjectGroup {
		return nil
	}
	return errcode.New(errcode.ParamInvalid, "subjectType must be user or group")
}

// validateObjectType 校验 objectType 合法;空串视为不过滤。
func validateObjectType(objectType string) error {
	if objectType == "" || objectType == model.ObjectTypeRole || objectType == model.ObjectTypeRoleGroup {
		return nil
	}
	return errcode.New(errcode.ParamInvalid, "objectType must be role or role_group")
}

// validatePermissions 校验权限点列表均合法。
func validatePermissions(permissions []string) error {
	for _, p := range permissions {
		if !validPermissionPoint(p) {
			return errcode.New(errcode.ParamInvalid, "unknown permission point: "+p)
		}
	}
	return nil
}

// checkSubjectExists 校验授权主体存在。
func (s *RoleService) checkSubjectExists(ctx context.Context, subjectType string, subjectID int64) error {
	_, err := s.subjectName(ctx, subjectType, subjectID)
	return err
}

// subjectName 解析主体展示名(用户名 / 用户组名);不存在返回 404xx。
func (s *RoleService) subjectName(ctx context.Context, subjectType string, subjectID int64) (string, error) {
	switch subjectType {
	case model.SubjectUser:
		u, err := s.users.GetUserByID(ctx, subjectID)
		if err != nil {
			if isNotFound(err) {
				return "", errcode.New(errcode.UserNotFound, "user not found").WithCause(err)
			}
			return "", err
		}
		return u.Username, nil
	case model.SubjectGroup:
		g, err := s.groups.GetUserGroupByID(ctx, subjectID)
		if err != nil {
			if isNotFound(err) {
				return "", errcode.New(errcode.UserGroupNotFound, "user group not found").WithCause(err)
			}
			return "", err
		}
		return g.Name, nil
	}
	return "", errcode.New(errcode.ParamInvalid, "subjectType must be user or group")
}

// objectName 解析授权对象展示名(角色名 / 角色组名);不存在返回 404xx。
func (s *RoleService) objectName(ctx context.Context, objectType string, objectID int64) (string, error) {
	switch objectType {
	case model.ObjectTypeRole:
		r, err := s.roles.GetRoleByID(ctx, objectID)
		if err != nil {
			if isNotFound(err) {
				return "", errcode.New(errcode.RoleNotFound, "role not found").WithCause(err)
			}
			return "", err
		}
		return r.Name, nil
	case model.ObjectTypeRoleGroup:
		g, err := s.roleGroups.GetRoleGroupByID(ctx, objectID)
		if err != nil {
			if isNotFound(err) {
				return "", errcode.New(errcode.NotFound, "role group not found").WithCause(err)
			}
			return "", err
		}
		return g.Name, nil
	}
	return "", errcode.New(errcode.ParamInvalid, "objectType must be role or role_group")
}

// grantItemOf 将授权实体转为 DTO(subjectName/objectName 实时解析)。
func (s *RoleService) grantItemOf(ctx context.Context, g *store.Grant) (*model.GrantItem, error) {
	subjectName, err := s.subjectName(ctx, g.SubjectType, g.SubjectID)
	if err != nil {
		return nil, err
	}
	objectName, err := s.objectName(ctx, g.ObjectType, g.ObjectID)
	if err != nil {
		return nil, err
	}
	item := model.GrantItem{
		ID:          g.ID,
		SubjectType: g.SubjectType,
		SubjectID:   g.SubjectID,
		SubjectName: subjectName,
		ObjectType:  g.ObjectType,
		ObjectID:    g.ObjectID,
		ObjectName:  objectName,
		Scopes:      make([]model.Scope, 0, len(g.Scopes)),
		CreatedAt:   g.CreatedAt,
	}
	for _, sc := range g.Scopes {
		item.Scopes = append(item.Scopes, model.Scope{Cluster: sc.Cluster, Namespace: sc.Namespace})
	}
	return &item, nil
}

// roleItemOf 将角色实体转为 DTO。
func roleItemOf(r *store.Role) model.RoleItem {
	return model.RoleItem{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Builtin:     r.Builtin,
		Permissions: decodePermissions(r.Permissions),
	}
}

// encodePermissions 序列化权限点列表为 JSON 数组字符串(永不为 nil)。
func encodePermissions(perms []string) string {
	if perms == nil {
		perms = []string{}
	}
	b, err := json.Marshal(perms)
	if err != nil { // []string 序列化不会失败
		return "[]"
	}
	return string(b)
}

// decodePermissions 反序列化 JSON 数组字符串;空串或非法内容返回空集。
func decodePermissions(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var perms []string
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return []string{}
	}
	if perms == nil {
		return []string{}
	}
	return perms
}
