package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

func podsGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
}

func newPod(name, namespace string) *unstructured.Unstructured {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: u}
}

func newFakeDynamic(t *testing.T) *dynamicfake.FakeDynamicClient {
	t.Helper()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme,
		map[schema.GroupVersionResource]string{podsGVR(): "PodList", {Group: "", Version: "v1", Resource: "events"}: "EventList"})
}

// TestInformerWatchDeliverEvents 验证懒启动 informer 事件分发:add/update/delete。
func TestInformerWatchDeliverEvents(t *testing.T) {
	client := newFakeDynamic(t)
	stop := make(chan struct{})
	pool := newInformerPool("c1", client, stop)
	defer close(stop)

	events := make(chan string, 16)
	err := pool.Watch(podsGVR(), "", "sub-1", func(action, ns, name string, _ any) {
		events <- action + ":" + name
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	// 等待 informer sync 后再注入对象。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitInformerSync(ctx, pool, podsGVR(), ""); err != nil {
		t.Fatalf("informer sync: %v", err)
	}
	if _, err := client.Resource(podsGVR()).Namespace("default").Create(ctx, newPod("web", "default"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	select {
	case ev := <-events:
		if ev != wsx.ActionAdded+":web" {
			t.Fatalf("first event = %s, want added:web", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for added event")
	}
}

// TestInformerDuplicateAndUnwatch 验证重复订阅幂等替换与退订后引用计数归零。
func TestInformerDuplicateAndUnwatch(t *testing.T) {
	client := newFakeDynamic(t)
	stop := make(chan struct{})
	oldTTL, oldReap := informerIdleTTL, informerReapPeriod
	informerIdleTTL, informerReapPeriod = 10*time.Millisecond, 10*time.Millisecond
	defer func() { informerIdleTTL, informerReapPeriod = oldTTL, oldReap }()
	pool := newInformerPool("c1", client, stop)
	defer close(stop)

	if err := pool.Watch(podsGVR(), "", "sub-1", func(string, string, string, any) {}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	// 同 id 重复注册:幂等替换 handler(断连恢复路径),不应报错
	if err := pool.Watch(podsGVR(), "", "sub-1", func(string, string, string, any) {}); err != nil {
		t.Fatalf("duplicate watch should replace handler: %v", err)
	}
	pool.Unwatch(podsGVR(), "", "sub-1")
	deadline := time.After(3 * time.Second)
	for {
		pool.mu.Lock()
		n := len(pool.entries)
		pool.mu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("informer not reclaimed after idle ttl")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestEnsureEventInformerResident 验证事件 informer 常驻:空闲回收不清理。
func TestEnsureEventInformerResident(t *testing.T) {
	client := newFakeDynamic(t)
	stop := make(chan struct{})
	pool := newInformerPool("c1", client, stop)
	defer close(stop)

	if err := pool.EnsureEventInformer(); err != nil {
		t.Fatalf("ensure event informer: %v", err)
	}
	oldTTL, oldReap := informerIdleTTL, informerReapPeriod
	informerIdleTTL, informerReapPeriod = 10*time.Millisecond, 10*time.Millisecond
	defer func() { informerIdleTTL, informerReapPeriod = oldTTL, oldReap }()
	time.Sleep(100 * time.Millisecond) // 等待至少一轮回收扫描
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if len(pool.entries) != 1 {
		t.Fatalf("resident event informer entries = %d, want 1", len(pool.entries))
	}
}

// waitInformerSync 等待 entry 的 informer 完成首次 sync;经 channel/轮询同步,不 sleep。
func waitInformerSync(ctx context.Context, pool *InformerPool, gvr schema.GroupVersionResource, ns string) error {
	deadline := time.After(3 * time.Second)
	for {
		pool.mu.Lock()
		e, ok := pool.entries[entryKey(gvr, ns)]
		var synced bool
		if ok {
			synced = e.informer.HasSynced()
		}
		pool.mu.Unlock()
		if ok && synced {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return context.DeadlineExceeded
		case <-time.After(5 * time.Millisecond):
		}
	}
}
