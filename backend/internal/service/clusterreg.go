package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/crypto"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// probeTimeout 是注册时拨测目标集群 /version 的超时(01-architecture §3.1)。
const probeTimeout = 10 * time.Second

// RuntimeHook 是 clusterreg 与 internal/k8s.ClusterManager 的串联挂载点:
// 注册/注销成功后由 ClusterManager 维护运行时与健康检查 goroutine。
type RuntimeHook interface {
	// OnClusterRegistered 在注册链路落库成功后调用,构建运行时并启动健康检查。
	OnClusterRegistered(ctx context.Context, name, version string, cfg *rest.Config) error
	// OnClusterUnregistered 在注销链路删库之前调用,先停 goroutine 再删。
	OnClusterUnregistered(ctx context.Context, name string)
}

// ClusterRegService 处理 kubeconfig 注册、列表、注销与状态查询。
type ClusterRegService struct {
	repo     *store.ClusterRepo
	cipher   *crypto.Cipher
	registry ClusterRegistry
	// probeVersion 拨测目标集群 /version 并返回版本号;可注入替换以便测试。
	probeVersion func(ctx context.Context, cfg *rest.Config) (string, error)
	// runtimeHook 可空;由 main 装配注入 ClusterManager。
	runtimeHook RuntimeHook
}

// NewClusterRegService 构造 ClusterRegService。
func NewClusterRegService(repo *store.ClusterRepo, cipher *crypto.Cipher, registry ClusterRegistry) *ClusterRegService {
	return &ClusterRegService{
		repo:         repo,
		cipher:       cipher,
		registry:     registry,
		probeVersion: probeServerVersion,
	}
}

// SetRuntimeHook 注入运行时串联钩子(ClusterManager);须在首次注册前调用。
func (s *ClusterRegService) SetRuntimeHook(h RuntimeHook) { s.runtimeHook = h }

// SetProbeVersion 注入自定义 /version 探测函数(仅供测试)。
func (s *ClusterRegService) SetProbeVersion(f func(ctx context.Context, cfg *rest.Config) (string, error)) {
	s.probeVersion = f
}

// RegisterRequest 是集群注册请求。
type RegisterRequest struct {
	Name        string // 集群展示名(注册表主键)
	Kubeconfig  string // kubeconfig YAML 原文
	ContextName string // 多 context 时必须显式指定
	Description string
	// AccessMode 接入方式:direct(默认,平台直连拨测) | agent(反连,跳过拨测,
	// 置 offline 等待 Agent 首次接入后由 TunnelHub 重注册收敛)。
	AccessMode string
}

// Register 执行注册全链路:解析 → 拨测 /version(仅 direct)→ AES-GCM 加密落库
// → 写入注册表。kubeconfig 原文与凭证内容只进入加密存储,严禁出现在日志与错误信息中。
func (s *ClusterRegService) Register(ctx context.Context, req RegisterRequest) (*model.ClusterInfo, error) {
	if req.Name == "" {
		return nil, errcode.New(errcode.ParamInvalid, "cluster name is required")
	}
	if req.Kubeconfig == "" {
		return nil, errcode.New(errcode.ClusterKubeconfigBad, "kubeconfig is required")
	}
	mode := req.AccessMode
	if mode == "" {
		mode = model.AccessModeDirect
	}
	if mode != model.AccessModeDirect && mode != model.AccessModeAgent {
		return nil, errcode.New(errcode.ParamInvalid, "accessMode must be direct or agent")
	}
	cfg, err := s.parseAndValidate(req.Kubeconfig, req.ContextName)
	if err != nil {
		return nil, err
	}
	// agent 模式下平台到 APIServer 通常不可达,拨测留待 Agent 接入后经隧道进行。
	var probeErr error
	version := ""
	if mode == model.AccessModeDirect {
		version, probeErr = s.probeVersion(ctx, cfg)
		if probeErr != nil {
			return nil, errcode.New(errcode.ClusterUnreachable, "cluster apiserver unreachable or credentials invalid").WithCause(probeErr)
		}
	}
	sealed, err := s.cipher.Encrypt([]byte(req.Kubeconfig))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	status := model.ClusterStatusReady
	message := ""
	if mode == model.AccessModeAgent {
		status = model.ClusterStatusOffline
		message = "waiting for agent enrollment"
	}
	rec := &store.Cluster{
		Name:                req.Name,
		Description:         req.Description,
		KubeconfigEncrypted: sealed,
		AccessMode:          mode,
		Status:              status,
		Message:             message,
		Version:             version,
		LastTransitionTime:  now,
	}
	if err := s.repo.UpsertCluster(ctx, rec); err != nil {
		return nil, err
	}
	s.registry.Put(ClusterRuntime{
		Name:               rec.Name,
		Status:             rec.Status,
		Version:            rec.Version,
		AccessMode:         rec.AccessMode,
		LastTransitionTime: now,
	})
	if s.runtimeHook != nil {
		if err := s.runtimeHook.OnClusterRegistered(ctx, rec.Name, version, cfg); err != nil {
			return nil, fmt.Errorf("start cluster runtime %s: %w", rec.Name, err)
		}
	}
	logx.Info(ctx, "cluster registered", "cluster", req.Name, "version", version)
	return toClusterInfo(rec), nil
}

// RestoreClusters 在服务启动时从数据库恢复集群运行时。
//
// 服务重启后集群注册表(内存)为空,资源端点会全部 40401;此方法按库中记录
// 解密 kubeconfig 并重建运行时:
//   - 解密失败(master key 变更等):置 offline 并写明原因,记录保留供重新轮转;
//   - 解析失败(如多 context 未存 context):置 offline 并提示重新轮转;
//   - 其余情况直接重建运行时并交给健康检查循环收敛状态(可达即 Ready)。
func (s *ClusterRegService) RestoreClusters(ctx context.Context) error {
	recs, err := s.repo.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters for restore: %w", err)
	}
	for i := range recs {
		rec := &recs[i]
		if err := s.restoreOne(ctx, rec); err != nil {
			logx.Error(ctx, "restore cluster failed", "cluster", rec.Name, "err", err)
		}
	}
	return nil
}

func (s *ClusterRegService) restoreOne(ctx context.Context, rec *store.Cluster) error {
	plain, err := s.cipher.Decrypt(rec.KubeconfigEncrypted)
	if err != nil {
		msg := "kubeconfig decrypt failed: master key mismatch, rotate kubeconfig to recover"
		logx.Error(ctx, "decrypt stored kubeconfig failed", "cluster", rec.Name, "err", err)
		if uerr := s.repo.UpdateClusterStatus(ctx, rec.Name, model.ClusterStatusOffline, rec.Version, msg); uerr != nil {
			return fmt.Errorf("mark offline: %w", uerr)
		}
		return nil
	}
	cfg, err := s.parseAndValidate(string(plain), "")
	if err != nil {
		msg := "stored kubeconfig parse failed, rotate kubeconfig to recover"
		logx.Error(ctx, "parse stored kubeconfig failed", "cluster", rec.Name, "err", err)
		if uerr := s.repo.UpdateClusterStatus(ctx, rec.Name, model.ClusterStatusOffline, rec.Version, msg); uerr != nil {
			return fmt.Errorf("mark offline: %w", uerr)
		}
		return nil
	}
	s.registry.Put(ClusterRuntime{
		Name:               rec.Name,
		Status:             model.ClusterStatusReconnecting,
		Version:            rec.Version,
		AccessMode:         rec.AccessMode,
		LastTransitionTime: time.Now(),
	})
	if s.runtimeHook != nil {
		if err := s.runtimeHook.OnClusterRegistered(ctx, rec.Name, rec.Version, cfg); err != nil {
			return fmt.Errorf("start cluster runtime %s: %w", rec.Name, err)
		}
	}
	logx.Info(ctx, "cluster runtime restored", "cluster", rec.Name)
	return nil
}

// List 返回全部集群信息(不含任何凭证内容)。
func (s *ClusterRegService) List(ctx context.Context) ([]model.ClusterInfo, error) {
	recs, err := s.repo.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.ClusterInfo, 0, len(recs))
	for i := range recs {
		items = append(items, *toClusterInfo(&recs[i]))
	}
	return items, nil
}

// Status 返回集群连接状态缓存,不透传探测。
func (s *ClusterRegService) Status(ctx context.Context, name string) (*model.ClusterStatus, error) {
	rec, err := s.repo.GetClusterByName(ctx, name)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
		}
		return nil, err
	}
	return &model.ClusterStatus{
		Name:               rec.Name,
		Status:             rec.Status,
		Version:            rec.Version,
		LastTransitionTime: rec.LastTransitionTime,
		Message:            rec.Message,
	}, nil
}

// Delete 注销集群:先停运行时 goroutine,再删库、移出注册表。危险操作,由 handler 挂审计。
func (s *ClusterRegService) Delete(ctx context.Context, name string) error {
	if _, err := s.repo.GetClusterByName(ctx, name); err != nil {
		if isNotFound(err) {
			return errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
		}
		return err
	}
	if s.runtimeHook != nil {
		s.runtimeHook.OnClusterUnregistered(ctx, name)
	}
	s.registry.Remove(name)
	if err := s.repo.DeleteCluster(ctx, name); err != nil {
		return err
	}
	logx.Info(ctx, "cluster unregistered", "cluster", name)
	return nil
}

// RotateKubeconfig 用新 kubeconfig 热更新集群凭证。
//
// TODO(next-wave #1): 由下一波的 ClusterManager 实现平滑热更新——
// 用新凭证重建 clientset 与 Dialer,期间置 Degraded,成功后切回 Ready;
// 当前实现复用注册链路整体覆盖(接入模式保持不变)。
func (s *ClusterRegService) RotateKubeconfig(ctx context.Context, name, kubeconfig, contextName string) (*model.ClusterInfo, error) {
	rec, err := s.repo.GetClusterByName(ctx, name)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
		}
		return nil, err
	}
	return s.Register(ctx, RegisterRequest{
		Name:        name,
		Kubeconfig:  kubeconfig,
		ContextName: contextName,
		Description: s.describeIfExists(ctx, name),
		AccessMode:  rec.AccessMode,
	})
}

// parseAndValidate 解析 kubeconfig:非法返回 40402;多 context 未指定返回 40403。
func (s *ClusterRegService) parseAndValidate(kubeconfig, contextName string) (*rest.Config, error) {
	raw, err := clientcmd.Load([]byte(kubeconfig))
	if err != nil {
		return nil, errcode.New(errcode.ClusterKubeconfigBad, "invalid kubeconfig").WithCause(err)
	}
	if len(raw.Contexts) > 1 && contextName == "" {
		return nil, errcode.New(errcode.ClusterKubeconfigMultiContext,
			"kubeconfig contains multiple contexts, specify contextName explicitly")
	}
	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" || raw.Contexts[name] == nil {
		return nil, errcode.New(errcode.ClusterKubeconfigBad, "context not found in kubeconfig")
	}
	cfg, err := buildRestConfig(raw, name)
	if err != nil {
		return nil, errcode.New(errcode.ClusterKubeconfigBad, "invalid kubeconfig").WithCause(err)
	}
	return cfg, nil
}

func (s *ClusterRegService) describeIfExists(ctx context.Context, name string) string {
	rec, err := s.repo.GetClusterByName(ctx, name)
	if err != nil {
		return ""
	}
	return rec.Description
}

// buildRestConfig 从指定 context 构造 rest.Config,不落任何凭证日志。
func buildRestConfig(raw *clientcmdapi.Config, contextName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, contextName, overrides, nil).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build rest config from context %s: %w", contextName, err)
	}
	return cfg, nil
}

// probeServerVersion 拨测目标集群 /version,10s 超时。
func probeServerVersion(ctx context.Context, cfg *rest.Config) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("build clientset: %w", err)
	}
	body, err := client.Discovery().RESTClient().Get().AbsPath("/version").Do(probeCtx).Raw()
	if err != nil {
		return "", fmt.Errorf("get server version: %w", err)
	}
	var v version.Info
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("decode server version: %w", err)
	}
	return v.GitVersion, nil
}

func toClusterInfo(rec *store.Cluster) *model.ClusterInfo {
	return &model.ClusterInfo{
		Name:        rec.Name,
		Status:      rec.Status,
		Version:     rec.Version,
		AccessMode:  rec.AccessMode,
		Description: rec.Description,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}
}
