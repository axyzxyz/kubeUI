package k8s

import (
	"context"
	"errors"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
)

const (
	// informerIdleTTL 订阅者归零后 informer 的空闲保留时长,超时即回收
	// (01 §3.3);var 仅供测试缩短回收周期。
	_ = ""
	// informerResyncPeriod informer resync 周期。
	informerResyncPeriod = time.Minute
)

var errPoolClosed = errors.New("informer pool closed")

// informerIdleTTL 订阅者归零后 informer 的空闲保留时长(01 §3.3);
// var 仅供测试缩短回收周期。
var (
	informerIdleTTL    = 5 * time.Minute  // 订阅者归零后的空闲保留时长
	informerReapPeriod = 30 * time.Second // 空闲回收扫描周期(测试可缩短)
)

// EventHandler 是 informer 事件的消费回调,object 为 unstructured 对象。
type EventHandler func(action string, namespace, name string, object any)

// informerEntry 是单个 cluster+GVR(+namespace 维度)的懒启动 informer。
type informerEntry struct {
	gvr        schema.GroupVersionResource
	namespace  string
	factory    dynamicinformer.DynamicSharedInformerFactory
	informer   cache.SharedIndexInformer
	stopCh     chan struct{}
	handlers   map[string]EventHandler
	resident   bool
	lastAccess time.Time
}

// InformerPool 管理单个集群的懒启动 dynamic informer:watch 订阅驱动启动,
// 引用计数 +1;订阅者归零后空闲 5 分钟回收;Event informer 集群 Ready 后常驻。
//
// 并发安全;Stop 关闭全部 informer 并等待回收协程退出。
type InformerPool struct {
	cluster string
	dyn     dynamic.Interface
	stopCh  <-chan struct{} // 随 ClusterRuntime 停止

	mu      sync.Mutex
	entries map[string]*informerEntry
	reaper  *time.Ticker
	closed  bool
}

// newInformerPool 构造 informer 池并启动空闲回收协程;runtimeStop 关闭时池整体停止。
// 回收协程由 ClusterRuntime.Stop 负责 join(经 stopCh)。
func newInformerPool(cluster string, dyn dynamic.Interface, runtimeStop <-chan struct{}) *InformerPool {
	p := &InformerPool{
		cluster: cluster,
		dyn:     dyn,
		stopCh:  runtimeStop,
		entries: make(map[string]*informerEntry),
		reaper:  time.NewTicker(informerReapPeriod),
	}
	go p.reapLoop()
	return p
}

func entryKey(gvr schema.GroupVersionResource, namespace string) string {
	return gvr.String() + "|" + namespace
}

// Watch 为 id 注册事件处理器;首次订阅懒启动 informer(引用计数 +1)。
// 重复 id 返回错误;调用方负责在订阅取消时调用 Unwatch。
func (p *InformerPool) Watch(gvr schema.GroupVersionResource, namespace, id string, h EventHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errPoolClosed
	}
	key := entryKey(gvr, namespace)
	e, ok := p.entries[key]
	if !ok {
		factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(p.dyn, informerResyncPeriod, namespaceOrAll(namespace), nil)
		inf := factory.ForResource(gvr).Informer()
		e = &informerEntry{
			gvr:        gvr,
			namespace:  namespace,
			factory:    factory,
			informer:   inf,
			stopCh:     make(chan struct{}),
			handlers:   make(map[string]EventHandler),
			lastAccess: time.Now(),
		}
		p.entries[key] = e
		if _, err := inf.AddEventHandler(&forwardHandler{cluster: p.cluster, gvr: gvr, pool: p, key: key}); err != nil {
			logx.Warn(context.Background(), "register event handler failed",
				"cluster", p.cluster, "resource_type", gvr.Resource, "err", err)
		}
		factory.Start(e.stopCh)
		logx.Info(context.Background(), "informer started",
			"cluster", p.cluster, "resource_type", gvr.Resource, "namespace", namespace)
	}
	// 同 id 重复注册(如断连后未及时 Unwatch 的恢复路径)直接替换 handler,
	// 保证订阅幂等;替换不改变引用计数语义。
	e.handlers[id] = h
	e.lastAccess = time.Now()
	return nil
}

// Unwatch 移除订阅,引用计数归零后 informer 进入空闲回收倒计时。
func (p *InformerPool) Unwatch(gvr schema.GroupVersionResource, namespace, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[entryKey(gvr, namespace)]
	if !ok {
		return
	}
	delete(e.handlers, id)
	if !e.resident && len(e.handlers) == 0 {
		e.lastAccess = time.Now()
	}
}

// EnsureEventInformer 启动常驻 corev1.Event informer(集群 Ready 后调用,01 §3.3)。
func (p *InformerPool) EnsureEventInformer() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errPoolClosed
	}
	gvr := builtinResources["events"].gvr
	key := entryKey(gvr, "")
	if e, ok := p.entries[key]; ok {
		e.resident = true
		return nil
	}
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(p.dyn, informerResyncPeriod, corev1.NamespaceAll, nil)
	inf := factory.ForResource(gvr).Informer()
	e := &informerEntry{
		gvr:        gvr,
		factory:    factory,
		informer:   inf,
		stopCh:     make(chan struct{}),
		handlers:   make(map[string]EventHandler),
		resident:   true,
		lastAccess: time.Now(),
	}
	p.entries[key] = e
	if _, err := inf.AddEventHandler(&forwardHandler{cluster: p.cluster, gvr: gvr, pool: p, key: key}); err != nil {
		logx.Warn(context.Background(), "register resident event handler failed",
			"cluster", p.cluster, "resource_type", gvr.Resource, "err", err)
	}
	factory.Start(e.stopCh)
	return nil
}

// Stop 关闭全部 informer 并停止回收协程;由 ClusterRuntime.Stop 调用。
func (p *InformerPool) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	for _, e := range p.entries {
		close(e.stopCh)
	}
	p.entries = make(map[string]*informerEntry)
	p.mu.Unlock()
	p.reaper.Stop()
}

// reapLoop 周期回收:订阅者归零且空闲超过 informerIdleTTL 的 informer 停止并移除。
// 生命周期:随 p.stopCh(ClusterRuntime.stopCh)退出。
func (p *InformerPool) reapLoop() {
	for {
		select {
		case <-p.stopCh:
			p.Stop()
			return
		case <-p.reaper.C:
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		now := time.Now()
		for key, e := range p.entries {
			if e.resident || len(e.handlers) > 0 {
				continue
			}
			if now.Sub(e.lastAccess) >= informerIdleTTL {
				close(e.stopCh)
				delete(p.entries, key)
				logx.Info(context.Background(), "informer reclaimed",
					"cluster", p.cluster, "resource_type", e.gvr.Resource, "namespace", e.namespace)
			}
		}
		p.mu.Unlock()
	}
}

// forwardHandler 将 informer 事件分发给当前注册的全部订阅回调;快照 handler
// 列表后在锁外执行,避免慢订阅阻塞池锁。
type forwardHandler struct {
	cluster string
	gvr     schema.GroupVersionResource
	pool    *InformerPool
	key     string // 所属 entry 的 key,避免按对象 namespace 反查
}

// OnAdd 实现 cache.ResourceEventHandler。
func (f *forwardHandler) OnAdd(obj any, _ bool) { f.dispatch("added", obj) }

// OnUpdate 实现 cache.ResourceEventHandler。
func (f *forwardHandler) OnUpdate(_, newObj any) { f.dispatch("modified", newObj) }

// OnDelete 实现 cache.ResourceEventHandler。
func (f *forwardHandler) OnDelete(obj any) { f.dispatch("deleted", obj) }

func (f *forwardHandler) dispatch(action string, obj any) {
	mObj, ok := obj.(interface {
		GetNamespace() string
		GetName() string
	})
	if !ok {
		return
	}
	f.pool.mu.Lock()
	e := f.pool.entries[f.key]
	if e == nil {
		f.pool.mu.Unlock()
		return
	}
	hs := make([]EventHandler, 0, len(e.handlers))
	for _, h := range e.handlers {
		hs = append(hs, h)
	}
	f.pool.mu.Unlock()
	for _, h := range hs {
		h(action, mObj.GetNamespace(), mObj.GetName(), obj)
	}
}

func namespaceOrAll(ns string) string {
	if ns == "" {
		return corev1.NamespaceAll
	}
	return ns
}
