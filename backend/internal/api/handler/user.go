package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/middleware"
	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/pagination"
)

// createUserRequest 是创建用户请求体。
type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role" binding:"required"`
}

// setUserStatusRequest 是启用/禁用用户请求体。
type setUserStatusRequest struct {
	Disabled bool `json:"disabled"`
}

// changePasswordRequest 是修改密码请求体。
type changePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

// GET /api/v1/users(管理员)
func listUsers(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := pagination.Parse(c.Request.URL.Query())
		if err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid pagination").WithCause(err))
			return
		}
		page, err := d.Users.ListUsers(c.Request.Context(), p)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, page)
	}
}

// POST /api/v1/users(管理员)
func createUser(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "username, password and role are required"))
			return
		}
		u, err := d.Users.CreateUser(c.Request.Context(), req.Username, req.Password, req.Role)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, u)
	}
}

// PUT /api/v1/users/:username/status(管理员)
func setUserStatus(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req setUserStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "invalid body"))
			return
		}
		if err := d.Users.DisableUser(c.Request.Context(), c.Param("username"), req.Disabled); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// PUT /api/v1/users/me/password
func changeMyPassword(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req changePasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "oldPassword and newPassword are required"))
			return
		}
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		if err := d.Users.ChangePassword(c.Request.Context(), ident.Username, req.OldPassword, req.NewPassword); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// GET /api/v1/users/me — 当前登录用户信息。
func getMe(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		u, err := d.Users.GetByUsername(c.Request.Context(), ident.Username)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, u)
	}
}

// GET /api/v1/users/me/permissions — 当前用户角色与权限列表。
func getMyPermissions(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		p, err := d.Users.Permissions(c.Request.Context(), ident.Username)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, p)
	}
}

// POST /api/v1/users/:username/reset-password(管理员)— 重置为一次性随机密码。
func resetUserPassword(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		res, err := d.Users.ResetPassword(c.Request.Context(), c.Param("username"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, res)
	}
}

// DELETE /api/v1/users/:username(管理员)— 删除用户(禁删自己与内置 admin)。
func deleteUser(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := middleware.IdentityOf(c)
		if !ok {
			response.Fail(c, errcode.New(errcode.Unauthorized, "authentication required"))
			return
		}
		if err := d.Users.DeleteUser(c.Request.Context(), ident.Username, c.Param("username")); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}
