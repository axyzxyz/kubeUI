package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/base64"
	"encoding/json"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	dynamic "k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
	"time"

	"github.com/axyzxyz/kubeui/backend/internal/k8s"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
)

// base64StdEncode 编码字符串为标准 base64。
func base64StdEncode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// appsv1Deployment 构造最小 Deployment 对象。
func appsv1Deployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// fakeMapper 是测试用空 RESTMapper。
type fakeMapper struct{ *meta.DefaultRESTMapper }

// Reset 实现 ResettableRESTMapper。
func (fakeMapper) Reset() {}

func podsGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
}

// newTestEnv 构造注入 fake 客户端的 manager 与资源服务;返回 dynamic fake 以便
// 注入 reactor 与断言。
func newTestEnv(t *testing.T) (*k8s.Manager, *ResourceService, *dynamicfake.FakeDynamicClient, *fake.Clientset, *metricsfake.Clientset) {
	t.Helper()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podsGVR(): "PodList",
			{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
			{Group: "", Version: "v1", Resource: "events"}:          "EventList",
		})
	cs := fake.NewSimpleClientset()
	mc := metricsfake.NewSimpleClientset()
	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			return cs, dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
		NewMetrics: func(*rest.Config) (metricsv.Interface, error) { return mc, nil },
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return m, NewResourceService(m), dyn, cs, mc
}

func newPodObj(name, namespace string, labels map[string]string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion("v1")
	o.SetKind("Pod")
	o.SetName(name)
	o.SetNamespace(namespace)
	o.SetLabels(labels)
	return o
}

func strPtr(s string) *string { return &s }

// TestResourceList 表驱动验证列表过滤、排序白名单与内存分页。
func TestResourceList(t *testing.T) {
	_, svc, dyn, _, _ := newTestEnv(t)
	// 注入 3 个 pod:两个带标签,一个不带;不同创建时间。
	now := metav1.Now()
	older := metav1.NewTime(now.Add(-2 * time.Hour))
	pods := []runtime.Object{}
	for i, name := range []string{"a", "b", "c"} {
		p := newPodObj(name, "default", map[string]string{"app": "web"})
		if i == 0 {
			p.SetCreationTimestamp(older)
		} else {
			p.SetCreationTimestamp(now)
		}
		pods = append(pods, p)
	}
	pods = append(pods, newPodObj("x", "default", nil))
	for i := range pods {
		if err := dyn.Tracker().Add(pods[i]); err != nil {
			t.Fatalf("add pod: %v", err)
		}
	}
	ctx := context.Background()

	tests := []struct {
		name      string
		q         ListQuery
		wantCount int
		wantFirst string
		wantErr   int
	}{
		{name: "all pods", q: ListQuery{Page: 1, Size: 20}, wantCount: 4},
		{name: "label selector filters", q: ListQuery{LabelSelector: "app=web", Page: 1, Size: 20}, wantCount: 3},
		{name: "page 2 size 2", q: ListQuery{Page: 2, Size: 2}, wantCount: 4},
		{name: "sort by name asc", q: ListQuery{SortBy: "name", Order: "asc", Page: 1, Size: 20}, wantFirst: "a", wantCount: 4},
		{name: "sort by name desc", q: ListQuery{SortBy: "name", Order: "desc", Page: 1, Size: 20}, wantFirst: "x", wantCount: 4},
		{name: "invalid sortBy", q: ListQuery{SortBy: "evil", Page: 1, Size: 20}, wantErr: errcode.ParamInvalid},
		{name: "invalid order", q: ListQuery{Order: "sideways", Page: 1, Size: 20}, wantErr: errcode.ParamInvalid},
		{name: "unknown cluster", q: ListQuery{}, wantErr: errcode.ClusterNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := "c1"
			if tt.name == "unknown cluster" {
				cluster = "nope"
			}
			body, err := svc.List(ctx, cluster, "pods", tt.q)
			if tt.wantErr != 0 {
				ec := errcode.From(err)
				if ec == nil || ec.Code != tt.wantErr {
					t.Fatalf("err = %v, want code %d", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if int(body.Total) != tt.wantCount {
				t.Fatalf("total = %d, want %d", body.Total, tt.wantCount)
			}
			if tt.wantFirst != "" && len(body.Items) > 0 && body.Items[0].Name != tt.wantFirst {
				t.Fatalf("first = %s, want %s", body.Items[0].Name, tt.wantFirst)
			}
		})
	}
}

// TestResourceSecretRedaction 验证 Secret 默认脱敏与 reveal 解码。
func TestResourceSecretRedaction(t *testing.T) {
	_, svc, dyn, _, _ := newTestEnv(t)
	sec := &unstructured.Unstructured{}
	sec.SetAPIVersion("v1")
	sec.SetKind("Secret")
	sec.SetName("s1")
	sec.SetNamespace("default")
	encoded := base64StdEncode("topsecret")
	_ = unstructured.SetNestedMap(sec.Object, map[string]any{"password": encoded}, "data")
	if err := dyn.Tracker().Add(sec); err != nil {
		t.Fatalf("add secret: %v", err)
	}
	ctx := context.Background()
	redacted, err := svc.Get(ctx, "c1", "secrets", "default", "s1", false)
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if v, _, _ := unstructured.NestedString(redacted.Object, "data", "password"); v != "***" {
		t.Fatalf("redacted value = %q, want ***", v)
	}
	data, err := svc.SecretData(ctx, "c1", "default", "s1")
	if err != nil {
		t.Fatalf("secret data: %v", err)
	}
	if data["password"] != "topsecret" {
		t.Fatalf("decoded = %q, want topsecret", data["password"])
	}
}

// TestUpdateYAMLConflict 验证 YAML 更新的冲突映射(40406)与名称校验。
func TestUpdateYAMLConflict(t *testing.T) {
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podsGVR(): "PodList",
			{Group: "", Version: "v1", Resource: "events"}: "EventList",
		})
	// reactor 必须在 Register(启动常驻事件 informer)之前安装,避免与 reflector 并发竞争。
	dyn.PrependReactor("update", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "web", errors.New("rv conflict"))
	})
	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			return fake.NewSimpleClientset(), dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewResourceService(m)
	ctx := context.Background()
	yamlText := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\n  namespace: default\n"
	if _, err := svc.UpdateYAML(ctx, "c1", "pods", "default", "web", yamlText); err == nil {
		t.Fatal("expected conflict error")
	} else if ec := errcode.From(err); ec == nil || ec.Code != errcode.ResourceConflict {
		t.Fatalf("err = %v, want code %d", err, errcode.ResourceConflict)
	}
	// 名称不匹配 → 40001。
	if _, err := svc.UpdateYAML(ctx, "c1", "pods", "default", "other", yamlText); err == nil {
		t.Fatal("expected name mismatch error")
	} else if ec := errcode.From(err); ec == nil || ec.Code != errcode.ParamInvalid {
		t.Fatalf("err = %v, want %d", err, errcode.ParamInvalid)
	}
}

// TestDeploymentRestartAndScale 用 reactor 捕获 patch 验证 restartedAt 与 scale。
// 回归 BUG-01:StrategicMergePatchType 要求 JSON 体,patch body 必须是合法 JSON。
func TestDeploymentRestartAndScale(t *testing.T) {
	_, svc, _, cs, _ := newTestEnv(t)
	var gotPatches []string
	cs.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		pa := action.(k8stesting.PatchActionImpl)
		gotPatches = append(gotPatches, string(pa.Patch))
		d := appsv1Deployment("web", pa.GetNamespace())
		return true, d, nil
	})
	ctx := context.Background()
	if _, err := svc.RestartDeployment(ctx, "c1", "default", "web"); err != nil {
		t.Fatalf("restart deployment: %v", err)
	}
	if len(gotPatches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(gotPatches))
	}
	if !json.Valid([]byte(gotPatches[0])) {
		t.Fatalf("restart patch body is not valid JSON (BUG-01): %q", gotPatches[0])
	}
	if !strings.Contains(gotPatches[0], "kubectl.kubernetes.io/restartedAt") {
		t.Fatalf("patch = %v, want restartedAt annotation", gotPatches)
	}
	if _, err := svc.ScaleDeployment(ctx, "c1", "default", "web", 3); err != nil {
		t.Fatalf("scale deployment: %v", err)
	}
	if len(gotPatches) != 2 || !strings.Contains(gotPatches[1], `"replicas":3`) {
		t.Fatalf("scale patch = %v", gotPatches)
	}
	if !json.Valid([]byte(gotPatches[1])) {
		t.Fatalf("scale patch body is not valid JSON: %q", gotPatches[1])
	}
}

// TestPodLogsViaHTTPServer 经 httptest 模拟 apiserver 日志端点验证历史日志。
func TestPodLogsViaHTTPServer(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:gosimple // 占位避免未使用(下方真正 clientset 经 NewClients 构建)
	_ = cs
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/log") {
			_, _ = w.Write([]byte("line1\nline2\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podsGVR(): "PodList",
			{Group: "", Version: "v1", Resource: "events"}: "EventList",
		})

	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(cfg *rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			// clientset 指向 httptest apiserver,日志流走真实 HTTP。
			realCS, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
			if err != nil {
				return nil, nil, nil, err
			}
			return realCS, dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewResourceService(m)
	logs, err := svc.PodLogs(context.Background(), "c1", "default", "web", PodLogsQuery{TailLines: 100})
	if err != nil {
		t.Fatalf("pod logs: %v", err)
	}
	if !strings.Contains(logs.Logs, "line1") {
		t.Fatalf("logs = %q", logs.Logs)
	}
}

// TestMetricsUnavailable 验证 metrics-server 未安装时返回 50100。
func TestMetricsUnavailable(t *testing.T) {
	_, svc, _, _, mc := newTestEnv(t) //nolint:dogsled // 测试忽略其余返回值
	mc.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodemetrics"}, "")
	})
	_, err := svc.NodeMetrics(context.Background(), "c1")
	ec := errcode.From(err)
	if ec == nil || ec.Code != errcode.UpstreamK8sError || ec.Message != "metrics-server not available" {
		t.Fatalf("err = %v, want 50100 metrics-server not available", err)
	}
}

// TestFollowPodLogsStreaming 用 httptest 分块响应模拟日志流,验证 follow 流读取。
func TestFollowPodLogsStreaming(t *testing.T) {
	firstChunk := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("chunk-1\n"))
		flusher.Flush()
		close(firstChunk)
		<-r.Context().Done() // 保持连接,直到客户端取消
	}))
	defer srv.Close()

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podsGVR(): "PodList",
			{Group: "", Version: "v1", Resource: "events"}: "EventList",
		})
	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			realCS, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
			if err != nil {
				return nil, nil, nil, err
			}
			return realCS, dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewResourceService(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rd, stop, err := svc.FollowPodLogs(ctx, "c1", "default", "web", "")
	if err != nil {
		t.Fatalf("follow logs: %v", err)
	}
	defer stop()

	buf := make([]byte, 64)
	n, err := rd.Read(buf)
	if err != nil || string(buf[:n]) != "chunk-1\n" {
		t.Fatalf("first read = %q, err = %v", buf[:n], err)
	}
	<-firstChunk
	cancel() // 客户端取消 → 流终止
}

// discoveryMapper 在 fakeMapper 基础上补齐 discovery 能力,用于 CRD plural
// 解析透传的测试(仅覆盖 ServerPreferredResources,其余接口保持 nil)。
type discoveryMapper struct {
	fakeMapper
	discovery.DiscoveryInterface
	preferred []*metav1.APIResourceList
}

// ServerPreferredResources 返回预置的 API 资源列表。
func (m discoveryMapper) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return m.preferred, nil
}

// newCRDEnv 构造带 discovery 的测试环境:example.org/v1 foos(CRD plural)。
func newCRDEnv(t *testing.T) (*k8s.Manager, *ResourceService, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: "example.org", Version: "v1", Resource: "foos"}: "FooList",
			// 常驻事件 informer 会在 Register 时 List events,fake 必须注册该 list kind
			{Group: "", Version: "v1", Resource: "events"}: "EventList",
		})
	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			dm := discoveryMapper{
				fakeMapper:         fakeMapper{meta.NewDefaultRESTMapper(nil)},
				DiscoveryInterface: nil,
				preferred: []*metav1.APIResourceList{{
					GroupVersion: "example.org/v1",
					APIResources: []metav1.APIResource{{Name: "foos", Namespaced: true, Kind: "Foo"}},
				}},
			}
			return fake.NewSimpleClientset(), dyn, dm, nil
		},
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return m, NewResourceService(m), dyn
}

// TestCreateResource 表驱动验证同构资源创建:JSON/YAML、namespace 优先级、
// 缺 namespace 40001、已存在 40406、CRD plural 透传。
func TestCreateResource(t *testing.T) {
	// reactor 必须在 Register(启动常驻事件 informer)之前安装,避免与 reflector 并发竞争。
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podsGVR(): "PodList",
			{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
			{Group: "", Version: "v1", Resource: "events"}:          "EventList",
		})
	// deployments 已存在 → AlreadyExists(409)。
	dyn.PrependReactor("create", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "apps", Resource: "deployments"}, "web")
	})
	m := k8s.NewManager(k8s.Options{
		HealthDisabled: true,
		NewClients: func(*rest.Config) (kubernetes.Interface, dynamic.Interface, meta.ResettableRESTMapper, error) {
			return fake.NewSimpleClientset(), dyn, fakeMapper{meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	if err := m.Register(context.Background(), "c1", "v1.28", &rest.Config{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewResourceService(m)
	deploymentJSON := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web"},"spec":{"replicas":1}}`
	configmapYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n  namespace: kube-public\ndata:\n  k: v\n"

	tests := []struct {
		name      string
		cluster   string
		resource  string
		namespace string
		body      string
		wantErr   int
		wantName  string
		wantNS    string
	}{
		{
			name:      "create configmap with query namespace",
			cluster:   "c1",
			resource:  "configmaps",
			namespace: "default",
			body:      `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm0"},"data":{"a":"b"}}`,
			wantName:  "cm0",
			wantNS:    "default",
		},
		{
			name:      "yaml body namespace wins over query",
			cluster:   "c1",
			resource:  "configmaps",
			namespace: "default",
			body:      configmapYAML,
			wantName:  "cm1",
			wantNS:    "kube-public",
		},
		{
			name:     "namespaced resource without namespace",
			cluster:  "c1",
			resource: "configmaps",
			body:     `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm2"}}`,
			wantErr:  errcode.ParamInvalid,
		},
		{
			name:      "already exists maps to 40406",
			cluster:   "c1",
			resource:  "deployments",
			namespace: "default",
			body:      deploymentJSON,
			wantErr:   errcode.ResourceConflict,
		},
		{
			name:      "cluster scoped resource ignores namespace",
			cluster:   "c1",
			resource:  "namespaces",
			namespace: "default",
			body:      `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"ns1"}}`,
			wantName:  "ns1",
			wantNS:    "",
		},
		{
			name:      "missing metadata.name",
			cluster:   "c1",
			resource:  "configmaps",
			namespace: "default",
			body:      `{"apiVersion":"v1","kind":"ConfigMap"}`,
			wantErr:   errcode.ParamInvalid,
		},
		{
			name:      "invalid manifest",
			cluster:   "c1",
			resource:  "configmaps",
			namespace: "default",
			body:      "!!!not yaml: [",
			wantErr:   errcode.ParamInvalid,
		},
		{
			name:      "unknown resource plural",
			cluster:   "c1",
			resource:  "unknownkinds",
			namespace: "default",
			body:      `{"apiVersion":"v1","kind":"UnknownKind","metadata":{"name":"x"}}`,
			wantErr:   errcode.ParamInvalid,
		},
		{
			name:     "cluster not found",
			cluster:  "nope",
			resource: "configmaps",
			body:     `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm3"}}`,
			wantErr:  errcode.ClusterNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := svc.CreateResource(context.Background(), tt.cluster, tt.resource, tt.namespace, tt.body)
			if tt.wantErr != 0 {
				ec := errcode.From(err)
				if ec == nil || ec.Code != tt.wantErr {
					t.Fatalf("err = %v, want code %d", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if obj.GetName() != tt.wantName {
				t.Fatalf("name = %s, want %s", obj.GetName(), tt.wantName)
			}
			if obj.GetNamespace() != tt.wantNS {
				t.Fatalf("namespace = %q, want %q", obj.GetNamespace(), tt.wantNS)
			}
		})
	}
}

// TestCreateResourceCRDPlural 验证 CRD plural 经 discovery 解析后透传创建。
func TestCreateResourceCRDPlural(t *testing.T) {
	_, svc, dyn := newCRDEnv(t)
	obj, err := svc.CreateResource(context.Background(), "c1", "foos", "default",
		`{"apiVersion":"example.org/v1","kind":"Foo","metadata":{"name":"f1"}}`)
	if err != nil {
		t.Fatalf("create cr: %v", err)
	}
	if obj.GetName() != "f1" || obj.GetNamespace() != "default" {
		t.Fatalf("created = %s/%s, want f1/default", obj.GetNamespace(), obj.GetName())
	}
	got, err := dyn.Resource(schema.GroupVersionResource{Group: "example.org", Version: "v1", Resource: "foos"}).
		Namespace("default").Get(context.Background(), "f1", metav1.GetOptions{})
	if err != nil || got.GetName() != "f1" {
		t.Fatalf("get created cr: %v", err)
	}
}
