// Package store 为 GORM 持久化层,默认 sqlite(纯 Go 驱动),可切换 postgres。
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Open 打开数据库连接并执行 AutoMigrate。
// driver 支持 "sqlite"(默认)与 "postgres"。
func Open(driver, dsn string) (*gorm.DB, error) {
	var dial gorm.Dialector
	switch driver {
	case "", "sqlite":
		if dir := filepath.Dir(dsn); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite dir %s: %w", dir, err)
			}
		}
		dial = sqlite.Open(dsn)
	case "postgres":
		dial = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := gorm.Open(dial, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(allEntities()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	migrateLegacyRoleBindings(db)
	return db, nil
}

func allEntities() []any {
	return []any{
		&User{},
		&RefreshToken{},
		&Cluster{},
		&AuditLog{},
		&Role{},
		&RoleGroup{},
		&RoleGroupRole{},
		&Grant{},
		&GrantScope{},
		&UserGroup{},
		&UserGroupMember{},
		&EnrollToken{},
		&IssuedKubeconfig{},
	}
}

// migrateLegacyRoleBindings 把旧 role_bindings(+ role_scopes)表迁移为中心授权表:
// 每行 role→subject 绑定转换为 object_type=role 的 grant(含 scopes),随后 DROP 旧表。
// 幂等:role_bindings 表不存在时跳过。必须在 AutoMigrate 之后调用(grants 表已建)。
func migrateLegacyRoleBindings(db *gorm.DB) {
	if !db.Migrator().HasTable("role_bindings") {
		return
	}

	// 读取旧绑定行;仅缺 subject 列的更早期 schema 无法换算,置空后只做清理。
	type legacyBinding struct {
		ID          int64
		RoleID      int64
		SubjectType string
		SubjectID   int64
	}
	var rows []legacyBinding
	if err := db.Raw("SELECT id, role_id, subject_type, subject_id FROM role_bindings").Scan(&rows).Error; err != nil {
		fmt.Fprintf(os.Stderr, "WARN migrate legacy role_bindings: read rows failed (drop only): %v\n", err)
		rows = nil
	}

	// 旧 scope 按 binding_id 分组;表缺失视为无 scope。
	scopesByBinding := map[int64][]GrantScope{}
	if db.Migrator().HasTable("role_scopes") {
		var scopes []struct {
			BindingID int64
			Cluster   string
			Namespace string
		}
		if err := db.Raw("SELECT binding_id, cluster, namespace FROM role_scopes").Scan(&scopes).Error; err != nil {
			fmt.Fprintf(os.Stderr, "WARN migrate legacy role_scopes: read rows failed: %v\n", err)
		}
		for _, sc := range scopes {
			scopesByBinding[sc.BindingID] = append(scopesByBinding[sc.BindingID],
				GrantScope{Cluster: sc.Cluster, Namespace: sc.Namespace})
		}
	}

	for _, r := range rows {
		if r.SubjectType == "" || r.SubjectID <= 0 || r.RoleID <= 0 {
			continue // 非法/残缺行跳过,不阻断迁移
		}
		g := Grant{
			SubjectType: r.SubjectType,
			SubjectID:   r.SubjectID,
			ObjectType:  "role",
			ObjectID:    r.RoleID,
			Scopes:      scopesByBinding[r.ID],
		}
		if err := db.Create(&g).Error; err != nil {
			fmt.Fprintf(os.Stderr, "WARN migrate legacy role_bindings %d: create grant: %v\n", r.ID, err)
		}
	}

	if err := db.Migrator().DropTable("role_bindings"); err != nil {
		fmt.Fprintf(os.Stderr, "WARN drop legacy table role_bindings: %v\n", err)
	}
	if db.Migrator().HasTable("role_scopes") {
		if err := db.Migrator().DropTable("role_scopes"); err != nil {
			fmt.Fprintf(os.Stderr, "WARN drop legacy table role_scopes: %v\n", err)
		}
	}
}

// User 是 users 表实体,密码仅存 bcrypt 哈希。
type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	Role         string    `gorm:"size:32;not null;default:viewer" json:"role"`
	Disabled     bool      `gorm:"not null;default:false" json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// RefreshToken 是 refresh_tokens 表实体,只存 SHA-256 哈希,可吊销。
type RefreshToken struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `gorm:"index;not null" json:"userId"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`
	Revoked   bool      `gorm:"not null;default:false" json:"revoked"`
	CreatedAt time.Time `json:"createdAt"`
}

// Cluster 是 clusters 表实体,KubeconfigEncrypted 为 AES-256-GCM 密文,禁止外泄。
type Cluster struct {
	ID                  int64     `gorm:"primaryKey" json:"id"`
	Name                string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Description         string    `gorm:"size:256" json:"description"`
	KubeconfigEncrypted string    `gorm:"type:text;not null" json:"-"`
	AccessMode          string    `gorm:"size:16;not null;default:direct" json:"accessMode"`
	Status              string    `gorm:"size:32;not null;default:offline" json:"status"`
	Version             string    `gorm:"size:32" json:"version,omitempty"`
	Message             string    `gorm:"size:256" json:"message,omitempty"`
	LastTransitionTime  time.Time `json:"lastTransitionTime"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// AuditLog 是 audit_logs 表实体,只追加、不可修改与删除。
type AuditLog struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	RequestID    string    `gorm:"size:64;index" json:"requestId"`
	UserID       int64     `gorm:"index" json:"userId"`
	Username     string    `gorm:"size:64" json:"username"`
	Action       string    `gorm:"size:64;index" json:"action"`
	Resource     string    `gorm:"size:128" json:"resource"`
	ResourceType string    `gorm:"size:64;index" json:"resourceType"`
	Cluster      string    `gorm:"size:128;index" json:"cluster,omitempty"`
	Namespace    string    `gorm:"size:128" json:"namespace,omitempty"`
	Name         string    `gorm:"size:256" json:"name,omitempty"`
	SourceIP     string    `gorm:"size:64" json:"sourceIp"`
	UserAgent    string    `gorm:"size:256" json:"userAgent"`
	Result       string    `gorm:"size:16" json:"result"` // allow|deny
	CreatedAt    time.Time `gorm:"index" json:"createdAt"`
}

// Role 是 roles 表实体,内置 admin/operator/viewer + 自定义角色;
// Permissions 为 JSON 数组字符串(如 ["resources:write"]),空表视为无权限。
type Role struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Description string    `gorm:"size:256" json:"description"`
	Builtin     bool      `gorm:"not null;default:false" json:"builtin"`
	Permissions string    `gorm:"type:text" json:"-"` // JSON 数组字符串,对外经 DTO 暴露
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RoleGroup 是 role_groups 表实体:一组角色,作为 grant 的授权对象。
type RoleGroup struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Description string    `gorm:"size:256" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RoleGroupRole 是 role_group_roles 表实体,(role_group_id, role_id) 联合唯一。
type RoleGroupRole struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	RoleGroupID int64     `gorm:"uniqueIndex:idx_group_role;not null" json:"roleGroupId"`
	RoleID      int64     `gorm:"uniqueIndex:idx_group_role;not null" json:"roleId"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Grant 是 grants 表实体(中心授权表,唯一关联事实源):把某 role 或某 role_group
// 授予某 user 或某 user_group。(subject_type, subject_id, object_type, object_id) 联合唯一。
type Grant struct {
	ID          int64        `gorm:"primaryKey" json:"id"`
	SubjectType string       `gorm:"uniqueIndex:idx_grant_subject_object;size:16;not null;default:user" json:"subjectType"` // user|group
	SubjectID   int64        `gorm:"uniqueIndex:idx_grant_subject_object;not null" json:"subjectId"`
	ObjectType  string       `gorm:"uniqueIndex:idx_grant_subject_object;size:16;not null;default:role" json:"objectType"` // role|role_group
	ObjectID    int64        `gorm:"uniqueIndex:idx_grant_subject_object;not null" json:"objectId"`
	Scopes      []GrantScope `gorm:"foreignKey:GrantID" json:"-"`
	CreatedAt   time.Time    `json:"createdAt"`
}

// GrantScope 是 grants 的生效范围,`*` 表示全部集群 / 全部命名空间。
type GrantScope struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	GrantID   int64  `gorm:"index;not null" json:"grantId"`
	Cluster   string `gorm:"size:128;not null;default:*" json:"cluster"`
	Namespace string `gorm:"size:128;not null;default:*" json:"namespace"`
}

// UserGroup 是 user_groups 表实体。
type UserGroup struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Description string    `gorm:"size:256" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// UserGroupMember 是 user_group_members 表实体,(group_id, user_id) 联合唯一。
type UserGroupMember struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	GroupID   int64     `gorm:"uniqueIndex:idx_group_user;not null" json:"groupId"`
	UserID    int64     `gorm:"uniqueIndex:idx_group_user;not null" json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

// EnrollToken 是 enroll_tokens 表实体,供 Agent 反连接入使用(V1)。
type EnrollToken struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	Cluster   string `gorm:"index;size:128;not null" json:"cluster"`
	TokenHash string `gorm:"uniqueIndex;size:64;not null" json:"-"`
	// NoExpiry 长期 token:永不过期,ExpiresAt 仅为展示兜底的哨兵远期时间。
	NoExpiry  bool       `gorm:"not null;default:false" json:"noExpiry"`
	ExpiresAt time.Time  `gorm:"not null" json:"expiresAt"`
	Revoked   bool       `gorm:"not null;default:false" json:"revoked"`
	UsedAt    *time.Time `json:"usedAt,omitempty"` // 一次性消费标记(注册成功后写入)
	CreatedAt time.Time  `json:"createdAt"`
}

// IssuedKubeconfig 是 issued_kubeconfigs 表实体,只存签发 token 的 SHA-256 哈希。
type IssuedKubeconfig struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	UserID      int64     `gorm:"index;not null" json:"userId"`
	Cluster     string    `gorm:"index;size:128;not null" json:"cluster"`
	TokenHash   string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Description string    `gorm:"size:256" json:"description"`
	ExpiresAt   time.Time `gorm:"not null" json:"expiresAt"`
	Revoked     bool      `gorm:"not null;default:false" json:"revoked"`
	CreatedAt   time.Time `json:"createdAt"`
}
