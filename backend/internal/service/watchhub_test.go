package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/rest"

	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/pkg/wsx"
)

// fakeConn 是测试用 WatchConn,收集收到的帧。
type fakeConn struct {
	id  string
	mu  sync.Mutex
	env []wsx.Envelope
}

// ID 实现 WatchConn。
func (c *fakeConn) ID() string { return c.id }

// Send 实现 WatchConn。
func (c *fakeConn) Send(env wsx.Envelope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env = append(c.env, env)
	return true
}

// frames 返回收到的全部帧快照。
func (c *fakeConn) frames() []wsx.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]wsx.Envelope, len(c.env))
	copy(out, c.env)
	return out
}

// waitForEvent 阻塞等待指定 resource+name 的事件帧(轮询 + 超时,不 sleep 等待)。
func waitForEvent(t *testing.T, c *fakeConn, resource, name string) wsx.EventPayload {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		for _, env := range c.frames() {
			if env.Type != wsx.TypeEvent {
				continue
			}
			var p wsx.EventPayload
			if err := json.Unmarshal(env.Payload, &p); err == nil && p.Resource == resource && p.Name == name {
				return p
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for event %s/%s", resource, name)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitForEventAction 等待指定 resource+name+action 的事件帧。
func waitForEventAction(t *testing.T, c *fakeConn, resource, name, action string) wsx.EventPayload {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		for _, env := range c.frames() {
			if env.Type != wsx.TypeEvent {
				continue
			}
			var p wsx.EventPayload
			if err := json.Unmarshal(env.Payload, &p); err == nil &&
				p.Resource == resource && p.Name == name && p.Action == action {
				return p
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for event %s/%s/%s", resource, name, action)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestWatchHubSubscribeBroadcastUnsubscribe 验证 hub 广播与退订:
// 多路订阅共享同一 informer;事件广播给全部订阅者;退订互不影响。
func TestWatchHubSubscribeBroadcastUnsubscribe(t *testing.T) {
	mgr, _, dyn, _, _ := newTestEnv(t)
	hub := NewWatchHub(mgr, nil)
	defer hub.Stop()

	c1, c2 := &fakeConn{id: "c1"}, &fakeConn{id: "c2"}
	if err := hub.Subscribe(c1, wsx.SubscribePayload{Cluster: "c1", Namespace: "default", Resource: "pods"}); err != nil {
		t.Fatalf("subscribe c1: %v", err)
	}
	if err := hub.Subscribe(c2, wsx.SubscribePayload{Cluster: "c1", Namespace: "default", Resource: "pods"}); err != nil {
		t.Fatalf("subscribe c2: %v", err)
	}
	// 首个订阅触发懒启动 informer:等待 tracker watch 建立(注入对象必然有事件)。
	pod := newPodObj("web", "default", nil)
	if _, err := dyn.Resource(podsGVR()).Namespace("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	ev := waitForEvent(t, c1, "pods", "web")
	if ev.Action != wsx.ActionAdded || ev.Cluster != "c1" || ev.Namespace != "default" {
		t.Fatalf("event = %+v", ev)
	}
	_ = waitForEvent(t, c2, "pods", "web") // 广播给全部订阅者

	// 退订 c1 后 c2 仍可收到事件。
	hub.Unsubscribe(c1, "c1", "pods", "default", "")
	if err := dyn.Resource(podsGVR()).Namespace("default").Delete(context.Background(), "web", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete pod: %v", err)
	}
	// informer 可能因 relist 重复投递 added,按 action 精确等待 deleted。
	del := waitForEventAction(t, c2, "pods", "web", wsx.ActionDeleted)
	if del.Action != wsx.ActionDeleted {
		t.Fatalf("action = %s, want deleted", del.Action)
	}
	// c1 退订后不再收到新事件。
	if _, err := dyn.Resource(podsGVR()).Namespace("default").Create(context.Background(), newPodObj("web2", "default", nil), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create web2: %v", err)
	}
	_ = waitForEventAction(t, c2, "pods", "web2", wsx.ActionAdded)
	for _, env := range c1.frames() {
		var p wsx.EventPayload
		if env.Type == wsx.TypeEvent && json.Unmarshal(env.Payload, &p) == nil && p.Name == "web2" {
			t.Fatal("unsubscribed conn still received events")
		}
	}
}

// TestWatchHubClustersResource 验证 clusters 订阅:状态迁移经 hub 推送为事件帧。
func TestWatchHubClustersResource(t *testing.T) {
	mgr := k8s.NewManager(k8s.Options{HealthDisabled: true})
	hub := NewWatchHub(mgr, nil)
	defer hub.Stop()
	if err := mgr.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	c := &fakeConn{id: "c1"}
	if err := hub.Subscribe(c, wsx.SubscribePayload{Cluster: "c1", Resource: "clusters"}); err != nil {
		t.Fatalf("subscribe clusters: %v", err)
	}
	ev := waitForEvent(t, c, "clusters", "c1")
	if ev.Action != wsx.ActionModified {
		t.Fatalf("action = %s, want modified", ev.Action)
	}
}

// TestWatchHubRejectsInvalidSubscription 表驱动验证非法订阅拒绝。
func TestWatchHubRejectsInvalidSubscription(t *testing.T) {
	mgr := k8s.NewManager(k8s.Options{HealthDisabled: true})
	hub := NewWatchHub(mgr, nil)
	defer hub.Stop()
	c := &fakeConn{id: "c1"}
	tests := []struct {
		name string
		p    wsx.SubscribePayload
	}{
		{name: "unknown resource", p: wsx.SubscribePayload{Cluster: "c1", Resource: "widgets"}},
		{name: "missing cluster", p: wsx.SubscribePayload{Resource: "pods"}},
		{name: "unregistered cluster", p: wsx.SubscribePayload{Cluster: "nope", Resource: "pods"}},
		{name: "podlogs without name", p: wsx.SubscribePayload{Cluster: "c1", Resource: "podlogs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := hub.Subscribe(c, tt.p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
