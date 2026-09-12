package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
)

// Identity 是认证通过后的用户身份信息。
type Identity struct {
	UserID   int64
	Username string
	Role     string
}

// permChecker 按角色返回权限集,由 main 装配时注入 service.PermissionsFor,
// 避免 middleware 反向依赖 service 包。
var permChecker func(role string) []string

// SetPermChecker 注入角色→权限集解析函数。
func SetPermChecker(f func(role string) []string) { permChecker = f }

// HasPerm 判断身份是否持有指定权限点(admin 通配全部权限)。
func HasPerm(ident Identity, perm string) bool {
	if permChecker == nil {
		return ident.Role == "admin"
	}
	for _, p := range permChecker(ident.Role) {
		if p == "*" || p == perm {
			return true
		}
	}
	return false
}

// authorizer 基于角色绑定解析带 scope 的权限,由 main 装配时注入
// service.RoleService.Authorize,避免 middleware 反向依赖 service 包。
// 未注入时 HasPermScope/RequirePermScope 回退静态角色映射(HasPerm)。
var authorizer func(userID int64, role, perm, cluster, namespace string) bool

// SetAuthorizer 注入带 scope 的授权解析函数。
func SetAuthorizer(f func(userID int64, role, perm, cluster, namespace string) bool) {
	authorizer = f
}

// HasPermScope 判断身份在 (cluster, namespace) 范围内是否持有指定权限点。
// 已注入 authorizer 时经角色绑定动态解析;否则回退静态角色映射(旧测试兼容)。
func HasPermScope(ident Identity, perm, cluster, namespace string) bool {
	if authorizer != nil {
		return authorizer(ident.UserID, ident.Role, perm, cluster, namespace)
	}
	return HasPerm(ident, perm)
}

// RequirePerm 要求当前身份持有指定权限点,否则 40300。
// 必须挂载在 Auth 之后(Auth 负责注入 Identity)。
func RequirePerm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := IdentityOf(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{
				Code: errcode.Unauthorized, Message: "authentication required",
			})
			return
		}
		if !HasPerm(ident, perm) {
			c.AbortWithStatusJSON(http.StatusForbidden, response.Body{
				Code: errcode.Forbidden, Message: "insufficient privilege: " + perm,
			})
			return
		}
		c.Next()
	}
}

// RequirePermScope 要求当前身份在 (:cluster, ?namespace=) 范围内持有指定权限点,
// 否则 40300。授权经 SetAuthorizer 注入的动态 authorizer 解析;
// 未注入时回退静态角色映射。必须挂载在 Auth 之后。
func RequirePermScope(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := IdentityOf(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{
				Code: errcode.Unauthorized, Message: "authentication required",
			})
			return
		}
		if !HasPermScope(ident, perm, c.Param("cluster"), c.Query("namespace")) {
			c.AbortWithStatusJSON(http.StatusForbidden, response.Body{
				Code: errcode.Forbidden, Message: "insufficient privilege: " + perm,
			})
			return
		}
		c.Next()
	}
}

// TokenVerifier 校验 access token 并返回用户身份,由 service.AuthService 实现。
type TokenVerifier interface {
	VerifyAccessToken(ctx context.Context, token string) (Identity, error)
}

// Auth 校验 Bearer JWT,通过后将身份写入 gin context。
// verifier 参数为最小接口,避免中间件依赖完整 service。
func Auth(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{
				Code: errcode.Unauthorized, Message: "missing bearer token",
			})
			return
		}
		ident, err := verifier.VerifyAccessToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{
				Code: errcode.Unauthorized, Message: "invalid or expired token",
			})
			return
		}
		c.Set("identity", ident)
		c.Next()
	}
}

// AdminOnly 要求当前用户角色为 admin,否则返回 40300。
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := IdentityOf(c)
		if !ok || ident.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, response.Body{
				Code: errcode.Forbidden, Message: "admin privilege required",
			})
			return
		}
		c.Next()
	}
}

// IdentityOf 从 gin context 取出认证身份。
func IdentityOf(c *gin.Context) (Identity, bool) {
	v, ok := c.Get("identity")
	if !ok {
		return Identity{}, false
	}
	ident, ok := v.(Identity)
	return ident, ok
}
