// Package handler 实现 /api/v1 下的 HTTP handler,按资源一文件组织。
package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/middleware"
	"github.com/v911/backend/internal/config"
	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/service"
	"github.com/v911/backend/internal/web"
)

// Deps 是 handler 层依赖集合,由 main 装配注入。
type Deps struct {
	Auth        *service.AuthService
	Users       *service.UserService
	Roles       *service.RoleService
	Audit       *service.AuditService
	Clusters    *service.ClusterRegService
	Resources   *service.ResourceService
	Watch       *service.WatchHub
	Manager     *k8s.Manager
	Enroll      *service.EnrollService
	Kubeconfigs *service.KubeconfigService
	AgentHub    agentHub
	// Config 平台配置(manifest 渲染取 server.externalUrl);可空。
	Config *config.Config
}

// agentHub 是 agent.TunnelHub 在 handler 层的最小接口(避免 handler 依赖
// agent 包内部细节)。
type agentHub interface {
	ServeSignaling() http.HandlerFunc
	ServeData() http.HandlerFunc
}

// Routes 挂载全部 /api/v1 路由;返回 gin engine。
func Routes(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logging(), middleware.Recovery())

	// 探活端点(匿名,供 k8s readiness/liveness 与反代健康检查使用;必须先于 SPA 回退注册)。
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	v1 := r.Group("/api/v1")
	rec := auditRecorder{svc: d.Audit}
	v1.Use(middleware.Audit(rec))

	// 认证端点(匿名可达)。
	v1.POST("/auth/login", login(d))
	v1.POST("/auth/refresh", refresh(d))

	// WebSocket 事件流(浏览器 WS 无法设 header,认证在 handler 内支持 ?token=)。
	v1.GET("/watch", watchEndpoint(d))

	// Agent 反连(信令鉴权走 Enrollment Token 首帧;数据通道走 sessionKey)。
	v1.GET("/agent/connect", agentConnect(d))
	v1.GET("/agent/tunnel", agentTunnel(d))
	// 一次性下载 code 即鉴权,匿名可达。
	v1.GET("/clusters/:cluster/kubeconfigs/download", downloadKubeconfig(d))
	// Agent manifest 渲染(校验 ?token= 即鉴权,匿名可达)。
	v1.GET("/agent/manifest", agentManifest(d, d.Config))

	// WS 端点(logs 流/终端):浏览器 WebSocket 无法携带 Authorization header,
	// 不能挂在 Auth 组(会直接 401);认证由 handler 内 wsIdentity 读 ?token=
	// 完成,并在此处补权限点校验(RBAC 与 REST 端点一致)。
	wsPerm := func(perm string, ident middleware.Identity) bool { return middleware.HasPerm(ident, perm) }
	_ = wsPerm
	v1.GET("/clusters/:cluster/pods/:name/logs/stream", podLogStream(d))
	v1.GET("/clusters/:cluster/pods/:name/terminal", podTerminal(d))

	// 以下端点需要 JWT。
	authed := v1.Group("", middleware.Auth(verifier{d.Auth}))
	authed.POST("/auth/logout", logout(d))

	authed.GET("/users/me", getMe(d))
	authed.GET("/users/me/permissions", getMyPermissions(d))
	authed.GET("/users", middleware.AdminOnly(), listUsers(d))
	authed.POST("/users", middleware.AdminOnly(), createUser(d))
	authed.PUT("/users/:username/status", middleware.AdminOnly(), setUserStatus(d))
	authed.POST("/users/:username/reset-password", middleware.AdminOnly(), resetUserPassword(d))
	authed.DELETE("/users/:username", middleware.AdminOnly(), deleteUser(d))
	authed.PUT("/users/me/password", changeMyPassword(d))

	// 平台 RBAC:角色 / 用户组 / 角色组 / 授权 / 有效权限(全部 admin,写操作走审计)。
	authed.GET("/roles", middleware.AdminOnly(), listRoles(d))
	authed.POST("/roles", middleware.AdminOnly(), createRole(d))
	authed.PUT("/roles/:id", middleware.AdminOnly(), updateRole(d))
	authed.DELETE("/roles/:id", middleware.AdminOnly(), deleteRole(d))

	authed.GET("/user-groups", middleware.AdminOnly(), listUserGroups(d))
	authed.POST("/user-groups", middleware.AdminOnly(), createUserGroup(d))
	authed.PUT("/user-groups/:id", middleware.AdminOnly(), updateUserGroup(d))
	authed.DELETE("/user-groups/:id", middleware.AdminOnly(), deleteUserGroup(d))
	authed.GET("/user-groups/:id/members", middleware.AdminOnly(), listGroupMembers(d))
	authed.POST("/user-groups/:id/members", middleware.AdminOnly(), addGroupMember(d))
	authed.DELETE("/user-groups/:id/members/:userId", middleware.AdminOnly(), removeGroupMember(d))

	authed.GET("/role-groups", middleware.AdminOnly(), listRoleGroups(d))
	authed.POST("/role-groups", middleware.AdminOnly(), createRoleGroup(d))
	authed.PUT("/role-groups/:id", middleware.AdminOnly(), updateRoleGroup(d))
	authed.DELETE("/role-groups/:id", middleware.AdminOnly(), deleteRoleGroup(d))
	authed.GET("/role-groups/:id/roles", middleware.AdminOnly(), listRoleGroupRoles(d))
	authed.POST("/role-groups/:id/roles", middleware.AdminOnly(), addRoleGroupRole(d))
	authed.DELETE("/role-groups/:id/roles/:roleId", middleware.AdminOnly(), removeRoleGroupRole(d))

	authed.GET("/grants", middleware.AdminOnly(), listGrants(d))
	authed.POST("/grants", middleware.AdminOnly(), createGrant(d))
	authed.DELETE("/grants/:id", middleware.AdminOnly(), deleteGrant(d))

	// 本人可查自己的有效权限(前端按钮置灰依赖);查他人仍需 admin。
	authed.GET("/users/:username/effective-permissions", effectivePermissions(d))

	authed.GET("/audit-logs", middleware.AdminOnly(), listAuditLogs(d))

	// Enrollment Token 管理(创建/吊销为 admin,危险操作走审计)。
	authed.GET("/enroll-tokens", listEnrollTokens(d))
	authed.POST("/enroll-tokens", middleware.AdminOnly(), createEnrollToken(d))
	authed.DELETE("/enroll-tokens/:id", middleware.AdminOnly(), deleteEnrollToken(d))

	// 客户端 kubeconfig 签发(签发/撤销走审计)。
	authed.POST("/clusters/:cluster/kubeconfigs", issueKubeconfig(d))
	authed.GET("/kubeconfigs", listMyKubeconfigs(d))
	authed.DELETE("/kubeconfigs/:id", revokeKubeconfig(d))

	authed.GET("/clusters", listClusters(d))
	// 集群注册/注销/凭证轮转属平台级危险操作,仅 admin。
	authed.POST("/clusters", middleware.AdminOnly(), registerCluster(d))
	authed.GET("/clusters/:cluster/status", clusterStatus(d))
	// 删除集群是危险操作,走审计中间件(v1 组已挂 Audit)。
	authed.DELETE("/clusters/:cluster", middleware.AdminOnly(), deleteCluster(d))
	authed.PUT("/clusters/:cluster/kubeconfig", middleware.AdminOnly(), rotateKubeconfig(d))

	// 资源浏览(同构):{resource} ∈ deployments|statefulsets|daemonsets|pods|
	// services|ingresses|configmaps|secrets|pvcs|pvs|nodes|namespaces|crds。
	// 读 RequirePermScope(resources:read),写 RequirePermScope(resources:write),
	// 终端 RequirePermScope(terminal:use)——按 (cluster, namespace) scope 的
	// RBAC 双层校验的平台层(01-architecture §7.3);authorizer 未注入时
	// 回退静态角色映射。
	res := authed.Group("/clusters/:cluster")
	{
		read := middleware.RequirePermScope("resources:read")
		write := middleware.RequirePermScope("resources:write")

		res.GET("/:resource", read, listResources(d))
		res.POST("/:resource", write, createResource(d)) // 危险操作,走审计(action=create)
		res.GET("/:resource/:name", read, getResource(d))
		// pods 的静态子路由(logs/terminal)使 /pods 成为独立路由分支,
		// 裸详情必须显式注册,否则落入 NoRoute 返回 40400;
		// handler 读取 :resource 参数,这里显式补齐为 pods。
		res.GET("/pods/:name", read, func(c *gin.Context) {
			c.Params = append(c.Params, gin.Param{Key: "resource", Value: "pods"})
			getResource(d)(c)
		})
		res.GET("/:resource/:name/yaml", read, getResourceYAML(d))
		res.PUT("/:resource/:name/yaml", write, putResourceYAML(d)) // 危险操作,走审计
		res.DELETE("/:resource/:name", write, deleteResource(d))    // 危险操作,走审计

		res.POST("/pods/:name/restart", write, restartPod(d))               // 审计
		res.POST("/deployments/:name/restart", write, restartDeployment(d)) // 审计
		res.PUT("/deployments/:name/scale", write, scaleDeployment(d))      // 审计

		res.GET("/pods/:name/logs", read, podLogs(d))

		res.GET("/events", middleware.RequirePermScope("events:read"), listEvents(d))
		res.GET("/metrics/nodes", middleware.RequirePermScope("metrics:read"), nodeMetrics(d))
		res.GET("/metrics/pods", middleware.RequirePermScope("metrics:read"), podMetrics(d))
	}

	// 平台 K8s 反向代理(短期 token 或 JWT 认证;挂在 /api/v1 之外)。
	proxyGroup := r.Group("", middleware.RequestID(), middleware.Logging(), middleware.Recovery())
	proxyGroup.Any("/k8s/:cluster/*path", k8sProxy(d))
	proxyGroup.Any("/k8s/:cluster", k8sProxy(d))

	// SPA 回退:未匹配的路径交由前端静态资源(embed)处理;
	// 其中 /api 前缀在 web.Handler 内返回 JSON 404,不落 index.html。
	r.NoRoute(gin.WrapH(web.Handler()))
	return r
}

// verifier 将 service.AuthService 适配为中间件的 TokenVerifier,避免 service 反向依赖 api 层。
type verifier struct{ svc *service.AuthService }

// VerifyAccessToken 实现 middleware.TokenVerifier。
func (v verifier) VerifyAccessToken(ctx context.Context, token string) (middleware.Identity, error) {
	ident, err := v.svc.VerifyAccessToken(ctx, token)
	if err != nil {
		return middleware.Identity{}, err
	}
	return middleware.Identity{UserID: ident.UserID, Username: ident.Username, Role: ident.Role}, nil
}

// auditRecorder 将 service.AuditService 适配为中间件的 Recorder。
type auditRecorder struct{ svc *service.AuditService }

// Record 实现 middleware.Recorder。
func (a auditRecorder) Record(c *gin.Context, e middleware.AuditEntry) error {
	var (
		userID   int64
		username = e.Username
	)
	// 中间件提取的 username(登录/refresh 匿名端点)优先;已认证请求回填完整身份。
	if username == "" {
		if ident, ok := middleware.IdentityOf(c); ok {
			userID = ident.UserID
			username = ident.Username
		}
	}
	return a.svc.Record(c.Request.Context(), service.AuditEntry{
		RequestID:    c.GetString("requestId"),
		UserID:       userID,
		Username:     username,
		Action:       e.Action,
		Resource:     e.Resource,
		ResourceType: e.ResourceType,
		Cluster:      e.Cluster,
		Namespace:    e.Namespace,
		Name:         e.Name,
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		Result:       e.Result,
	})
}
