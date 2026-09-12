package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/middleware"
	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/crypto"
	"github.com/v911/backend/internal/service"
	"github.com/v911/backend/internal/store"
)

// newRBACTestRouter 构造带完整 RBAC 依赖的路由:注入 permChecker 与 authorizer
// (测试结束恢复,保证未注入回退路径的既有测试不受影响)。
func newRBACTestRouter(t *testing.T) (*gin.Engine, *service.RoleService, *store.UserRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.SeedDefaults(context.Background(), db, service.BuiltinRoleSeeds()); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	cipher, err := crypto.NewCipher(testMasterKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	userRepo := store.NewUserRepo(db)
	hash, err := service.HashPassword("admin123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := userRepo.CreateUser(context.Background(), &store.User{
		Username: "admin", PasswordHash: hash, Role: "admin",
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	authSvc := service.NewAuthService(userRepo, store.NewRefreshTokenRepo(db), "test-secret")
	auditRepo := store.NewAuditRepo(db)
	clusterSvc := service.NewClusterRegService(
		store.NewClusterRepo(db), cipher, service.NewMemRegistry())
	roleSvc := service.NewRoleService(store.NewRoleRepo(db), store.NewUserGroupRepo(db), userRepo,
		store.NewRoleGroupRepo(db), store.NewGrantRepo(db))

	middleware.SetPermChecker(service.PermissionsFor)
	middleware.SetAuthorizer(roleSvc.Authorize)
	t.Cleanup(func() {
		middleware.SetPermChecker(nil)
		middleware.SetAuthorizer(nil)
	})

	manager := k8s.NewManager(k8s.Options{})
	clusterSvc.SetRuntimeHook(manager)
	r := Routes(Deps{
		Auth:      authSvc,
		Users:     service.NewUserService(userRepo, authSvc),
		Roles:     roleSvc,
		Audit:     service.NewAuditService(auditRepo),
		Clusters:  clusterSvc,
		Manager:   manager,
		Resources: service.NewResourceService(manager),
	})
	return r, roleSvc, userRepo
}

// fakeK8sServer 模拟 K8s API Server:/version 探活 + default ns Pod 创建。
func fakeK8sServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"gitVersion": "v1.29.0"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/namespaces/default/pods":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Pod",
				"metadata":{"name":"web","namespace":"default","uid":"u-1"},
				"spec":{"containers":[{"name":"c","image":"nginx"}]},
				"status":{"phase":"Running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func bareArrayOf(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var raw []map[string]any
	if err := json.Unmarshal(stripData(t, w), &raw); err != nil {
		t.Fatalf("data is not a bare array: %s", w.Body.String())
	}
	return raw
}

// stripData 取出响应 data 字段的原样 JSON。
func stripData(t *testing.T, w *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	var b response.Body
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	raw, err := json.Marshal(b.Data)
	if err != nil {
		t.Fatalf("re-marshal data: %v", err)
	}
	return raw
}

func mapOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(stripData(t, w), &m); err != nil {
		t.Fatalf("data is not an object: %s", w.Body.String())
	}
	return m
}

func podManifest(name string) map[string]any {
	return map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name},
		"spec":     map[string]any{"containers": []any{map[string]any{"name": "c", "image": "nginx"}}},
	}
}

// TestRBACFullChain 全链路(中心授权表契约):建用户组 → 加成员 → 建角色 →
// 建角色组(挂角色)→ grant(subject=group,object=role_group,scope 集群/命名空间)
// → 成员在该 scope 写 200、范围外 403、role_group 展开含 resources:write →
// 按 subjectId / objectId 反向查 grants → 重复 grant 40406 → grant 删除后 403。
func TestRBACFullChain(t *testing.T) {
	r, _, userRepo := newRBACTestRouter(t)
	srv := fakeK8sServer(t)

	admin := loginAs(t, r, "admin", "admin123")

	// 注册集群 local(fake API Server,探活成功)。
	if w := doJSON(t, r, http.MethodPost, "/api/v1/clusters", admin, map[string]string{
		"name": "local", "kubeconfig": kubeconfig(srv.URL),
	}); w.Code != http.StatusOK {
		t.Fatalf("register cluster: %d %s", w.Code, w.Body.String())
	}

	// 1. 创建用户 alice / bob(viewer)。
	createViewer := func(name string) int64 {
		if w := doJSON(t, r, http.MethodPost, "/api/v1/users", admin, map[string]string{
			"username": name, "password": "viewer-pass-123", "role": "viewer",
		}); w.Code != http.StatusOK {
			t.Fatalf("create user %s: %d %s", name, w.Code, w.Body.String())
		}
		u, err := userRepo.GetUserByUsername(context.Background(), name)
		if err != nil {
			t.Fatalf("get user %s: %v", name, err)
		}
		return u.ID
	}
	aliceID := createViewer("alice")
	createViewer("bob")

	// 2. 建用户组并加成员。
	w := doJSON(t, r, http.MethodPost, "/api/v1/user-groups", admin,
		map[string]string{"name": "devs", "description": "dev team"})
	if w.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", w.Code, w.Body.String())
	}
	groupID := int64(mapOf(t, w)["id"].(float64))
	if w := doJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/user-groups/%d/members", groupID), admin,
		map[string]int64{"userId": aliceID}); w.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", w.Code, w.Body.String())
	}

	// 3. 建自定义角色(仅 resources:write)。
	w = doJSON(t, r, http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "ns-writer", "description": "write in scoped ns", "permissions": []string{"resources:write"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create role: %d %s", w.Code, w.Body.String())
	}
	roleID := int64(mapOf(t, w)["id"].(float64))

	// 4. 建角色组并挂角色;GET roles 返回 RoleItem[](裸数组)。
	w = doJSON(t, r, http.MethodPost, "/api/v1/role-groups", admin,
		map[string]string{"name": "writers", "description": "writers bundle"})
	if w.Code != http.StatusOK {
		t.Fatalf("create role group: %d %s", w.Code, w.Body.String())
	}
	rg := mapOf(t, w)
	roleGroupID := int64(rg["id"].(float64))
	if rg["name"].(string) != "writers" {
		t.Fatalf("role group name = %v", rg["name"])
	}
	if w := doJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/role-groups/%d/roles", roleGroupID), admin,
		map[string]int64{"roleId": roleID}); w.Code != http.StatusOK {
		t.Fatalf("add role to role group: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/v1/role-groups/%d/roles", roleGroupID), admin, nil)
	rgRoles := bareArrayOf(t, w)
	if len(rgRoles) != 1 || rgRoles[0]["name"].(string) != "ns-writer" {
		t.Fatalf("role group roles = %s", w.Body.String())
	}

	// 5. grant(subject=group,object=role_group,scope 集群=local、ns=default)。
	w = doJSON(t, r, http.MethodPost, "/api/v1/grants", admin, map[string]any{
		"subjectType": "group", "subjectId": groupID,
		"objectType": "role_group", "objectId": roleGroupID,
		"scopes": []model.Scope{{Cluster: "local", Namespace: "default"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create grant: %d %s", w.Code, w.Body.String())
	}
	grant := mapOf(t, w)
	grantID := int64(grant["id"].(float64))
	if grant["subjectName"].(string) != "devs" || grant["objectName"].(string) != "writers" {
		t.Fatalf("grant names = %v / %v", grant["subjectName"], grant["objectName"])
	}
	if grant["subjectType"].(string) != "group" || grant["objectType"].(string) != "role_group" {
		t.Fatalf("grant types = %v / %v", grant["subjectType"], grant["objectType"])
	}
	if scopes, ok := grant["scopes"].([]any); !ok || len(scopes) != 1 {
		t.Fatalf("grant scopes = %v", grant["scopes"])
	}

	// 6. alice 登录,对该 ns 写 200(role_group 展开含 resources:write)。
	alice := loginAs(t, r, "alice", "viewer-pass-123")
	w = doJSON(t, r, http.MethodPost, "/api/v1/clusters/local/pods?namespace=default", alice, podManifest("web"))
	if w.Code != http.StatusOK || decodeBody(t, w).Code != 0 {
		t.Fatalf("scoped write should be 200: %d %s", w.Code, w.Body.String())
	}

	// 7. 其他 ns 403 / 其他集群 403。
	w = doJSON(t, r, http.MethodPost, "/api/v1/clusters/local/pods?namespace=kube-system", alice, podManifest("x"))
	if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
		t.Fatalf("write in other ns should be 40300: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodPost, "/api/v1/clusters/other-cluster/pods?namespace=default", alice, podManifest("x"))
	if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
		t.Fatalf("write in other cluster should be 40300: %d %s", w.Code, w.Body.String())
	}

	// 8. 有效权限:全局(含 role_group 展开聚合)与按 scope。
	w = doJSON(t, r, http.MethodGet, "/api/v1/users/alice/effective-permissions", admin, nil)
	foundWrite := false
	for _, p := range mapOf(t, w)["permissions"].([]any) {
		if p.(string) == "resources:write" {
			foundWrite = true
		}
	}
	if !foundWrite {
		t.Fatalf("global effective permissions should aggregate role_group grant: %v", mapOf(t, w))
	}
	w = doJSON(t, r, http.MethodGet, "/api/v1/users/alice/effective-permissions?cluster=other&namespace=default", admin, nil)
	for _, p := range mapOf(t, w)["permissions"].([]any) {
		if p.(string) == "resources:write" {
			t.Fatal("scoped effective permissions should exclude out-of-scope grant")
		}
	}

	// 9. builtin 角色删改 403。
	for _, id := range []int64{1, 2, 3} {
		w = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/roles/%d", id), admin, nil)
		if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
			t.Fatalf("delete builtin role %d should be 40300: %d %s", id, w.Code, w.Body.String())
		}
		w = doJSON(t, r, http.MethodPut, fmt.Sprintf("/api/v1/roles/%d", id), admin,
			map[string]any{"name": "hijacked", "permissions": []string{"*"}})
		if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
			t.Fatalf("update builtin role %d should be 40300: %d %s", id, w.Code, w.Body.String())
		}
	}

	// 10. 回归:viewer 无 grant 写资源仍 403。
	bob := loginAs(t, r, "bob", "viewer-pass-123")
	w = doJSON(t, r, http.MethodPost, "/api/v1/clusters/local/pods?namespace=default", bob, podManifest("x"))
	if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
		t.Fatalf("viewer without grant should be 40300: %d %s", w.Code, w.Body.String())
	}

	// 11. 反向查询:按主体查(该组的全部授权)与按对象查(该角色组授给了谁)。
	w = doJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/grants?subjectType=group&subjectId=%d", groupID), admin, nil)
	bySubject := bareArrayOf(t, w)
	if len(bySubject) != 1 || int64(bySubject[0]["id"].(float64)) != grantID {
		t.Fatalf("grants by subject = %s", w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet,
		fmt.Sprintf("/api/v1/grants?objectType=role_group&objectId=%d", roleGroupID), admin, nil)
	byObject := bareArrayOf(t, w)
	if len(byObject) != 1 || byObject[0]["subjectName"].(string) != "devs" {
		t.Fatalf("grants by object = %s", w.Body.String())
	}

	// 12. 重复 grant 40406;cluster=* 时 ns 必须 * 40001;错误响应 data 为 null。
	w = doJSON(t, r, http.MethodPost, "/api/v1/grants", admin, map[string]any{
		"subjectType": "group", "subjectId": groupID,
		"objectType": "role_group", "objectId": roleGroupID,
		"scopes": []model.Scope{{Cluster: "local", Namespace: "default"}},
	})
	if w.Code != http.StatusConflict || decodeBody(t, w).Code != 40406 {
		t.Fatalf("duplicate grant should be 40406: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodPost, "/api/v1/grants", admin, map[string]any{
		"subjectType": "user", "subjectId": aliceID,
		"objectType": "role", "objectId": roleID,
		"scopes": []model.Scope{{Cluster: "*", Namespace: "default"}}})
	if decodeBody(t, w).Code != 40001 {
		t.Fatalf(`cluster=* with ns=default should be 40001: %s`, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatalf("error response data must be null: %s", w.Body.String())
	}

	// 13. 删除 grant 后 alice 写 403(授权即时失效)。
	if w := doJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/grants/%d", grantID), admin, nil); w.Code != http.StatusOK {
		t.Fatalf("delete grant: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodPost, "/api/v1/clusters/local/pods?namespace=default", alice, podManifest("web2"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("write after grant deletion should be 403: %d %s", w.Code, w.Body.String())
	}

	// 14. 角色列表为裸数组,包含 builtin + 自定义;角色组名重复 40413。
	w = doJSON(t, r, http.MethodGet, "/api/v1/roles", admin, nil)
	raw := bareArrayOf(t, w)
	if len(raw) != 4 { // admin/operator/viewer + ns-writer
		t.Fatalf("roles length = %d, want 4", len(raw))
	}
	w = doJSON(t, r, http.MethodPost, "/api/v1/role-groups", admin,
		map[string]string{"name": "writers", "description": "dup"})
	if decodeBody(t, w).Code != 40413 {
		t.Fatalf("duplicate role group name should be 40413: %s", w.Body.String())
	}
}

// TestRBACEndpointsRequireAdmin 非 admin 访问 RBAC 管理端点 40300。
func TestRBACEndpointsRequireAdmin(t *testing.T) {
	r, _, userRepo := newRBACTestRouter(t)
	hash, err := service.HashPassword("viewer-pass-123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := userRepo.CreateUser(context.Background(), &store.User{
		Username: "bob", PasswordHash: hash, Role: "viewer",
	}); err != nil {
		t.Fatalf("seed bob: %v", err)
	}
	bob := loginAs(t, r, "bob", "viewer-pass-123")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/roles"},
		{http.MethodPost, "/api/v1/roles"},
		{http.MethodGet, "/api/v1/user-groups"},
		{http.MethodGet, "/api/v1/role-groups"},
		{http.MethodPost, "/api/v1/role-groups"},
		{http.MethodGet, "/api/v1/grants"},
		{http.MethodPost, "/api/v1/grants"},
		{http.MethodDelete, "/api/v1/grants/1"},
		{http.MethodGet, "/api/v1/users/alice/effective-permissions"},
	} {
		w := doJSON(t, r, tc.method, tc.path, bob, map[string]any{})
		if w.Code != http.StatusForbidden || decodeBody(t, w).Code != 40300 {
			t.Fatalf("%s %s should be 40300: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
