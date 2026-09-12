package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
)

// loginRequest 是登录请求体。
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// refreshTokenRequest 是刷新/登出请求体。
type refreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// POST /api/v1/auth/login
func login(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "username and password are required"))
			return
		}
		pair, err := d.Auth.Login(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			response.Fail(c, err)
			return
		}
		logx.Info(c.Request.Context(), "user logged in", "username", req.Username)
		response.OK(c, pair)
	}
}

// POST /api/v1/auth/refresh
func refresh(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshTokenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "refreshToken is required"))
			return
		}
		pair, err := d.Auth.Refresh(c.Request.Context(), req.RefreshToken)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, pair)
	}
}

// POST /api/v1/auth/logout
func logout(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshTokenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "refreshToken is required"))
			return
		}
		if err := d.Auth.Revoke(c.Request.Context(), req.RefreshToken); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}
