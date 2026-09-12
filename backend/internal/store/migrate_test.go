package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMigrateLegacyRoleBindings 验证旧 role_bindings(+ role_scopes)表在启动时
// 迁移为 grants/grant_scopes(object_type=role,含 scope),旧表随后被 DROP;
// 再次 Open 验证幂等。
func TestMigrateLegacyRoleBindings(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")

	// 用原始 SQL 模拟旧版本数据库的 role_bindings / role_scopes。
	legacy, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	stmts := []string{
		`CREATE TABLE role_bindings (
			id integer PRIMARY KEY AUTOINCREMENT,
			role_id integer NOT NULL,
			subject_type text NOT NULL DEFAULT 'user',
			subject_id integer NOT NULL,
			created_at datetime)`,
		`CREATE TABLE role_scopes (
			id integer PRIMARY KEY AUTOINCREMENT,
			binding_id integer NOT NULL,
			cluster text NOT NULL DEFAULT '*',
			namespace text NOT NULL DEFAULT '*')`,
		`INSERT INTO role_bindings (role_id, subject_type, subject_id, created_at)
			VALUES (7, 'user', 3, CURRENT_TIMESTAMP)`,
		`INSERT INTO role_bindings (role_id, subject_type, subject_id, created_at)
			VALUES (8, 'group', 4, CURRENT_TIMESTAMP)`,
		`INSERT INTO role_scopes (binding_id, cluster, namespace) VALUES (1, 'local', 'default')`,
		`INSERT INTO role_scopes (binding_id, cluster, namespace) VALUES (2, '*', '*')`,
	}
	for _, s := range stmts {
		if err := legacy.Exec(s).Error; err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	sqlDB, err := legacy.DB()
	if err != nil {
		t.Fatalf("legacy sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	// Open 触发迁移。
	db, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open with migration: %v", err)
	}
	if db.Migrator().HasTable("role_bindings") {
		t.Fatal("legacy role_bindings table should be dropped")
	}
	if db.Migrator().HasTable("role_scopes") {
		t.Fatal("legacy role_scopes table should be dropped")
	}
	var grants []Grant
	if err := db.WithContext(context.Background()).Preload("Scopes").Order("id").Find(&grants).Error; err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("grants = %d, want 2", len(grants))
	}
	want := []struct {
		subjectType string
		subjectID   int64
		objectID    int64
		cluster     string
		namespace   string
	}{
		{"user", 3, 7, "local", "default"},
		{"group", 4, 8, "*", "*"},
	}
	for i, w := range want {
		g := grants[i]
		if g.SubjectType != w.subjectType || g.SubjectID != w.subjectID ||
			g.ObjectType != "role" || g.ObjectID != w.objectID {
			t.Fatalf("grant[%d] = %+v, want subject=%s/%d object=role/%d",
				i, g, w.subjectType, w.subjectID, w.objectID)
		}
		if len(g.Scopes) != 1 || g.Scopes[0].Cluster != w.cluster || g.Scopes[0].Namespace != w.namespace {
			t.Fatalf("grant[%d] scopes = %+v, want %s/%s", i, g.Scopes, w.cluster, w.namespace)
		}
	}

	// 幂等:再次 Open,不报错、不重复转换。
	if _, err := Open("sqlite", dsn); err != nil {
		t.Fatalf("reopen (idempotency): %v", err)
	}
	var count int64
	if err := db.Model(&Grant{}).Count(&count).Error; err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if count != 2 {
		t.Fatalf("grants after reopen = %d, want 2", count)
	}
}

// TestOpenFreshDatabase 新库启动无旧表,迁移直接跳过。
func TestOpenFreshDatabase(t *testing.T) {
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open fresh db: %v", err)
	}
	if db.Migrator().HasTable("role_bindings") {
		t.Fatal("fresh db should not have role_bindings")
	}
	for _, tbl := range []string{"role_groups", "role_group_roles", "grants", "grant_scopes"} {
		if !db.Migrator().HasTable(tbl) {
			t.Fatalf("fresh db should have table %s", tbl)
		}
	}
}
