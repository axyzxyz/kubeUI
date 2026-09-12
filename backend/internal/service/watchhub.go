package service

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/axyzxyz/kubeui/backend/internal/k8s"
	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

// watchResources 是 /api/v1/watch 可订阅的 resource 枚举(websocket.md §1.2)。
var watchResources = map[string]bool{
	"pods": true, "deployments": true, "statefulsets": true, "daemonsets": true,
	"services": true, "ingresses": true, "configmaps": true, "secrets": true,
	"events": true, "nodes": true, "crds": true,
	"clusters": true, "audit": true, "podlogs": true,
}

// WatchConn 是 watch 端点连接的抽象,由 handler 层实现(hub 不依赖 gin/websocket)。
type WatchConn interface {
	// ID 连接唯一标识,用于退订。
	ID() string
	// Send 非阻塞投递一帧;连接慢或已关闭返回 false(hub 丢弃,不阻塞)。
	Send(env wsx.Envelope) bool
}

// subKey 是一个订阅维度:普通资源按 cluster+resource+namespace;
// podlogs 追加 Name;clusters/audit 只看 resource(带 cluster 过滤)。
type subKey struct {
	cluster   string
	resource  string
	namespace string
	name      string
}

// subState 是一个订阅维度的聚合状态。
type subState struct {
	conns map[WatchConn]wsx.SubscribePayload
	gvr   schema.GroupVersionResource
	// stop 终止 podlogs 跟随 goroutine;普通资源为 nil。
	stop func()
}

// WatchHub 管理订阅并驱动 informer 懒启动:首个订阅者触发 InformerPool.Watch,
// 最后一个退订触发 Unwatch(空闲回收由池完成);clusters 资源经 Manager 状态
// 监听器推送;audit 经 AuditService 事件流转发;podlogs 经日志 follow 流转发。
//
// 并发安全;Stop 关闭全部跟随协程。
type WatchHub struct {
	mgr   *k8s.Manager
	audit *AuditService

	mu          sync.Mutex
	subs        map[subKey]*subState
	conns       map[WatchConn]map[subKey]struct{} // 连接 → 订阅集合
	clusterSubs map[WatchConn]string              // clusters 订阅的 cluster 过滤(空 = 全部)
	auditSubs   map[WatchConn]struct{}
	statusUnsub func()
	auditCh     <-chan model.AuditLog
	auditUnsub  func()
	logs        *ResourceService
	stopped     bool
}

// NewWatchHub 构造并接管 Manager 的状态推送;audit 可空(测试场景)。
func NewWatchHub(mgr *k8s.Manager, audit *AuditService) *WatchHub {
	h := &WatchHub{
		mgr:         mgr,
		audit:       audit,
		logs:        NewResourceService(mgr),
		subs:        make(map[subKey]*subState),
		conns:       make(map[WatchConn]map[subKey]struct{}),
		clusterSubs: make(map[WatchConn]string),
		auditSubs:   make(map[WatchConn]struct{}),
	}
	h.statusUnsub = mgr.AddStatusListener(h.publishClusterStatus)
	if audit != nil {
		ch, unsub := audit.SubscribeStream()
		h.auditCh, h.auditUnsub = ch, unsub
		// 受控生命周期 worker:随 Hub.Stop 退出。
		go h.pumpAudit()
	}
	return h
}

// Stop 停止全部跟随协程并退订;由 server 关闭路径调用。
func (h *WatchHub) Stop() {
	h.mu.Lock()
	h.stopped = true
	subs := h.subs
	h.subs = make(map[subKey]*subState)
	h.mu.Unlock()
	for _, st := range subs {
		if st.stop != nil {
			st.stop()
		}
	}
	if h.statusUnsub != nil {
		h.statusUnsub()
	}
	if h.auditUnsub != nil {
		h.auditUnsub()
	}
}

// Subscribe 为连接新增一路订阅;资源枚举非法返回 40001,集群未注册返回 40401。
func (h *WatchHub) Subscribe(conn WatchConn, p wsx.SubscribePayload) error {
	if !watchResources[p.Resource] {
		return errcode.New(errcode.ParamInvalid, "unsupported resource "+p.Resource)
	}
	if p.Cluster == "" {
		return errcode.New(errcode.ParamInvalid, "cluster is required")
	}
	key := subKey{cluster: p.Cluster, resource: p.Resource, namespace: p.Namespace, name: p.Name}
	switch p.Resource {
	case "clusters":
		h.mu.Lock()
		h.clusterSubs[conn] = p.Cluster
		h.mu.Unlock()
		h.pushClusterSnapshot(conn, p.Cluster)
		return nil
	case "audit":
		h.mu.Lock()
		h.auditSubs[conn] = struct{}{}
		h.addConnSub(conn, key)
		h.mu.Unlock()
		return nil
	}
	rt, err := h.mgr.Get(p.Cluster)
	if err != nil {
		return errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
	}
	if p.Resource == "podlogs" {
		if p.Name == "" {
			return errcode.New(errcode.ParamInvalid, "podlogs subscription requires name")
		}
		return h.subscribePodLogs(conn, key)
	}
	ref, err := rt.ResolveResource(p.Resource)
	if err != nil {
		return errcode.New(errcode.ParamInvalid, "unsupported resource "+p.Resource).WithCause(err)
	}
	h.mu.Lock()
	st, ok := h.subs[key]
	if !ok {
		st = &subState{conns: make(map[WatchConn]wsx.SubscribePayload), gvr: ref.GVR}
		h.subs[key] = st
	}
	st.conns[conn] = p
	h.addConnSub(conn, key)
	first := len(st.conns) == 1
	h.mu.Unlock()
	if first {
		gvr, ns := ref.GVR, p.Namespace
		err := rt.Pool.Watch(gvr, ns, h.watchID(key), func(action, ens, name string, obj any) {
			h.deliver(key, action, ens, name, obj)
		})
		if err != nil {
			h.mu.Lock()
			h.removeConnSubLocked(conn, key)
			delete(st.conns, conn)
			if len(st.conns) == 0 {
				delete(h.subs, key)
			}
			h.mu.Unlock()
			return errcode.New(errcode.UpstreamK8sError, "start watch failed").WithCause(err)
		}
	}
	return nil
}

// Unsubscribe 按维度退订;无匹配订阅静默返回。
func (h *WatchHub) Unsubscribe(conn WatchConn, cluster, resource, namespace, name string) {
	key := subKey{cluster: cluster, resource: resource, namespace: namespace, name: name}
	h.mu.Lock()
	if resource == "clusters" {
		delete(h.clusterSubs, conn)
	}
	if resource == "audit" {
		delete(h.auditSubs, conn)
	}
	h.removeConnSubLocked(conn, key)
	st, ok := h.subs[key]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(st.conns, conn)
	last := len(st.conns) == 0
	stop := st.stop
	if last {
		delete(h.subs, key)
	}
	h.mu.Unlock()
	if last {
		if stop != nil {
			stop()
		}
		if rt, err := h.mgr.Get(cluster); err == nil && resource != "podlogs" {
			if ref, rerr := rt.ResolveResource(resource); rerr == nil {
				rt.Pool.Unwatch(ref.GVR, namespace, h.watchID(key))
			}
		}
	}
}

// RemoveConn 连接断开时清理其全部订阅。
// 普通资源的 informer 引用计数必须在此回收(与 Unsubscribe 一致):
// 否则残留 handler 永久占住 watcher id,后续同 key 订阅将报 duplicate 50100。
func (h *WatchHub) RemoveConn(conn WatchConn) {
	h.mu.Lock()
	keys := make([]subKey, 0, len(h.conns[conn]))
	for k := range h.conns[conn] {
		keys = append(keys, k)
	}
	delete(h.conns, conn)
	delete(h.clusterSubs, conn)
	delete(h.auditSubs, conn)
	// 需要池级 Unwatch 的 key:该 key 的最后一个订阅者消失,且非 clusters/audit。
	needUnwatch := make([]subKey, 0, len(keys))
	for _, k := range keys {
		if st, ok := h.subs[k]; ok {
			delete(st.conns, conn)
			if len(st.conns) == 0 {
				delete(h.subs, k)
				if st.stop != nil {
					st.stop()
				}
				if k.resource != "clusters" && k.resource != "audit" {
					needUnwatch = append(needUnwatch, k)
				}
			}
		}
	}
	h.mu.Unlock()
	for _, k := range needUnwatch {
		rt, err := h.mgr.Get(k.cluster)
		if err != nil {
			continue
		}
		ref, err := rt.ResolveResource(k.resource)
		if err != nil {
			continue
		}
		rt.Pool.Unwatch(ref.GVR, k.namespace, h.watchID(k))
	}
}

// deliver 将 informer 事件投递给该订阅维度的全部连接。
func (h *WatchHub) deliver(key subKey, action, namespace, name string, obj any) {
	h.mu.Lock()
	st, ok := h.subs[key]
	if !ok {
		h.mu.Unlock()
		return
	}
	conns := make([]WatchConn, 0, len(st.conns))
	for c := range st.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	env := wsx.NewEnvelope(wsx.TypeEvent, "", wsx.EventPayload{
		Resource:  key.resource,
		Cluster:   key.cluster,
		Namespace: namespace,
		Name:      name,
		Action:    action,
		Object:    obj,
	})
	for _, c := range conns {
		c.Send(env)
	}
}

// publishClusterStatus 推送集群状态迁移到 clusters 订阅者(Manager 监听器回调)。
func (h *WatchHub) publishClusterStatus(ch k8s.StatusChange) {
	h.mu.Lock()
	conns := make([]WatchConn, 0, len(h.clusterSubs))
	filters := make(map[WatchConn]string, len(h.clusterSubs))
	for c, f := range h.clusterSubs {
		conns = append(conns, c)
		filters[c] = f
	}
	h.mu.Unlock()
	if len(conns) == 0 {
		return
	}
	env := wsx.NewEnvelope(wsx.TypeEvent, "", wsx.EventPayload{
		Resource: "clusters",
		Cluster:  ch.Name,
		Name:     ch.Name,
		Action:   wsx.ActionModified,
		Object:   ch,
	})
	for _, c := range conns {
		if f := filters[c]; f == "" || f == ch.Name {
			c.Send(env)
		}
	}
}

// pushClusterSnapshot 订阅成功即推送一次当前快照(websocket.md §1.2)。
func (h *WatchHub) pushClusterSnapshot(conn WatchConn, cluster string) {
	names := []string{}
	if cluster != "" {
		names = append(names, cluster)
	} else {
		names = h.mgr.Names()
	}
	for _, n := range names {
		if ch, ok := h.mgr.Snapshot(n); ok {
			conn.Send(wsx.NewEnvelope(wsx.TypeEvent, "", wsx.EventPayload{
				Resource: "clusters", Cluster: n, Name: n,
				Action: wsx.ActionModified, Object: ch,
			}))
		}
	}
}

// pumpAudit 转发审计事件流到 audit 订阅者。
func (h *WatchHub) pumpAudit() {
	for ev := range h.auditCh {
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			return
		}
		conns := make([]WatchConn, 0, len(h.auditSubs))
		for c := range h.auditSubs {
			conns = append(conns, c)
		}
		h.mu.Unlock()
		env := wsx.NewEnvelope(wsx.TypeEvent, "", wsx.EventPayload{
			Resource: "audit",
			Name:     ev.Username,
			Action:   wsx.ActionAdded,
			Object:   ev,
		})
		for _, c := range conns {
			c.Send(env)
		}
	}
}

// subscribePodLogs 启动 follow 日志跟随协程,逐块作为 event 帧推送。
func (h *WatchHub) subscribePodLogs(conn WatchConn, key subKey) error {
	h.mu.Lock()
	if _, dup := h.subs[key]; dup {
		h.mu.Unlock()
		return errcode.New(errcode.ParamInvalid, "podlogs subscription already active")
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &subState{conns: map[WatchConn]wsx.SubscribePayload{conn: {}}, stop: cancel}
	h.subs[key] = st
	h.addConnSub(conn, key)
	h.mu.Unlock()
	// 受控生命周期 worker:ctx 由退订/Stop 取消,goroutine 随之退出。
	go followPodLogs(ctx, h, key, conn)
	return nil
}

// followPodLogs 读取 follow 日志流并按块推送;退出条件:ctx 取退订取消或流结束。
// 由 subscribePodLogs 启动,退订/Hub.Stop 负责 ctx 取消。
func followPodLogs(ctx context.Context, h *WatchHub, key subKey, conn WatchConn) {
	rd, cancel, err := h.logs.FollowPodLogs(ctx, key.cluster, key.namespace, key.name, "")
	if err != nil {
		logx.Warn(ctx, "podlogs follow open failed", "cluster", key.cluster,
			"namespace", key.namespace, "resource_type", "podlogs", "name", key.name, "err", err)
		code := errcode.InternalError
		if ec := errcode.From(err); ec != nil {
			code = ec.Code
		}
		conn.Send(wsx.NewEnvelope(wsx.TypeError, "", wsx.ErrorPayload{Code: code, Message: err.Error()}))
		h.Unsubscribe(conn, key.cluster, key.resource, key.namespace, key.name)
		return
	}
	defer cancel()
	buf := make([]byte, 4096)
	for {
		n, rerr := rd.Read(buf)
		if n > 0 {
			h.deliver(key, wsx.ActionAdded, key.namespace, key.name, map[string]string{"text": string(buf[:n])})
		}
		if rerr != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// addConnSub 记录连接的订阅集合(调用方持锁)。
func (h *WatchHub) addConnSub(conn WatchConn, key subKey) {
	if h.conns[conn] == nil {
		h.conns[conn] = make(map[subKey]struct{})
	}
	h.conns[conn][key] = struct{}{}
}

// removeConnSubLocked 移除连接订阅记录(调用方持锁)。
func (h *WatchHub) removeConnSubLocked(conn WatchConn, key subKey) {
	if set, ok := h.conns[conn]; ok {
		delete(set, key)
	}
}

func (h *WatchHub) watchID(key subKey) string {
	return key.cluster + "|" + key.resource + "|" + key.namespace + "|" + key.name
}
