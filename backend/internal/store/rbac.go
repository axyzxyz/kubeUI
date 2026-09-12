package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// RoleRepo roles 表访问(角色组与授权见 RoleGroupRepo / GrantRepo)。
type RoleRepo struct{ db *gorm.DB }

// NewRoleRepo 构造 RoleRepo。
func NewRoleRepo(db *gorm.DB) *RoleRepo { return &RoleRepo{db: db} }

// ListRoles 列出全部角色(内置 + 自定义),按 ID 升序。
func (r *RoleRepo) ListRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	if err := r.db.WithContext(ctx).Order("id").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return roles, nil
}

// GetRoleByID 按 ID 查询角色。
func (r *RoleRepo) GetRoleByID(ctx context.Context, id int64) (*Role, error) {
	var role Role
	if err := r.db.WithContext(ctx).First(&role, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("role %d", id))
	}
	return &role, nil
}

// GetRoleByName 按名称查询角色。
func (r *RoleRepo) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	var role Role
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&role).Error; err != nil {
		return nil, wrapNotFound(err, "role "+name)
	}
	return &role, nil
}

// CreateRole 创建角色。
func (r *RoleRepo) CreateRole(ctx context.Context, role *Role) error {
	if err := r.db.WithContext(ctx).Create(role).Error; err != nil {
		return fmt.Errorf("create role %s: %w", role.Name, err)
	}
	return nil
}

// UpdateRole 保存角色变更(名称/描述/权限)。
func (r *RoleRepo) UpdateRole(ctx context.Context, role *Role) error {
	if err := r.db.WithContext(ctx).Save(role).Error; err != nil {
		return fmt.Errorf("update role %d: %w", role.ID, err)
	}
	return nil
}

// DeleteRole 删除角色,并级联删除引用它的 role_group_roles 与 grants
// (含 grant_scopes);调用方负责 builtin 保护。
func (r *RoleRepo) DeleteRole(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&RoleGroupRole{}).Error; err != nil {
			return fmt.Errorf("delete role_group_roles of role %d: %w", id, err)
		}
		if err := tx.Exec(`DELETE FROM grant_scopes WHERE grant_id IN
			(SELECT id FROM grants WHERE object_type = 'role' AND object_id = ?)`, id).Error; err != nil {
			return fmt.Errorf("delete grant_scopes of role %d: %w", id, err)
		}
		if err := tx.Where("object_type = ? AND object_id = ?", "role", id).Delete(&Grant{}).Error; err != nil {
			return fmt.Errorf("delete grants of role %d: %w", id, err)
		}
		res := tx.Delete(&Role{}, id)
		if res.Error != nil {
			return fmt.Errorf("delete role %d: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("role %d: %w", id, ErrNotFound)
		}
		return nil
	})
}

// grantPreloads 返回授权关联的 Preload 链(scope)。
func grantPreloads(tx *gorm.DB) *gorm.DB { return tx.Preload("Scopes") }

// RoleGroupRepo role_groups / role_group_roles 表访问。
type RoleGroupRepo struct{ db *gorm.DB }

// NewRoleGroupRepo 构造 RoleGroupRepo。
func NewRoleGroupRepo(db *gorm.DB) *RoleGroupRepo { return &RoleGroupRepo{db: db} }

// ListRoleGroups 列出全部角色组,按 ID 升序。
func (r *RoleGroupRepo) ListRoleGroups(ctx context.Context) ([]RoleGroup, error) {
	var groups []RoleGroup
	if err := r.db.WithContext(ctx).Order("id").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list role groups: %w", err)
	}
	return groups, nil
}

// GetRoleGroupByID 按 ID 查询角色组。
func (r *RoleGroupRepo) GetRoleGroupByID(ctx context.Context, id int64) (*RoleGroup, error) {
	var g RoleGroup
	if err := r.db.WithContext(ctx).First(&g, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("role group %d", id))
	}
	return &g, nil
}

// GetRoleGroupByName 按名称查询角色组。
func (r *RoleGroupRepo) GetRoleGroupByName(ctx context.Context, name string) (*RoleGroup, error) {
	var g RoleGroup
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&g).Error; err != nil {
		return nil, wrapNotFound(err, "role group "+name)
	}
	return &g, nil
}

// CreateRoleGroup 创建角色组。
func (r *RoleGroupRepo) CreateRoleGroup(ctx context.Context, g *RoleGroup) error {
	if err := r.db.WithContext(ctx).Create(g).Error; err != nil {
		return fmt.Errorf("create role group %s: %w", g.Name, err)
	}
	return nil
}

// UpdateRoleGroup 保存角色组变更。
func (r *RoleGroupRepo) UpdateRoleGroup(ctx context.Context, g *RoleGroup) error {
	if err := r.db.WithContext(ctx).Save(g).Error; err != nil {
		return fmt.Errorf("update role group %d: %w", g.ID, err)
	}
	return nil
}

// DeleteRoleGroup 删除角色组及其成员关联与以该组为对象的 grants(级联)。
func (r *RoleGroupRepo) DeleteRoleGroup(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM grant_scopes WHERE grant_id IN
			(SELECT id FROM grants WHERE object_type = 'role_group' AND object_id = ?)`, id).Error; err != nil {
			return fmt.Errorf("delete grant_scopes of role group %d: %w", id, err)
		}
		if err := tx.Where("object_type = ? AND object_id = ?", "role_group", id).Delete(&Grant{}).Error; err != nil {
			return fmt.Errorf("delete grants of role group %d: %w", id, err)
		}
		if err := tx.Where("role_group_id = ?", id).Delete(&RoleGroupRole{}).Error; err != nil {
			return fmt.Errorf("delete roles of role group %d: %w", id, err)
		}
		res := tx.Delete(&RoleGroup{}, id)
		if res.Error != nil {
			return fmt.Errorf("delete role group %d: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("role group %d: %w", id, ErrNotFound)
		}
		return nil
	})
}

// ListGroupRoles 列出角色组内的全部角色(join roles),按角色 ID 升序。
func (r *RoleGroupRepo) ListGroupRoles(ctx context.Context, groupID int64) ([]Role, error) {
	var roles []Role
	if err := r.db.WithContext(ctx).
		Joins("JOIN role_group_roles rgr ON rgr.role_id = roles.id").
		Where("rgr.role_group_id = ?", groupID).
		Order("roles.id").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("list roles of role group %d: %w", groupID, err)
	}
	return roles, nil
}

// AddGroupRole 将角色加入角色组(幂等)。
func (r *RoleGroupRepo) AddGroupRole(ctx context.Context, groupID, roleID int64) error {
	gr := RoleGroupRole{RoleGroupID: groupID, RoleID: roleID, CreatedAt: time.Now()}
	if err := r.db.WithContext(ctx).
		Where(RoleGroupRole{RoleGroupID: groupID, RoleID: roleID}).
		FirstOrCreate(&gr).Error; err != nil {
		return fmt.Errorf("add role %d to role group %d: %w", roleID, groupID, err)
	}
	return nil
}

// RemoveGroupRole 将角色移出角色组;不在组内返回 ErrNotFound。
func (r *RoleGroupRepo) RemoveGroupRole(ctx context.Context, groupID, roleID int64) error {
	res := r.db.WithContext(ctx).
		Where("role_group_id = ? AND role_id = ?", groupID, roleID).
		Delete(&RoleGroupRole{})
	if res.Error != nil {
		return fmt.Errorf("remove role %d from role group %d: %w", roleID, groupID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("role %d in role group %d: %w", roleID, groupID, ErrNotFound)
	}
	return nil
}

// GrantRepo grants / grant_scopes 表访问。
type GrantRepo struct{ db *gorm.DB }

// NewGrantRepo 构造 GrantRepo。
func NewGrantRepo(db *gorm.DB) *GrantRepo { return &GrantRepo{db: db} }

// GrantFilter 是授权列表过滤条件;零值表示不过滤(可任意组合)。
type GrantFilter struct {
	SubjectType string
	SubjectID   int64
	ObjectType  string
	ObjectID    int64
}

// ListGrants 按过滤条件列出 grants(含 scope),按 ID 升序。
func (r *GrantRepo) ListGrants(ctx context.Context, f GrantFilter) ([]Grant, error) {
	q := r.db.WithContext(ctx).Model(&Grant{})
	if f.SubjectType != "" {
		q = q.Where("subject_type = ?", f.SubjectType)
	}
	if f.SubjectID != 0 {
		q = q.Where("subject_id = ?", f.SubjectID)
	}
	if f.ObjectType != "" {
		q = q.Where("object_type = ?", f.ObjectType)
	}
	if f.ObjectID != 0 {
		q = q.Where("object_id = ?", f.ObjectID)
	}
	var grants []Grant
	if err := q.Scopes(grantPreloads).Order("id").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	return grants, nil
}

// GetGrantByID 按 ID 查询授权(含 scope)。
func (r *GrantRepo) GetGrantByID(ctx context.Context, id int64) (*Grant, error) {
	var g Grant
	if err := r.db.WithContext(ctx).Scopes(grantPreloads).First(&g, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("grant %d", id))
	}
	return &g, nil
}

// GetGrant 按联合唯一键查询授权(存在性校验)。
func (r *GrantRepo) GetGrant(ctx context.Context, subjectType string, subjectID int64, objectType string, objectID int64) (*Grant, error) {
	var g Grant
	q := Grant{SubjectType: subjectType, SubjectID: subjectID, ObjectType: objectType, ObjectID: objectID}
	if err := r.db.WithContext(ctx).Where(q).First(&g).Error; err != nil {
		return nil, wrapNotFound(err, "grant")
	}
	return &g, nil
}

// CreateGrant 创建授权及其 scope(同一事务)。
func (r *GrantRepo) CreateGrant(ctx context.Context, g *Grant) error {
	if err := r.db.WithContext(ctx).Create(g).Error; err != nil {
		return fmt.Errorf("create grant: %w", err)
	}
	return nil
}

// DeleteGrant 删除授权及其全部 scope。
func (r *GrantRepo) DeleteGrant(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("grant_id = ?", id).Delete(&GrantScope{}).Error; err != nil {
			return fmt.Errorf("delete scopes of grant %d: %w", id, err)
		}
		res := tx.Delete(&Grant{}, id)
		if res.Error != nil {
			return fmt.Errorf("delete grant %d: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("grant %d: %w", id, ErrNotFound)
		}
		return nil
	})
}

// GrantsForUser 返回直接授予该用户或其所在用户组的全部授权(含 scope)。
func (r *GrantRepo) GrantsForUser(ctx context.Context, userID int64) ([]Grant, error) {
	groupIDs := r.db.Model(&UserGroupMember{}).Select("group_id").Where("user_id = ?", userID)
	var grants []Grant
	if err := r.db.WithContext(ctx).Scopes(grantPreloads).
		Where("subject_type = ? AND subject_id = ?", "user", userID).
		Or("subject_type = ? AND subject_id IN (?)", "group", groupIDs).
		Order("id").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("list grants of user %d: %w", userID, err)
	}
	return grants, nil
}

// UserGroupRepo user_groups / user_group_members 表访问。
type UserGroupRepo struct{ db *gorm.DB }

// NewUserGroupRepo 构造 UserGroupRepo。
func NewUserGroupRepo(db *gorm.DB) *UserGroupRepo { return &UserGroupRepo{db: db} }

// ListUserGroups 列出全部用户组,按 ID 升序。
func (r *UserGroupRepo) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	var groups []UserGroup
	if err := r.db.WithContext(ctx).Order("id").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list user groups: %w", err)
	}
	return groups, nil
}

// GetUserGroupByID 按 ID 查询用户组。
func (r *UserGroupRepo) GetUserGroupByID(ctx context.Context, id int64) (*UserGroup, error) {
	var g UserGroup
	if err := r.db.WithContext(ctx).First(&g, id).Error; err != nil {
		return nil, wrapNotFound(err, fmt.Sprintf("user group %d", id))
	}
	return &g, nil
}

// GetUserGroupByName 按名称查询用户组。
func (r *UserGroupRepo) GetUserGroupByName(ctx context.Context, name string) (*UserGroup, error) {
	var g UserGroup
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&g).Error; err != nil {
		return nil, wrapNotFound(err, "user group "+name)
	}
	return &g, nil
}

// CreateUserGroup 创建用户组。
func (r *UserGroupRepo) CreateUserGroup(ctx context.Context, g *UserGroup) error {
	if err := r.db.WithContext(ctx).Create(g).Error; err != nil {
		return fmt.Errorf("create user group %s: %w", g.Name, err)
	}
	return nil
}

// UpdateUserGroup 保存用户组变更。
func (r *UserGroupRepo) UpdateUserGroup(ctx context.Context, g *UserGroup) error {
	if err := r.db.WithContext(ctx).Save(g).Error; err != nil {
		return fmt.Errorf("update user group %d: %w", g.ID, err)
	}
	return nil
}

// DeleteUserGroup 删除用户组及其成员关联与以该组为主体的 grants(级联)。
func (r *UserGroupRepo) DeleteUserGroup(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM grant_scopes WHERE grant_id IN
			(SELECT id FROM grants WHERE subject_type = 'group' AND subject_id = ?)`, id).Error; err != nil {
			return fmt.Errorf("delete grant_scopes of group %d: %w", id, err)
		}
		if err := tx.Where("subject_type = ? AND subject_id = ?", "group", id).Delete(&Grant{}).Error; err != nil {
			return fmt.Errorf("delete grants of group %d: %w", id, err)
		}
		if err := tx.Where("group_id = ?", id).Delete(&UserGroupMember{}).Error; err != nil {
			return fmt.Errorf("delete members of group %d: %w", id, err)
		}
		res := tx.Delete(&UserGroup{}, id)
		if res.Error != nil {
			return fmt.Errorf("delete user group %d: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("user group %d: %w", id, ErrNotFound)
		}
		return nil
	})
}

// ListGroupMembers 列出用户组成员(join users),按用户 ID 升序。
func (r *UserGroupRepo) ListGroupMembers(ctx context.Context, groupID int64) ([]User, error) {
	var users []User
	if err := r.db.WithContext(ctx).
		Joins("JOIN user_group_members ugm ON ugm.user_id = users.id").
		Where("ugm.group_id = ?", groupID).
		Order("users.id").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list members of group %d: %w", groupID, err)
	}
	return users, nil
}

// AddGroupMember 将用户加入用户组(幂等)。
func (r *UserGroupRepo) AddGroupMember(ctx context.Context, groupID, userID int64) error {
	m := UserGroupMember{GroupID: groupID, UserID: userID, CreatedAt: time.Now()}
	if err := r.db.WithContext(ctx).
		Where(UserGroupMember{GroupID: groupID, UserID: userID}).
		FirstOrCreate(&m).Error; err != nil {
		return fmt.Errorf("add user %d to group %d: %w", userID, groupID, err)
	}
	return nil
}

// RemoveGroupMember 将用户移出用户组;不在组内返回 ErrNotFound。
func (r *UserGroupRepo) RemoveGroupMember(ctx context.Context, groupID, userID int64) error {
	res := r.db.WithContext(ctx).
		Where("group_id = ? AND user_id = ?", groupID, userID).
		Delete(&UserGroupMember{})
	if res.Error != nil {
		return fmt.Errorf("remove user %d from group %d: %w", userID, groupID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("user %d in group %d: %w", userID, groupID, ErrNotFound)
	}
	return nil
}
