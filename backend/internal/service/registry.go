package service

import (
	"sync"
	"time"
)

// ClusterRegistry 是集群运行时注册表的消费方接口(定义在本包,符合规范 §2.1)。
//
// 下一波挂载点说明:
//   - internal/k8s 的 ClusterManager 将扩展或实现完整生命周期:
//     Register 时构建 rest.Config / clientset / dynamic client / restMapper 缓存 /
//     InformerPool,并启动健康检查协程(30s 探活 + 状态机迁移);
//   - 本波最小实现 memRegistry 只维护名称 → 状态元数据的映射,
//     状态字段由 clusterreg 在注册/注销时写入;
//   - ClusterRuntime 的深度字段(RestConfig、ClientSet、Dialer 等)留待
//     ClusterManager 落地时补充,接口方法保持不变。
type ClusterRegistry interface {
	// Put 写入或更新集群运行时元数据。
	Put(entry ClusterRuntime)
	// Get 返回指定集群的运行时元数据,不存在时返回 ErrClusterNotRegistered。
	Get(name string) (*ClusterRuntime, error)
	// Remove 从注册表移除集群。
	Remove(name string)
	// Names 返回当前注册的全部集群名。
	Names() []string
}

// ClusterRuntime 是集群在内存注册表中的最小运行时元数据。
//
// 所有字段写入后只读(状态迁移由持有者独占写);深度 K8s 对象
// (rest.Config、clientset、informer 等)在下一波由 internal/k8s.ClusterManager
// 扩展本结构或引入新结构承载。
type ClusterRuntime struct {
	Name               string
	Status             string // ready|degraded|reconnecting|offline
	Version            string
	AccessMode         string // direct|agent
	LastTransitionTime time.Time
}

// ErrClusterNotRegistered 表示集群不在内存注册表中。
type errClusterNotRegistered struct{ name string }

func (e *errClusterNotRegistered) Error() string {
	return "cluster not registered: " + e.name
}

// ErrClusterNotRegistered 构造注册表未命中错误。
func NewErrClusterNotRegistered(name string) error { return &errClusterNotRegistered{name: name} }

// memRegistry 是 ClusterRegistry 的最小内存实现,并发安全。
type memRegistry struct {
	mu      sync.RWMutex
	entries map[string]ClusterRuntime
}

// NewMemRegistry 构造空的内存注册表。
func NewMemRegistry() ClusterRegistry {
	return &memRegistry{entries: make(map[string]ClusterRuntime)}
}

// Put 写入或更新集群运行时元数据。
func (r *memRegistry) Put(entry ClusterRuntime) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[entry.Name] = entry
}

// Get 返回指定集群的运行时元数据。
func (r *memRegistry) Get(name string) (*ClusterRuntime, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return nil, NewErrClusterNotRegistered(name)
	}
	return &e, nil
}

// Remove 从注册表移除集群。
func (r *memRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

// Names 返回当前注册的全部集群名。
func (r *memRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	return names
}
