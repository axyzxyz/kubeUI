// v911 服务端唯一入口:仅做装配,禁止业务逻辑。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/v911/backend/internal/agent"
	"github.com/v911/backend/internal/api/handler"
	"github.com/v911/backend/internal/api/middleware"
	"github.com/v911/backend/internal/config"
	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/crypto"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/service"
	"github.com/v911/backend/internal/store"
	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "", "path to config yaml (optional)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	logx.Init(cfg.Log.Level, cfg.Log.Format)

	if err := run(cfg); err != nil {
		logx.Error(context.Background(), "server exited with error", "err", err)
		os.Exit(1)
	}
}

// run 装配依赖并启动 HTTP 服务,阻塞直到收到退出信号;由 main 调用。
func run(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	bootstrapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := store.SeedDefaults(bootstrapCtx, db, service.BuiltinRoleSeeds()); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}
	if err := ensureDefaultAdmin(bootstrapCtx, db); err != nil {
		return fmt.Errorf("ensure default admin: %w", err)
	}

	cipher, err := crypto.NewCipher(cfg.Security.MasterKey)
	if err != nil {
		return fmt.Errorf("init crypto: %w", err)
	}

	userRepo := store.NewUserRepo(db)
	refreshRepo := store.NewRefreshTokenRepo(db)
	clusterRepo := store.NewClusterRepo(db)
	auditRepo := store.NewAuditRepo(db)
	enrollRepo := store.NewEnrollTokenRepo(db)
	issuedRepo := store.NewIssuedKubeconfigRepo(db)

	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg.Security.JWTSecret)
	userSvc := service.NewUserService(userRepo, authSvc)
	// RBAC:中间件按角色权限点放行(PermissionsFor 由 service 维护);
	// authorizer 经中心授权表(grants)按 (cluster, namespace) scope 动态解析。
	middleware.SetPermChecker(service.PermissionsFor)
	groupRepo := store.NewUserGroupRepo(db)
	roleSvc := service.NewRoleService(store.NewRoleRepo(db), groupRepo, userRepo,
		store.NewRoleGroupRepo(db), store.NewGrantRepo(db))
	middleware.SetAuthorizer(roleSvc.Authorize)
	auditSvc := service.NewAuditService(auditRepo)
	registry := service.NewMemRegistry()
	clusterSvc := service.NewClusterRegService(clusterRepo, cipher, registry)
	enrollSvc := service.NewEnrollService(enrollRepo, clusterRepo)
	kubeSvc := service.NewKubeconfigService(issuedRepo, clusterRepo, cfg.Server.ExternalURL)

	// K8s 运行时:ClusterManager + 资源浏览服务 + watch hub。
	// 状态迁移经监听器串联:更新内存注册表 + DB 状态缓存(不透传探测)。
	manager := k8s.NewManager(k8s.Options{
		StatusUpdater: func(ctx context.Context, ch k8s.StatusChange) {
			registry.Put(service.ClusterRuntime{
				Name:               ch.Name,
				Status:             ch.Status,
				Version:            ch.Version,
				LastTransitionTime: ch.LastTransitionTime,
			})
			if err := clusterRepo.UpdateClusterStatus(ctx, ch.Name, ch.Status, ch.Version, ch.Message); err != nil {
				logx.Warn(ctx, "persist cluster status failed", "cluster", ch.Name, "err", err)
			}
		},
	})
	clusterSvc.SetRuntimeHook(manager)
	// 启动恢复:从数据库重建集群运行时(解密失败/解析失败的集群置 offline 并保留记录)。
	restoreCtx, restoreCancel := context.WithTimeout(ctx, 2*time.Minute)
	if err := clusterSvc.RestoreClusters(restoreCtx); err != nil {
		logx.Error(ctx, "restore clusters failed", "err", err)
	}
	restoreCancel()
	resourceSvc := service.NewResourceService(manager)
	watchHub := service.NewWatchHub(manager, auditSvc)

	// Agent 反连:会话建立即注入 TunnelDialer 并重注册运行时;
	// 接入/断开同步内存注册表与 DB 状态缓存。
	agentHub := agent.NewHub(agent.Options{
		Manager:       manager,
		ValidateToken: enrollSvc.Validate,
		ConfirmToken:  enrollSvc.Confirm,
		OnConnect: func(ctx context.Context, cluster, agentVersion string) {
			registry.Put(service.ClusterRuntime{
				Name:               cluster,
				Status:             model.ClusterStatusReady,
				AccessMode:         model.AccessModeAgent,
				LastTransitionTime: time.Now(),
			})
			if err := clusterRepo.UpdateClusterAccessMode(ctx, cluster, model.AccessModeAgent); err != nil {
				logx.Warn(ctx, "persist cluster access mode failed", "cluster", cluster, "err", err)
			}
			if err := clusterRepo.UpdateClusterStatus(ctx, cluster, model.ClusterStatusReady, "agent "+agentVersion, ""); err != nil {
				logx.Warn(ctx, "persist cluster status failed", "cluster", cluster, "err", err)
			}
			logx.Info(ctx, "agent tunnel established", "cluster", cluster, "agent_version", agentVersion)
		},
		OnDisconnect: func(ctx context.Context, cluster string) {
			registry.Put(service.ClusterRuntime{
				Name:               cluster,
				Status:             model.ClusterStatusDegraded,
				AccessMode:         model.AccessModeAgent,
				LastTransitionTime: time.Now(),
			})
			if err := clusterRepo.UpdateClusterStatus(ctx, cluster,
				model.ClusterStatusDegraded, "", "agent 反连中"); err != nil {
				logx.Warn(ctx, "persist cluster status failed", "cluster", cluster, "err", err)
			}
			logx.Warn(ctx, "agent tunnel disconnected", "cluster", cluster)
		},
	})

	srv := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: handler.Routes(handler.Deps{
			Auth:        authSvc,
			Users:       userSvc,
			Roles:       roleSvc,
			Audit:       auditSvc,
			Clusters:    clusterSvc,
			Resources:   resourceSvc,
			Watch:       watchHub,
			Manager:     manager,
			Enroll:      enrollSvc,
			Kubeconfigs: kubeSvc,
			AgentHub:    agentHub,
			Config:      cfg,
		}),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	errCh := make(chan error, 1)
	// 受控启动(main 关闭路径):错误经 channel 回传,由本函数统一处理。
	go func() {
		logx.Info(ctx, "http server listening", "addr", cfg.Server.Addr)
		if cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
			if err := srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logx.Error(ctx, "https server exited with error", "err", err)
			}
		} else if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen and serve: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// 关闭顺序:先停 HTTP,再停 K8s 运行时与健康检查 goroutine。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	watchHub.Stop()
	agentHub.Close(shutdownCtx)
	manager.Close(shutdownCtx)
	logx.Info(context.Background(), "server stopped")
	return nil
}

// ensureDefaultAdmin 首次启动时创建默认管理员 admin/admin123 并提示修改。
func ensureDefaultAdmin(ctx context.Context, db *gorm.DB) error {
	userRepo := store.NewUserRepo(db)
	if _, err := userRepo.GetUserByUsername(ctx, "admin"); err == nil {
		return nil
	}
	hash, err := service.HashPassword("admin123")
	if err != nil {
		return err
	}
	if err := userRepo.CreateUser(ctx, &store.User{
		Username:     "admin",
		PasswordHash: hash,
		Role:         model.RoleAdmin,
	}); err != nil {
		return err
	}
	logx.Warn(ctx, "default admin created (admin/admin123), change the password immediately")
	return nil
}
