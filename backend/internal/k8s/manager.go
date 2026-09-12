package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/v911/backend/internal/pkg/logx"
)

// 状态机阈值与周期(01-architecture §3.1/§3.5)。
const (
	DefaultHealthInterval = 30 * time.Second // 探活周期
	DefaultProbeTimeout   = 5 * time.Second  // /readyz 超时
	degradedThreshold     = 3                // 连续 3 次失败 → Degraded
	reconnectThreshold    = 10               // 连续 10 次失败 → Reconnecting
	backoffStart          = time.Second      // 重连退避起点 1s
	backoffMax            = 60 * time.Second // 重连退避封顶 60s
)

// StatusChange 是一次集群状态迁移的快照,经监听器推送给 watch hub 与状态缓存。
type StatusChange struct {
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	Version            string    `json:"version,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

// Options 是 ClusterManager 的构造参数;nil 字段取默认实现。
type Options struct {
	// NewClients 按集群 rest.Config 构建 clientset / dynamic client / 缓存 mapper;
	// 测试注入 fake 实现。
	NewClients func(cfg *rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error)
	// NewMetrics 构建 metrics-server 客户端;测试注入 fake。
	NewMetrics func(cfg *rest.Config) (metricsv.Interface, error)
	// HealthInterval 探活周期,默认 30s。
	HealthInterval time.Duration
	// ProbeTimeout 单次探活超时,默认 5s。
	ProbeTimeout time.Duration
	// HealthDisabled 关闭健康检查协程(测试用);healthDone 立即标记退出。
	HealthDisabled bool
	// StatusUpdater 状态迁移时同步持久缓存(如 DB status 字段);可空。
	// 禁止在回调内做耗时阻塞操作。
	StatusUpdater func(ctx context.Context, change StatusChange)
	// Probe 覆盖默认 /readyz 探活实现(仅供测试注入故障探针)。
	Probe func(ctx context.Context, rt *ClusterRuntime, timeout time.Duration) error
	// BackoffStart Reconnecting 退避起点,默认 1s(测试可缩短)。
	BackoffStart time.Duration
}

// Manager 是多集群运行时注册表:持有各集群 ClusterRuntime,串联 clusterreg
// 注册链路,驱动健康检查状态机与状态变更推送。
//
// 并发安全;所有 goroutine 由 Unregister / Close 负责停 join。
type Manager struct {
	opts Options

	mu       sync.RWMutex
	runtimes map[string]*ClusterRuntime
	dialers  *dialers

	lmu       sync.Mutex
	listeners map[int64]func(StatusChange)
	nextID    int64
}

// NewManager 构造 ClusterManager。
func NewManager(opts Options) *Manager {
	if opts.HealthInterval <= 0 {
		opts.HealthInterval = DefaultHealthInterval
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = DefaultProbeTimeout
	}
	if opts.NewClients == nil {
		opts.NewClients = defaultClients
	}
	if opts.NewMetrics == nil {
		opts.NewMetrics = defaultMetrics
	}
	if opts.BackoffStart <= 0 {
		opts.BackoffStart = backoffStart
	}
	return &Manager{
		opts:      opts,
		runtimes:  make(map[string]*ClusterRuntime),
		dialers:   newDialers(),
		listeners: make(map[int64]func(StatusChange)),
	}
}

// defaultClients 构建真实集群客户端:clientset、dynamic client 与
// MemCacheClient 包装的 ResettableRESTMapper。
func defaultClients(cfg *rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build discovery client: %w", err)
	}
	// MemCacheClient 缓存 discovery 结果,DeferredDiscoveryRESTMapper 包装为
	// ResettableRESTMapper(Reset 由健康恢复与 NoMatch 重试触发,01 §3.4)。
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return cs, dyn, mapper, nil
}

// defaultMetrics 构建 metrics-server 客户端;未安装时调用侧优雅降级。
func defaultMetrics(cfg *rest.Config) (metricsv.Interface, error) {
	return metricsv.NewForConfig(cfg)
}

// Register 将一个已通过校验的集群投入运行时注册表,构建客户端并启动健康检查。
// 由 clusterreg 注册链路在加密落库成功后调用;幂等(同名先注销旧运行时)。
func (m *Manager) Register(ctx context.Context, name, version string, cfg *rest.Config) error {
	if cfg == nil {
		return fmt.Errorf("register cluster %s: rest config is nil", name)
	}
	if err := applyDialer(name, cfg, m.dialers.get(name)); err != nil {
		return err
	}
	cs, dyn, mapper, err := m.opts.NewClients(cfg)
	if err != nil {
		return fmt.Errorf("build clients for cluster %s: %w", name, err)
	}
	metrics, err := m.opts.NewMetrics(cfg)
	if err != nil {
		// metrics-server 不可用不阻断注册,运行时按需降级。
		logx.Warn(ctx, "metrics client unavailable", "cluster", name, "err", err)
	}
	rt := newClusterRuntime(name, version, cfg, cs, dyn, metrics, mapper)
	if m.opts.HealthDisabled {
		close(rt.healthDone)
	}

	m.mu.Lock()
	if old, ok := m.runtimes[name]; ok {
		m.mu.Unlock()
		old.Stop() // 幂等重注册:先停旧 goroutine 再替换
		m.mu.Lock()
	}
	m.runtimes[name] = rt
	m.mu.Unlock()

	// 状态初始化为 ready(注册链路已拨 /version 校验过)。
	rt.setStatus("ready", "")
	m.ensureEventInformer(rt)
	m.notify(StatusChange{Name: name, Status: "ready", Version: version, LastTransitionTime: rt.lastTransitionTime()})
	if !m.opts.HealthDisabled {
		// 受控生命周期 worker:由 Unregister/Close 停止并 join。
		go m.runHealth(rt)
	}
	logx.Info(ctx, "cluster runtime registered", "cluster", name, "version", version)
	return nil
}

// Unregister 停止集群的健康检查与 informer goroutine 并等待退出,再移出注册表。
// 由 clusterreg 注销链路在删库之前调用。
func (m *Manager) Unregister(ctx context.Context, name string) {
	m.mu.Lock()
	rt, ok := m.runtimes[name]
	if ok {
		delete(m.runtimes, name)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	rt.Stop() // 先停 goroutine(join),再从注册表移除(上面已删,保证窗口内 Get 不命中)
	logx.Info(ctx, "cluster runtime unregistered", "cluster", name)
}

// Close 停止全部集群运行时;由 server 关闭路径调用。
func (m *Manager) Close(ctx context.Context) {
	m.mu.RLock()
	names := make([]string, 0, len(m.runtimes))
	for n := range m.runtimes {
		names = append(names, n)
	}
	m.mu.RUnlock()
	for _, n := range names {
		m.Unregister(ctx, n)
	}
}

// Get 返回指定集群的运行时对象;未注册返回 ErrClusterNotRegistered 语义错误。
func (m *Manager) Get(name string) (*ClusterRuntime, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rt, ok := m.runtimes[name]
	if !ok {
		return nil, fmt.Errorf("cluster %s is not registered", name)
	}
	return rt, nil
}

// Snapshot 返回集群状态快照;未注册时 ok=false。
func (m *Manager) Snapshot(name string) (StatusChange, bool) {
	m.mu.RLock()
	rt, ok := m.runtimes[name]
	m.mu.RUnlock()
	if !ok {
		return StatusChange{}, false
	}
	st, msg, lt := rt.Status()
	return StatusChange{Name: name, Status: st, Version: rt.Version, Message: msg, LastTransitionTime: lt}, true
}

// SetDialer 为指定集群注入自定义拨号器(Wave2-agent 挂载点)。
//
// Agent 反连落地后,agent 波次代码在 TunnelHub 建立 Agent 会话时调用
// SetDialer(clusterName, TunnelDialer);此后该集群 Register/重注册时经
// rest.Config.Dial 挂载隧道拨号器。d 传 nil 清除。
func (m *Manager) SetDialer(cluster string, d Dialer) {
	m.dialers.set(cluster, d)
}

// Dialer 返回指定集群当前注入的自定义拨号器;未注入返回 nil。
func (m *Manager) Dialer(cluster string) Dialer {
	return m.dialers.get(cluster)
}

// AddStatusListener 注册状态迁移监听器(如 watch hub 的 clusters resource 推送);
// 返回退订函数。监听器在健康检查协程内同步调用,必须快速返回。
func (m *Manager) AddStatusListener(l func(StatusChange)) (unsubscribe func()) {
	m.lmu.Lock()
	defer m.lmu.Unlock()
	id := m.nextID
	m.nextID++
	m.listeners[id] = l
	return func() {
		m.lmu.Lock()
		defer m.lmu.Unlock()
		delete(m.listeners, id)
	}
}

// notify 向全部监听器与持久化回调广播状态变更;发送方不阻塞、回调慢不阻断状态机。
func (m *Manager) notify(ch StatusChange) {
	if m.opts.StatusUpdater != nil {
		m.opts.StatusUpdater(context.Background(), ch)
	}
	m.lmu.Lock()
	ls := make([]func(StatusChange), 0, len(m.listeners))
	for _, l := range m.listeners {
		ls = append(ls, l)
	}
	m.lmu.Unlock()
	for _, l := range ls {
		l(ch)
	}
}

func (r *ClusterRuntime) lastTransitionTime() time.Time {
	_, _, lt := r.Status()
	return lt
}

// ensureEventInformer 集群 Ready 后启动常驻事件 informer,失败只记日志。
func (m *Manager) ensureEventInformer(rt *ClusterRuntime) {
	if err := rt.Pool.EnsureEventInformer(); err != nil {
		logx.Warn(context.Background(), "ensure event informer failed", "cluster", rt.Name, "err", err)
	}
}

// Names 返回当前已注册的全部集群名。
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.runtimes))
	for n := range m.runtimes {
		names = append(names, n)
	}
	return names
}

// OnClusterRegistered 实现 service.RuntimeHook:注册链路串联点,构建运行时并启动健康检查。
func (m *Manager) OnClusterRegistered(ctx context.Context, name, version string, cfg *rest.Config) error {
	return m.Register(ctx, name, version, cfg)
}

// OnClusterUnregistered 实现 service.RuntimeHook:注销链路串联点,先停 goroutine 再删。
func (m *Manager) OnClusterUnregistered(ctx context.Context, name string) {
	m.Unregister(ctx, name)
}
