package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
)

// maxLogBytes 单次历史日志读取上限(16MB),防止超大日志打爆内存。
const maxLogBytes = 16 << 20

// dynList 按作用域执行 dynamic 列表,空 namespace 表示全命名空间/集群级。
func dynList(ctx context.Context, rt *k8s.ClusterRuntime, ref k8s.ResourceRef, namespace, labelSelector, fieldSelector string) (*unstructured.UnstructuredList, error) {
	opts := metav1.ListOptions{LabelSelector: labelSelector, FieldSelector: fieldSelector}
	if ref.Namespaced && namespace != "" {
		return rt.Dyn.Resource(ref.GVR).Namespace(namespace).List(ctx, opts)
	}
	return rt.Dyn.Resource(ref.GVR).List(ctx, opts)
}

// dynGet 按作用域读取单个对象。
func dynGet(ctx context.Context, rt *k8s.ClusterRuntime, ref k8s.ResourceRef, namespace, name string) (*unstructured.Unstructured, error) {
	if ref.Namespaced {
		return rt.Dyn.Resource(ref.GVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	return rt.Dyn.Resource(ref.GVR).Get(ctx, name, metav1.GetOptions{})
}

// readStreamer 是日志流的最小读接口(GetLogs Stream 返回 io.ReadCloser)。
type readStreamer interface {
	Read(p []byte) (int, error)
}

// readAllLimit 读取流内容,超过 limit 即报错,防止内存放大。
func readAllLimit(r readStreamer, limit int64) ([]byte, error) {
	lr := io.LimitReader(struct{ io.Reader }{r}, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("log output exceeds limit")
	}
	return data, nil
}

// toResourceItem 从 unstructured 对象提取通用摘要。
func toResourceItem(o *unstructured.Unstructured) model.ResourceItem {
	item := model.ResourceItem{
		Kind:      o.GetKind(),
		Name:      o.GetName(),
		Namespace: o.GetNamespace(),
		Status:    extractStatus(o),
		Labels:    o.GetLabels(),
	}
	if !o.GetCreationTimestamp().Time.IsZero() {
		t := o.GetCreationTimestamp().Time
		item.CreatedAt = &t
	}
	return item
}

// extractStatus 按资源类型提取摘要状态。
func extractStatus(o *unstructured.Unstructured) string {
	switch o.GetKind() {
	case "Pod":
		phase, _, _ := unstructured.NestedString(o.Object, "status", "phase")
		return phase
	case "Deployment":
		ready, _, _ := unstructured.NestedInt64(o.Object, "status", "readyReplicas")
		want, _, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
		return fmt.Sprintf("%d/%d", ready, want)
	case "StatefulSet":
		ready, _, _ := unstructured.NestedInt64(o.Object, "status", "readyReplicas")
		want, _, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
		return fmt.Sprintf("%d/%d", ready, want)
	case "DaemonSet":
		ready, _, _ := unstructured.NestedInt64(o.Object, "status", "numberReady")
		want, _, _ := unstructured.NestedInt64(o.Object, "status", "desiredNumberScheduled")
		return fmt.Sprintf("%d/%d", ready, want)
	case "Node":
		conditions, found, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
		if !found {
			return ""
		}
		for _, c := range conditions {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == string(corev1.NodeReady) {
				return fmt.Sprintf("%v", m["status"])
			}
		}
		return ""
	case "PersistentVolumeClaim", "PersistentVolume":
		phase, _, _ := unstructured.NestedString(o.Object, "status", "phase")
		return phase
	default:
		return ""
	}
}

// sortItems 按 sortBy 白名单排序;非法值返回 40001。
func sortItems(items []model.ResourceItem, sortBy, order string) error {
	if sortBy == "" {
		sortBy = "createdAt"
	}
	desc := true
	switch order {
	case "", "desc":
	case "asc":
		desc = false
	default:
		return errcode.New(errcode.ParamInvalid, "invalid order, expect asc or desc")
	}
	less := func(a, b string) bool {
		if desc {
			return a > b
		}
		return a < b
	}
	switch sortBy {
	case "name":
		sort.SliceStable(items, func(i, j int) bool { return less(items[i].Name, items[j].Name) })
	case "namespace":
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Namespace == items[j].Namespace {
				return less(items[i].Name, items[j].Name)
			}
			return less(items[i].Namespace, items[j].Namespace)
		})
	case "createdAt":
		sort.SliceStable(items, func(i, j int) bool {
			ti, tj := tsOf(items[i]), tsOf(items[j])
			if desc {
				return ti.After(tj)
			}
			return ti.Before(tj)
		})
	default:
		return errcode.New(errcode.ParamInvalid, "invalid sortBy, expect name|namespace|createdAt")
	}
	return nil
}

func tsOf(i model.ResourceItem) time.Time {
	if i.CreatedAt != nil {
		return *i.CreatedAt
	}
	return time.Time{}
}

// toUnstructured 将 typed 对象转 unstructured(统一返回形态)。
func toUnstructured(obj runtime.Object) *unstructured.Unstructured {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return &unstructured.Unstructured{}
	}
	return &unstructured.Unstructured{Object: u}
}

// isMetricsUnavailable 判定 metrics-server 未安装/不可达的典型错误形态。
func isMetricsUnavailable(err error) bool {
	return apierrors.IsNotFound(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsBadRequest(err) ||
		apierrors.IsMethodNotSupported(err)
}

// NodeMetricsList 返回节点资源快照;metrics-server 未安装时映射 50100,
// message 明确注明 metrics-server not available(rest.md §5)。
func (s *ResourceService) nodeMetricsList(ctx context.Context, rt *k8s.ClusterRuntime) ([]model.NodeMetrics, error) {
	list, err := rt.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		if isMetricsUnavailable(err) {
			return nil, errcode.New(errcode.UpstreamK8sError, "metrics-server not available").WithCause(err)
		}
		return nil, mapK8sError(rt.Name, err)
	}
	// 使用率分母取节点 allocatable(容量 - 系统预留),经 O-01 用例要求回填百分比。
	alloc, aerr := s.nodeAllocatable(ctx, rt)
	if aerr != nil {
		logx.Warn(ctx, "fetch node allocatable failed; metrics without pct",
			"cluster", rt.Name, "err", aerr)
	}
	out := make([]model.NodeMetrics, 0, len(list.Items))
	for i := range list.Items {
		m := &list.Items[i]
		nm := model.NodeMetrics{
			Name:   m.Name,
			CPU:    m.Usage.Cpu().String(),
			Memory: m.Usage.Memory().String(),
		}
		if a, ok := alloc[m.Name]; ok {
			nm.CPUPct = pctOf(m.Usage.Cpu(), a.cpu)
			nm.MemPct = pctOf(m.Usage.Memory(), a.memory)
		}
		out = append(out, nm)
	}
	return out, nil
}

// nodeAlloc is a node's allocatable CPU/memory quantities.
type nodeAlloc struct {
	cpu    *k8sresource.Quantity
	memory *k8sresource.Quantity
}

// nodeAllocatable 拉取全部节点并按名返回 allocatable CPU/内存。
func (s *ResourceService) nodeAllocatable(ctx context.Context, rt *k8s.ClusterRuntime) (map[string]nodeAlloc, error) {
	nodes, err := rt.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	out := make(map[string]nodeAlloc, len(nodes.Items))
	for i := range nodes.Items {
		n := &nodes.Items[i]
		cpu := n.Status.Allocatable.Cpu()
		mem := n.Status.Allocatable.Memory()
		out[n.Name] = nodeAlloc{cpu: cpu, memory: mem}
	}
	return out, nil
}

// pctOf 计算 usage/allocatable 百分比,保留一位小数(如 "12.5%");分母无效返回空。
func pctOf(usage, allocatable *k8sresource.Quantity) string {
	if allocatable == nil || usage == nil {
		return ""
	}
	total := allocatable.AsApproximateFloat64()
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", usage.AsApproximateFloat64()/total*100)
}

// podMetricsList 返回 Pod 资源快照(namespace 为空取全部)。
func (s *ResourceService) podMetricsList(ctx context.Context, rt *k8s.ClusterRuntime, namespace string) ([]model.PodMetrics, error) {
	list, err := rt.Metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if isMetricsUnavailable(err) {
			return nil, errcode.New(errcode.UpstreamK8sError, "metrics-server not available").WithCause(err)
		}
		return nil, mapK8sError(rt.Name, err)
	}
	out := make([]model.PodMetrics, 0, len(list.Items))
	for i := range list.Items {
		m := &list.Items[i]
		pm := model.PodMetrics{Namespace: m.Namespace, Name: m.Name, Containers: make([]model.ContainerStat, 0, len(m.Containers))}
		for _, c := range m.Containers {
			pm.Containers = append(pm.Containers, model.ContainerStat{
				Name:   c.Name,
				CPU:    c.Usage.Cpu().String(),
				Memory: c.Usage.Memory().String(),
			})
		}
		out = append(out, pm)
	}
	return out, nil
}

// filterByKeyword 按 keyword 对名称做 contains 模糊过滤(大小写不敏感)。
func filterByKeyword(items []model.ResourceItem, keyword string) []model.ResourceItem {
	if keyword == "" {
		return items
	}
	kw := strings.ToLower(keyword)
	out := make([]model.ResourceItem, 0, len(items))
	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Name), kw) {
			out = append(out, items[i])
		}
	}
	return out
}
