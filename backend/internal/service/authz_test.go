package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// newRBACFixture 构造 RBAC 授权解析测试夹具:
// 内置角色种子 + 用户 alice(operator)/bob(viewer)/carol(viewer)/dave(admin)
// + 用户组 devs(carol 成员)+ 自定义角色 writer(resources:write)/
// reader(logs:read)/allfix(* 全部)+ 角色组 bundle(allfix + reader)。
type rbacFixture struct {
	roles  *RoleService
	users  *store.UserRepo
	groups *store.UserGroupRepo
	roleR  *store.RoleRepo

	writerRoleID int64 // 自定义角色:仅 resources:write
	readerRoleID int64 // 自定义角色:仅 logs:read
	allRoleID    int64 // 自定义角色:仅 "*"
	bundleGrpID  int64 // 角色组 bundle:allfix + reader
	groupID      int64 // 用户组 devs
	aliceID      int64 // operator
	bobID        int64 // viewer,无任何 grant
	carolID      int64 // viewer,加入 devs 组
	daveID       int64 // admin
}

func newRBACFixture(t *testing.T) *rbacFixture {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if err := store.SeedDefaults(ctx, db, BuiltinRoleSeeds()); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	users := store.NewUserRepo(db)
	roleRepo := store.NewRoleRepo(db)
	groupRepo := store.NewUserGroupRepo(db)
	roleGroupRepo := store.NewRoleGroupRepo(db)
	grantRepo := store.NewGrantRepo(db)
	svc := NewRoleService(roleRepo, groupRepo, users, roleGroupRepo, grantRepo)

	f := &rbacFixture{roles: svc, users: users, groups: groupRepo, roleR: roleRepo}

	for i, u := range []struct{ name, role string }{
		{"alice", model.RoleOperator},
		{"bob", model.RoleViewer},
		{"carol", model.RoleViewer},
		{"dave", model.RoleAdmin},
	} {
		hash, err := HashPassword(fmt.Sprintf("pwd-%d", i))
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if err := users.CreateUser(ctx, &store.User{Username: u.name, PasswordHash: hash, Role: u.role}); err != nil {
			t.Fatalf("create user %s: %v", u.name, err)
		}
	}
	alice, err := users.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}
	bob, err := users.GetUserByUsername(ctx, "bob")
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	carol, err := users.GetUserByUsername(ctx, "carol")
	if err != nil {
		t.Fatalf("get carol: %v", err)
	}
	dave, err := users.GetUserByUsername(ctx, "dave")
	if err != nil {
		t.Fatalf("get dave: %v", err)
	}
	f.aliceID, f.bobID, f.carolID, f.daveID = alice.ID, bob.ID, carol.ID, dave.ID

	g, err := svc.CreateUserGroup(ctx, "devs", "dev team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	f.groupID = g.ID
	if err := svc.AddGroupMember(ctx, g.ID, carol.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	writer, err := svc.CreateRole(ctx, "writer", "write only", []string{PermResourcesWrite})
	if err != nil {
		t.Fatalf("create writer role: %v", err)
	}
	f.writerRoleID = writer.ID
	reader, err := svc.CreateRole(ctx, "reader", "read logs", []string{PermLogsRead})
	if err != nil {
		t.Fatalf("create reader role: %v", err)
	}
	f.readerRoleID = reader.ID
	all, err := svc.CreateRole(ctx, "allfix", "wildcard", []string{PermAll})
	if err != nil {
		t.Fatalf("create allfix role: %v", err)
	}
	f.allRoleID = all.ID

	// 角色组 bundle:allfix + reader(供 role_group 展开并集测试)。
	bundle, err := svc.CreateRoleGroup(ctx, "bundle", "all + logs")
	if err != nil {
		t.Fatalf("create role group: %v", err)
	}
	f.bundleGrpID = bundle.ID
	for _, id := range []int64{f.allRoleID, f.readerRoleID} {
		if err := svc.AddRoleGroupRole(ctx, bundle.ID, id); err != nil {
			t.Fatalf("add role %d to role group: %v", id, err)
		}
	}

	return f
}

func sortedEqual(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		t.Fatalf("perms = %v, want %v", got, want)
	}
	for i := range g {
		if g[i] != w[i] {
			t.Fatalf("perms = %v, want %v", got, want)
		}
	}
}

// codeOf 提取业务错误码;非 errcode.Error 视为测试失败。
func codeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("expected errcode.Error, got %T: %v", err, err)
	}
	return ec.Code
}

// TestEffectivePermissions 表驱动:直接 grant(role 对象)/ 组 grant(role_group
// 对象展开并集)/ scope 过滤 / 通配角色 / admin 恒通配 / 无 grant 回退内置角色。
func TestEffectivePermissions(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	// bob 直接 grant writer 角色,scope 集群=local、ns=default。
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.bobID, model.ObjectTypeRole, f.writerRoleID,
		[]model.Scope{{Cluster: "local", Namespace: "default"}}); err != nil {
		t.Fatalf("grant writer to bob: %v", err)
	}
	// dev 组 grant bundle 角色组(allfix + reader 展开),scope 集群=prod、ns=*。
	if _, err := f.roles.CreateGrant(ctx, model.SubjectGroup, f.groupID, model.ObjectTypeRoleGroup, f.bundleGrpID,
		[]model.Scope{{Cluster: "prod", Namespace: "*"}}); err != nil {
		t.Fatalf("grant bundle to group: %v", err)
	}

	tests := []struct {
		name      string
		userID    int64
		builtRole string
		cluster   string
		namespace string
		want      []string
	}{
		{
			name: "admin 恒通配(带 scope 查询同样)", userID: f.daveID, builtRole: model.RoleAdmin,
			cluster: "any", namespace: "any", want: []string{PermAll},
		},
		{
			name: "无 grant 回退内置 operator(全局)", userID: f.aliceID, builtRole: model.RoleOperator,
			want: operatorPermissions,
		},
		{
			name: "直接 grant 命中 scope:内置 ∪ resources:write", userID: f.bobID, builtRole: model.RoleViewer,
			cluster: "local", namespace: "default",
			want: append(append([]string(nil), viewerPermissions...), PermResourcesWrite),
		},
		{
			name: "直接 grant 集群不匹配:仅内置", userID: f.bobID, builtRole: model.RoleViewer,
			cluster: "other", namespace: "default", want: viewerPermissions,
		},
		{
			name: "直接 grant ns 不匹配:仅内置", userID: f.bobID, builtRole: model.RoleViewer,
			cluster: "local", namespace: "kube-system", want: viewerPermissions,
		},
		{
			// role_group 展开并集:allfix(*)+ reader(logs:read)。
			name: "组 grant role_group 展开:全局查询聚合并集", userID: f.carolID, builtRole: model.RoleViewer,
			want: append(append([]string(nil), viewerPermissions...), PermAll),
		},
		{
			name: "组 grant scope 集群=prod ns=*:命中并集", userID: f.carolID, builtRole: model.RoleViewer,
			cluster: "prod", namespace: "anything",
			want: append(append([]string(nil), viewerPermissions...), PermAll),
		},
		{
			name: "组 grant 集群不匹配:仅内置", userID: f.carolID, builtRole: model.RoleViewer,
			cluster: "local", namespace: "default", want: viewerPermissions,
		},
		{
			name: "全局查询聚合直接 grant(writer)", userID: f.bobID, builtRole: model.RoleViewer,
			want: append(append([]string(nil), viewerPermissions...), PermResourcesWrite),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms, err := f.roles.EffectivePermissions(ctx, tt.userID, tt.builtRole, tt.cluster, tt.namespace)
			if err != nil {
				t.Fatalf("EffectivePermissions: %v", err)
			}
			sortedEqual(t, perms, tt.want)
		})
	}
}

// TestRoleGroupExpansionUnion 专测 role_group 展开并集:
// grant 指向包含 writer + reader 的角色组,组内角色(bob 的 viewer 内置不含
// resources:write)的权限点应全部生效,移出角色后收缩。
func TestRoleGroupExpansionUnion(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	bundle, err := f.roles.CreateRoleGroup(ctx, "writers", "write + logs")
	if err != nil {
		t.Fatalf("create role group: %v", err)
	}
	for _, id := range []int64{f.writerRoleID, f.readerRoleID} {
		if err := f.roles.AddRoleGroupRole(ctx, bundle.ID, id); err != nil {
			t.Fatalf("add role to group: %v", err)
		}
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.bobID, model.ObjectTypeRoleGroup, bundle.ID,
		[]model.Scope{{Cluster: "local", Namespace: "*"}}); err != nil {
		t.Fatalf("grant role group: %v", err)
	}

	// 命中 scope:viewer 内置 ∪ writer ∪ reader。
	perms, err := f.roles.EffectivePermissions(ctx, f.bobID, model.RoleViewer, "local", "default")
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	set := map[string]bool{}
	for _, p := range perms {
		set[p] = true
	}
	for _, p := range append(append([]string(nil), viewerPermissions...), PermResourcesWrite) {
		if !set[p] {
			t.Fatalf("role_group expansion missing perm %q in %v", p, perms)
		}
	}
	// scope 不命中:仅内置。
	perms, err = f.roles.EffectivePermissions(ctx, f.bobID, model.RoleViewer, "other", "default")
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	sortedEqual(t, perms, viewerPermissions)

	// 组内移除角色后展开收缩。
	if err := f.roles.RemoveRoleGroupRole(ctx, bundle.ID, f.writerRoleID); err != nil {
		t.Fatalf("remove role from group: %v", err)
	}
	perms, err = f.roles.EffectivePermissions(ctx, f.bobID, model.RoleViewer, "local", "default")
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	for _, p := range perms {
		if p == PermResourcesWrite {
			t.Fatalf("writer perm should be gone after removing role from group: %v", perms)
		}
	}
}

// TestAuthorizeWildcardCoversPerm 验证 Authorize 的 "*" 通配与不匹配路径。
func TestAuthorizeWildcardCoversPerm(t *testing.T) {
	f := newRBACFixture(t)
	// 组 grant bundle(含 allfix(*)),scope 集群=prod、ns=*。
	if _, err := f.roles.CreateGrant(context.Background(), model.SubjectGroup, f.groupID,
		model.ObjectTypeRoleGroup, f.bundleGrpID, []model.Scope{{Cluster: "prod", Namespace: "*"}}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !f.roles.Authorize(f.carolID, model.RoleViewer, PermResourcesWrite, "prod", "default") {
		t.Fatal("wildcard role_group grant should authorize any perm in scoped cluster")
	}
	if f.roles.Authorize(f.carolID, model.RoleViewer, PermResourcesWrite, "dev", "default") {
		t.Fatal("should deny outside scoped cluster")
	}
	// 直接 grant writer(仅 resources:write),scope 集群=local、ns=default。
	if _, err := f.roles.CreateGrant(context.Background(), model.SubjectUser, f.aliceID,
		model.ObjectTypeRole, f.writerRoleID, []model.Scope{{Cluster: "local", Namespace: "default"}}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !f.roles.Authorize(f.aliceID, model.RoleOperator, PermTerminalUse, "local", "default") {
		t.Fatal("operator builtin should include terminal:use within any scope")
	}
	if f.roles.Authorize(f.bobID, model.RoleViewer, PermResourcesWrite, "local", "kube-system") {
		t.Fatal("should deny write outside scoped namespace")
	}
}

// TestCreateGrantValidation 授权创建的参数与存在性校验(含重复 grant 40406)。
func TestCreateGrantValidation(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	valid := []model.Scope{{Cluster: "*", Namespace: "*"}}

	if _, err := f.roles.CreateGrant(ctx, "robot", f.aliceID, model.ObjectTypeRole, f.writerRoleID, valid); codeOf(t, err) != errcode.ParamInvalid {
		t.Fatalf("invalid subjectType should be 40001, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.aliceID, "role_group_group", f.writerRoleID, valid); codeOf(t, err) != errcode.ParamInvalid {
		t.Fatalf("invalid objectType should be 40001, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.aliceID, model.ObjectTypeRole, f.writerRoleID, nil); codeOf(t, err) != errcode.ParamInvalid {
		t.Fatalf("empty scopes should be 40001, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.aliceID, model.ObjectTypeRole, f.writerRoleID,
		[]model.Scope{{Cluster: "*", Namespace: "default"}}); codeOf(t, err) != errcode.ParamInvalid {
		t.Fatalf(`cluster=* with ns=default should be 40001, got %v`, err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, 424242, model.ObjectTypeRole, f.writerRoleID, valid); codeOf(t, err) != errcode.UserNotFound {
		t.Fatalf("missing subject user should be 40410, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectGroup, 424242, model.ObjectTypeRole, f.writerRoleID, valid); codeOf(t, err) != errcode.UserGroupNotFound {
		t.Fatalf("missing subject group should be 40416, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.aliceID, model.ObjectTypeRole, 999999, valid); codeOf(t, err) != errcode.RoleNotFound {
		t.Fatalf("missing role should be 40414, got %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectUser, f.aliceID, model.ObjectTypeRoleGroup, 999999, valid); codeOf(t, err) != errcode.NotFound {
		t.Fatalf("missing role group should be 40400, got %v", err)
	}

	// 重复 grant(四元组相同)→ 40406。
	if _, err := f.roles.CreateGrant(ctx, model.SubjectGroup, f.groupID, model.ObjectTypeRoleGroup, f.bundleGrpID, valid); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if _, err := f.roles.CreateGrant(ctx, model.SubjectGroup, f.groupID, model.ObjectTypeRoleGroup, f.bundleGrpID, valid); codeOf(t, err) != errcode.ResourceConflict {
		t.Fatalf("duplicate grant should be 40406, got %v", err)
	}
}
