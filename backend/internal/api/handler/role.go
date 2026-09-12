package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/api/response"
	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
)

// roleRequest 是创建/更新角色请求体。
type roleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// userGroupRequest 是创建/更新用户组请求体。
type userGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// groupMemberRequest 是添加用户组成员请求体。
type groupMemberRequest struct {
	UserID int64 `json:"userId" binding:"required"`
}

// roleGroupRequest 是创建/更新角色组请求体。
type roleGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// roleGroupRoleRequest 是向角色组添加角色请求体。
type roleGroupRoleRequest struct {
	RoleID int64 `json:"roleId" binding:"required"`
}

// grantRequest 是创建授权请求体。
type grantRequest struct {
	SubjectType string        `json:"subjectType" binding:"required"`
	SubjectID   int64         `json:"subjectId" binding:"required"`
	ObjectType  string        `json:"objectType" binding:"required"`
	ObjectID    int64         `json:"objectId" binding:"required"`
	Scopes      []model.Scope `json:"scopes"`
}

// parseID 解析路径参数 :id,非法返回 40001。
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid id"))
		return 0, false
	}
	return id, true
}

// GET /api/v1/roles(管理员)— 角色列表(裸数组)。
func listRoles(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Roles.ListRoles(c.Request.Context())
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/roles(管理员)— 创建自定义角色。
func createRole(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req roleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name and permissions are required"))
			return
		}
		item, err := d.Roles.CreateRole(c.Request.Context(), req.Name, req.Description, req.Permissions)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// PUT /api/v1/roles/:id(管理员)— 更新角色(builtin 返回 40300)。
func updateRole(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req roleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name and permissions are required"))
			return
		}
		item, err := d.Roles.UpdateRole(c.Request.Context(), id, req.Name, req.Description, req.Permissions)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// DELETE /api/v1/roles/:id(管理员)— 删除自定义角色(builtin 返回 40300)。
func deleteRole(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		if err := d.Roles.DeleteRole(c.Request.Context(), id); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/user-groups(管理员)— 用户组列表(裸数组)。
func listUserGroups(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Roles.ListUserGroups(c.Request.Context())
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/user-groups(管理员)— 创建用户组。
func createUserGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req userGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name is required"))
			return
		}
		item, err := d.Roles.CreateUserGroup(c.Request.Context(), req.Name, req.Description)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// PUT /api/v1/user-groups/:id(管理员)— 更新用户组。
func updateUserGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req userGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name is required"))
			return
		}
		item, err := d.Roles.UpdateUserGroup(c.Request.Context(), id, req.Name, req.Description)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// DELETE /api/v1/user-groups/:id(管理员)— 删除用户组。
func deleteUserGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		if err := d.Roles.DeleteUserGroup(c.Request.Context(), id); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/user-groups/:id/members(管理员)— 成员用户列表(UserItem[])。
func listGroupMembers(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		items, err := d.Roles.ListGroupMembers(c.Request.Context(), id)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/user-groups/:id/members(管理员)— 添加成员 {userId}。
func addGroupMember(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req groupMemberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "userId is required"))
			return
		}
		if err := d.Roles.AddGroupMember(c.Request.Context(), id, req.UserID); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// DELETE /api/v1/user-groups/:id/members/:userId(管理员)— 移除成员。
func removeGroupMember(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		userID, err := strconv.ParseInt(c.Param("userId"), 10, 64)
		if err != nil || userID <= 0 {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid userId"))
			return
		}
		if err := d.Roles.RemoveGroupMember(c.Request.Context(), id, userID); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/role-groups(管理员)— 角色组列表(裸数组)。
func listRoleGroups(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Roles.ListRoleGroups(c.Request.Context())
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/role-groups(管理员)— 创建角色组。
func createRoleGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req roleGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name is required"))
			return
		}
		item, err := d.Roles.CreateRoleGroup(c.Request.Context(), req.Name, req.Description)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// PUT /api/v1/role-groups/:id(管理员)— 更新角色组。
func updateRoleGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req roleGroupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name is required"))
			return
		}
		item, err := d.Roles.UpdateRoleGroup(c.Request.Context(), id, req.Name, req.Description)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// DELETE /api/v1/role-groups/:id(管理员)— 删除角色组。
func deleteRoleGroup(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		if err := d.Roles.DeleteRoleGroup(c.Request.Context(), id); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/role-groups/:id/roles(管理员)— 角色组内角色(RoleItem[],裸数组)。
func listRoleGroupRoles(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		items, err := d.Roles.ListRoleGroupRoles(c.Request.Context(), id)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/role-groups/:id/roles(管理员)— 添加角色 {roleId}。
func addRoleGroupRole(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req roleGroupRoleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "roleId is required"))
			return
		}
		if err := d.Roles.AddRoleGroupRole(c.Request.Context(), id, req.RoleID); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// DELETE /api/v1/role-groups/:id/roles/:roleId(管理员)— 移除角色。
func removeRoleGroupRole(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		roleID, err := strconv.ParseInt(c.Param("roleId"), 10, 64)
		if err != nil || roleID <= 0 {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid roleId"))
			return
		}
		if err := d.Roles.RemoveRoleGroupRole(c.Request.Context(), id, roleID); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/grants(管理员)— 授权列表(裸数组)。subjectType/subjectId/
// objectType/objectId 四个过滤参数均可选、可组合:按主体查 = 该用户/组的全部授权,
// 按对象查 = 该角色/角色组被授给了谁。
func listGrants(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		subjectID, err := parseOptionalQueryInt(c, "subjectId")
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid subjectId"))
			return
		}
		objectID, err := parseOptionalQueryInt(c, "objectId")
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid objectId"))
			return
		}
		items, err := d.Roles.ListGrants(c.Request.Context(),
			c.Query("subjectType"), subjectID, c.Query("objectType"), objectID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, items)
	}
}

// POST /api/v1/grants(管理员)— 创建授权。
func createGrant(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req grantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid,
				"subjectType, subjectId, objectType, objectId and scopes are required"))
			return
		}
		item, err := d.Roles.CreateGrant(c.Request.Context(),
			req.SubjectType, req.SubjectID, req.ObjectType, req.ObjectID, req.Scopes)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, item)
	}
}

// DELETE /api/v1/grants/:id(管理员)— 删除授权。
func deleteGrant(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		if err := d.Roles.DeleteGrant(c.Request.Context(), id); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// parseOptionalQueryInt 解析可选的整数查询参数;缺省或为空返回 0(不过滤)。
func parseOptionalQueryInt(c *gin.Context, key string) (int64, error) {
	raw := c.Query(key)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid %s %q", key, raw)
	}
	return v, nil
}

// GET /api/v1/users/:username/effective-permissions(管理员)—
// 有效权限(含用户组聚合);?cluster=&namespace= 可省略(全局)。
func effectivePermissions(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 本人可查自己的有效权限(前端按钮置灰依赖);查他人需 admin。
		ident, hasIdent := middleware.IdentityOf(c)
		username := c.Param("username")
		if !hasIdent || (ident.Username != username && ident.Role != "admin") {
			c.JSON(http.StatusForbidden, gin.H{
				"code": errcode.Forbidden, "message": "只能查询本人或 admin 可查询他人", "data": nil,
			})
			return
		}
		u, err := d.Users.GetByUsername(c.Request.Context(), username)
		if err != nil {
			response.Fail(c, err)
			return
		}
		perms, err := d.Roles.EffectivePermissions(c.Request.Context(),
			u.ID, u.Role, c.Query("cluster"), c.Query("namespace"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, model.EffectivePermissions{Permissions: perms})
	}
}
