package model

import "time"

// RoleItem 是角色 DTO(GET/POST/PUT /roles)。
type RoleItem struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
}

// UserGroup 是用户组 DTO(GET/POST/PUT /user-groups)。
type UserGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RoleGroup 是角色组 DTO(GET/POST/PUT /role-groups)。
type RoleGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Scope 是授权的生效范围;"*" 表示全部集群 / 全部命名空间。
type Scope struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}

// GrantItem 是授权 DTO(GET/POST /grants)。
type GrantItem struct {
	ID          int64     `json:"id"`
	SubjectType string    `json:"subjectType"` // user|group
	SubjectID   int64     `json:"subjectId"`
	SubjectName string    `json:"subjectName"`
	ObjectType  string    `json:"objectType"` // role|role_group
	ObjectID    int64     `json:"objectId"`
	ObjectName  string    `json:"objectName"`
	Scopes      []Scope   `json:"scopes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// EffectivePermissions 是 GET /users/:username/effective-permissions 的响应 DTO。
type EffectivePermissions struct {
	Permissions []string `json:"permissions"`
}

// 主体类型常量(grant subjectType)。
const (
	SubjectUser  = "user"
	SubjectGroup = "group"
)

// 对象类型常量(grant objectType)。
const (
	ObjectTypeRole      = "role"
	ObjectTypeRoleGroup = "role_group"
)
