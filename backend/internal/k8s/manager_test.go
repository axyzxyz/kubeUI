package k8s

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamic "k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8stesting "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/axyzxyz/kubeui/backend/internal/model"
)

// fakeMapper 是测试用空 RESTMapper。
type fakeMapper struct{ *meta.DefaultRESTMapper }

// Reset 实现 ResettableRESTMapper。
func (fakeMapper) Reset() {}

// waitForStatus 阻塞等待指定状态迁移事件,超时失败;经 channel 同步,不 sleep。
func waitForStatus(t *testing.T, ch <-chan StatusChange, want string) StatusChange {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case sc := <-ch:
			if sc.Status == want {
				return sc
			}
		case <-deadline:
			t.Fatalf("timed out waiting for status %s", want)
		}
	}
}

// newTestManager 构造注入探针与 fake 客户端的 manager。
func newTestManager(t *testing.T, probe func(context.Context, *ClusterRuntime, time.Duration) error) (*Manager, <-chan StatusChange) {
	t.Helper()
	ch := make(chan StatusChange, 64)
	m := NewManager(Options{
		HealthInterval: 10 * time.Millisecond,
		ProbeTimeout:   50 * time.Millisecond,
		BackoffStart:   5 * time.Millisecond,
		Probe:          probe,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme,
				map[schema.GroupVersionResource]string{
					{Group: "", Version: "v1", Resource: "pods"}: "PodList",
				})
			return k8stesting.NewSimpleClientset(), dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	m.AddStatusListener(func(sc StatusChange) { ch <- sc })
	return m, ch
}

// TestHealthStateMachine 验证健康状态机:连续失败 → Degraded(3)→ Reconnecting(10),
// 恢复 → Ready;探活周期与退避经 Options 缩短。
func TestHealthStateMachine(t *testing.T) {
	var (
		mu    sync.Mutex
		fails int
	)
	probe := func(context.Context, *ClusterRuntime, time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		fails++
		if fails <= 12 {
			return errors.New("probe boom")
		}
		return nil
	}
	m, ch := newTestManager(t, probe)
	defer m.Close(context.Background())
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	waitForStatus(t, ch, model.ClusterStatusDegraded)
	waitForStatus(t, ch, model.ClusterStatusReconnecting)
	waitForStatus(t, ch, model.ClusterStatusReady)
	mu.Lock()
	defer mu.Unlock()
	if fails < 13 {
		t.Fatalf("probe calls = %d, want >= 13", fails)
	}
}

// TestRegisterUnregister 验证注册/注销串联:注销后 Get 失败且 goroutine 已退出。
func TestRegisterUnregister(t *testing.T) {
	m := NewManager(Options{HealthDisabled: true})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := m.Get("c1"); err != nil {
		t.Fatalf("get after register: %v", err)
	}
	if _, ok := m.Snapshot("c1"); !ok {
		t.Fatal("snapshot missing after register")
	}
	m.Unregister(context.Background(), "c1")
	if _, err := m.Get("c1"); err == nil {
		t.Fatal("get after unregister should fail")
	}
	if got := m.Names(); len(got) != 0 {
		t.Fatalf("names after unregister = %v, want empty", got)
	}
}

// TestSetDialer 挂载点验证:注入 Dialer 后 Register 将其挂到 rest.Config.Dial。
func TestSetDialer(t *testing.T) {
	m := NewManager(Options{HealthDisabled: true})
	m.SetDialer("c1", NetDialer{Timeout: time.Second})
	cfg := &rest.Config{}
	if err := m.Register(context.Background(), "c1", "v1.28", cfg); err != nil {
		t.Fatalf("register: %v", err)
	}
	if cfg.Dial == nil {
		t.Fatal("rest.Config.Dial not set after SetDialer")
	}
	m.SetDialer("c1", nil)
	cfg2 := &rest.Config{}
	if err := m.Register(context.Background(), "c1", "v1.28", cfg2); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if cfg2.Dial != nil {
		t.Fatal("rest.Config.Dial should be cleared")
	}
}

// TestStatusListenerUnsubscribe 验证监听器退订后不再接收迁移事件。
func TestStatusListenerUnsubscribe(t *testing.T) {
	m := NewManager(Options{HealthDisabled: true})
	defer m.Close(context.Background())
	events := make(chan StatusChange, 8)
	unsub := m.AddStatusListener(func(sc StatusChange) { events <- sc })
	unsub()
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	select {
	case sc := <-events:
		t.Fatalf("received event after unsubscribe: %+v", sc)
	case <-time.After(50 * time.Millisecond):
	}
}
