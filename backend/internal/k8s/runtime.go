package k8s

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	dynamic "k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// gvrSpec 描述一类内置资源的 GVR 与作用域。
type gvrSpec struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

// builtinResources 内置资源名 → GVR 映射;未收录的资源(CRD plural 等)
// 经 RESTMapper/discovery 动态解析。
var builtinResources = map[string]gvrSpec{
	"deployments":   {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
	"statefulsets":  {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true},
	"daemonsets":    {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true},
	"pods":          {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, true},
	"services":      {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true},
	"ingresses":     {schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true},
	"configmaps":    {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true},
	"secrets":       {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true},
	"pvcs":          {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, true},
	"pvs":           {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"}, false},
	"nodes":         {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, false},
	"namespaces":    {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, false},
	"crds":          {schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}, false},
	"events":        {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}, true},
	"roles":         {schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true},
	"role-bindings": {schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true},
}

// ResourceRef 是解析后的资源定位:GVR + 是否命名空间级。
type ResourceRef struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// ClusterRuntime 是单个集群的运行时对象,持有访问该集群所需的全部客户端。
//
// 并发约定(01-architecture §3.2):构建完成后非状态字段只读;
// 状态字段(status/message/lastTransition)由健康检查协程独占写,读侧经 Status() 快照。
type ClusterRuntime struct {
	Name       string
	Version    string
	RestConfig *rest.Config
	ClientSet  kubernetes.Interface
	Dyn        dynamic.Interface
	Metrics    metricsv.Interface
	// Mapper 是 MemCacheClient 包装的 ResettableRESTMapper,按集群隔离禁止共享。
	Mapper meta.ResettableRESTMapper
	Pool   *InformerPool

	statusMu       sync.RWMutex
	status         string
	message        string
	lastTransition time.Time
	stopCh         chan struct{} // 关闭即通知健康检查与 informer 停止
	healthDone     chan struct{} // 健康检查协程退出时关闭
	stopOnce       sync.Once
}

// newClusterRuntime 构建运行时对象并启动 informer 池;调用方负责在 Unregister 时调用 Stop。
func newClusterRuntime(name, version string, cfg *rest.Config, cs kubernetes.Interface,
	dyn dynamic.Interface, m metricsv.Interface, mapper meta.ResettableRESTMapper) *ClusterRuntime {
	rt := &ClusterRuntime{
		Name:           name,
		Version:        version,
		RestConfig:     cfg,
		ClientSet:      cs,
		Dyn:            dyn,
		Metrics:        m,
		Mapper:         mapper,
		status:         "",
		lastTransition: time.Now(),
		stopCh:         make(chan struct{}),
		healthDone:     make(chan struct{}),
	}
	rt.Pool = newInformerPool(name, dyn, rt.stopCh)
	return rt
}

// Status 返回集群状态快照。
func (r *ClusterRuntime) Status() (status, message string, lastTransition time.Time) {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()
	return r.status, r.message, r.lastTransition
}

// setStatus 由健康检查协程独占调用,迁移状态;返回是否发生变更。
func (r *ClusterRuntime) setStatus(status, message string) bool {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	if r.status == status && r.message == message {
		return false
	}
	r.status = status
	r.message = message
	r.lastTransition = time.Now()
	return true
}

// Stop 停止 informer 池;幂等,Unregister 负责调用。
func (r *ClusterRuntime) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
	<-r.healthDone
}

// ResolveResource 将资源 plural 名解析为 GVR:先查内置表,
// 未命中走 discovery 偏好资源(CRD 等);收到 NoMatch 时 Reset 缓存重试一次
// (01-architecture §3.4,处理 CRD 刚安装的场景)。
func (r *ClusterRuntime) ResolveResource(name string) (ResourceRef, error) {
	if spec, ok := builtinResources[name]; ok {
		return ResourceRef{GVR: spec.gvr, Namespaced: spec.namespaced}, nil
	}
	ref, err := r.resolveViaDiscovery(name)
	if err != nil {
		if resettable, ok := r.Mapper.(interface{ Reset() }); ok {
			resettable.Reset()
			return r.resolveViaDiscovery(name)
		}
	}
	return ref, err
}

func (r *ClusterRuntime) resolveViaDiscovery(name string) (ResourceRef, error) {
	dc, ok := r.Mapper.(discovery.DiscoveryInterface)
	if !ok {
		return ResourceRef{}, fmt.Errorf("mapper of cluster %s does not support discovery", r.Name)
	}
	lists, err := dc.ServerPreferredResources()
	if err != nil {
		// 部分聚合 API 不可达时 ServerPreferredResources 仍返回部分结果,
		// 只有完全失败才视为解析错误。
		if lists == nil {
			return ResourceRef{}, fmt.Errorf("discover resources of cluster %s: %w", r.Name, err)
		}
	}
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, ri := range list.APIResources {
			if ri.Name == name && !strings.Contains(ri.Name, "/") {
				return ResourceRef{
					GVR:        gv.WithResource(name),
					Namespaced: ri.Namespaced,
				}, nil
			}
		}
	}
	return ResourceRef{}, fmt.Errorf("resource %q not found in cluster %s", name, r.Name)
}
