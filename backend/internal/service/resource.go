package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/model"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/pkg/pagination"
)

// k8sCallTimeout 是所有 K8s 出站调用的统一超时(04 §2.3)。
const k8sCallTimeout = 10 * time.Second

// restartedAtAnnotation 是 deployment 重启 patch 的注解键,与 kubectl rollout restart 一致。
const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// ResourceService 提供统一同构的资源浏览能力:列表/详情/YAML/删除/restart/scale/
// logs/events/metrics。全部经 ClusterManager 出口,10s 超时,错误映射
// 40401(集群不存在)/40400(资源不存在)/40406(版本冲突)/50100(上游 K8s 错误)。
type ResourceService struct {
	mgr *k8s.Manager
}

// NewResourceService 构造 ResourceService。
func NewResourceService(mgr *k8s.Manager) *ResourceService { return &ResourceService{mgr: mgr} }

// ListQuery 是资源列表的过滤/排序/分页参数。
type ListQuery struct {
	Namespace     string // 空 = 全部命名空间(集群级资源忽略)
	LabelSelector string // 原样透传 K8s labelSelector
	FieldSelector string // 原样透传 K8s fieldSelector
	SortBy        string // 白名单:name|namespace|createdAt,默认 createdAt
	Order         string // asc|desc,默认 desc
	// Keyword 名称模糊过滤(contains,大小写不敏感);在分页前应用。
	Keyword string
	Page    int
	Size    int
}

// runtimeOf 取集群运行时;未注册映射 40401。
func (s *ResourceService) runtimeOf(cluster string) (*k8s.ClusterRuntime, error) {
	rt, err := s.mgr.Get(cluster)
	if err != nil {
		return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
	}
	return rt, nil
}

// mapK8sError 将 K8s API 错误映射为业务错误码。
func mapK8sError(cluster string, err error) error {
	if err == nil {
		return nil
	}
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return err
	}
	switch {
	case apierrors.IsNotFound(err):
		return errcode.New(errcode.NotFound, "resource not found").WithCause(err)
	case apierrors.IsAlreadyExists(err):
		return errcode.New(errcode.ResourceConflict, "resource already exists").WithCause(err)
	case apierrors.IsConflict(err):
		return errcode.New(errcode.ResourceConflict, "resource version conflict, reload and retry").WithCause(err)
	case apierrors.IsUnauthorized(err), apierrors.IsForbidden(err):
		return errcode.New(errcode.UpstreamK8sError, "cluster apiserver rejected the request").WithCause(err)
	default:
		logx.Error(context.Background(), "upstream k8s call failed", "cluster", cluster, "err", err)
		// K8s 校验类错误(422 Invalid 等)含用户可读的原因(如 spec.containers 必填),
		// 直接透出避免统一 500 文案掩盖真实原因。
		var status *apierrors.StatusError
		if errors.As(err, &status) && status.Status().Message != "" {
			return errcode.New(errcode.UpstreamK8sError, status.Status().Message).WithCause(err)
		}
		return errcode.New(errcode.UpstreamK8sError, "upstream kubernetes error").WithCause(err)
	}
}

// resolve 解析资源 plural 名并校验作用域。
func (s *ResourceService) resolve(rt *k8s.ClusterRuntime, resource string, namespace string, namespacedRequired bool) (k8s.ResourceRef, error) {
	ref, err := rt.ResolveResource(resource)
	if err != nil {
		return k8s.ResourceRef{}, errcode.New(errcode.ParamInvalid, "unknown resource "+resource).WithCause(err)
	}
	if namespacedRequired && !ref.Namespaced {
		return k8s.ResourceRef{}, errcode.New(errcode.ParamInvalid, "resource "+resource+" is cluster-scoped")
	}
	if !ref.Namespaced {
		namespace = ""
	}
	return ref, nil
}

// List 拉取资源列表:labelSelector/fieldSelector 服务端过滤,sortBy 白名单排序,
// 内存分页(全量后分页,支持任意排序;04 §4.3)。
func (s *ResourceService) List(ctx context.Context, cluster, resource string, q ListQuery) (pagination.ResultBody[model.ResourceItem], error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return pagination.ResultBody[model.ResourceItem]{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	ref, err := s.resolve(rt, resource, q.Namespace, false)
	if err != nil {
		return pagination.ResultBody[model.ResourceItem]{}, err
	}
	list, err := dynList(ctx, rt, ref, q.Namespace, q.LabelSelector, q.FieldSelector)
	if err != nil {
		return pagination.ResultBody[model.ResourceItem]{}, mapK8sError(cluster, err)
	}
	items := make([]model.ResourceItem, 0, len(list.Items))
	for i := range list.Items {
		items = append(items, toResourceItem(&list.Items[i]))
	}
	items = filterByKeyword(items, q.Keyword)
	if err := sortItems(items, q.SortBy, q.Order); err != nil {
		return pagination.ResultBody[model.ResourceItem]{}, err
	}
	total := int64(len(items))
	start := (q.Page - 1) * q.Size
	if start > len(items) {
		start = len(items)
	}
	end := start + q.Size
	if end > len(items) {
		end = len(items)
	}
	return pagination.Result(items[start:end], total, pagination.Pagination{Page: q.Page, Size: q.Size}), nil
}

// Get 返回资源详情;Secret 默认脱敏(data 值替换为 ***)。
func (s *ResourceService) Get(ctx context.Context, cluster, resource, namespace, name string, reveal bool) (*unstructured.Unstructured, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	ref, err := s.resolve(rt, resource, namespace, false)
	if err != nil {
		return nil, err
	}
	obj, err := dynGet(ctx, rt, ref, namespace, name)
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	if !reveal && ref.GVR.Resource == "secrets" {
		redactSecret(obj)
	}
	return obj, nil
}

// SecretData 返回解码后的 Secret 数据;调用方必须完成审计(viewer 不允许)。
func (s *ResourceService) SecretData(ctx context.Context, cluster, namespace, name string) (map[string]string, error) {
	obj, err := s.Get(ctx, cluster, "secrets", namespace, name, true)
	if err != nil {
		return nil, err
	}
	data, found, err := unstructured.NestedMap(obj.Object, "data")
	if err != nil || !found {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(data))
	for k, v := range data {
		s, _ := v.(string)
		raw, decErr := base64.StdEncoding.DecodeString(s)
		if decErr != nil {
			out[k] = s
			continue
		}
		out[k] = string(raw)
	}
	return out, nil
}

// redactSecret 将 Secret data 值替换为 ***,防止默认详情泄露明文。
func redactSecret(obj *unstructured.Unstructured) {
	data, found, _ := unstructured.NestedMap(obj.Object, "data")
	if !found {
		return
	}
	masked := make(map[string]any, len(data))
	for k := range data {
		masked[k] = "***"
	}
	_ = unstructured.SetNestedMap(obj.Object, masked, "data")
}

// GetYAML 返回资源的 YAML 表示。
func (s *ResourceService) GetYAML(ctx context.Context, cluster, resource, namespace, name string) (string, error) {
	obj, err := s.Get(ctx, cluster, resource, namespace, name, false)
	if err != nil {
		return "", err
	}
	out, err := yamlMarshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("marshal yaml of %s/%s: %w", namespace, name, err)
	}
	return string(out), nil
}

// UpdateYAML 用提交的 YAML 更新资源:校验名称/命名空间一致,冲突返回 40406。
func (s *ResourceService) UpdateYAML(ctx context.Context, cluster, resource, namespace, name, yamlText string) (*unstructured.Unstructured, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	obj, err := yamlUnmarshal([]byte(yamlText))
	if err != nil {
		return nil, errcode.New(errcode.ParamInvalid, "invalid yaml").WithCause(err)
	}
	gotName, _, _ := unstructured.NestedString(obj.Object, "metadata", "name")
	if gotName != name {
		return nil, errcode.New(errcode.ParamInvalid, "yaml metadata.name does not match url")
	}
	if ref, rerr := s.resolve(rt, resource, namespace, false); rerr != nil {
		return nil, rerr
	} else if ref.Namespaced {
		gotNS, _, _ := unstructured.NestedString(obj.Object, "metadata", "namespace")
		if gotNS == "" {
			_ = unstructured.SetNestedField(obj.Object, namespace, "metadata", "namespace")
		} else if gotNS != namespace {
			return nil, errcode.New(errcode.ParamInvalid, "yaml metadata.namespace does not match url")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	ref, _ := s.resolve(rt, resource, namespace, false)
	var updated *unstructured.Unstructured
	if ref.Namespaced {
		updated, err = rt.Dyn.Resource(ref.GVR).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	} else {
		updated, err = rt.Dyn.Resource(ref.GVR).Update(ctx, obj, metav1.UpdateOptions{})
	}
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	return updated, nil
}

// Delete 删除资源;属于危险操作,审计由 handler 层挂载。
func (s *ResourceService) Delete(ctx context.Context, cluster, resource, namespace, name string) error {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	ref, err := s.resolve(rt, resource, namespace, false)
	if err != nil {
		return err
	}
	opts := metav1.DeleteOptions{}
	if ref.Namespaced {
		err = rt.Dyn.Resource(ref.GVR).Namespace(namespace).Delete(ctx, name, opts)
	} else {
		err = rt.Dyn.Resource(ref.GVR).Delete(ctx, name, opts)
	}
	if err != nil {
		return mapK8sError(cluster, err)
	}
	logx.Info(ctx, "resource deleted", "cluster", cluster, "namespace", namespace,
		"resource", resource, "resource_type", resource, "name", name)
	return nil
}

// CreateResource 在目标集群创建资源:body 为 K8s 对象的 JSON 或 YAML 文本
// (yamlUnmarshal 兼容两者)。namespace 优先级:body.metadata.namespace >
// query.namespace;两者皆空且资源为命名空间级时返回 40001。GVR 解析复用
// ResolveResource,天然支持 CRD plural 透传;已存在映射 40406,属于危险操作,
// 审计由 handler 层挂载(action=create)。
func (s *ResourceService) CreateResource(ctx context.Context, cluster, resource, namespace, body string) (*unstructured.Unstructured, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	obj, err := yamlUnmarshal([]byte(body))
	if err != nil {
		return nil, errcode.New(errcode.ParamInvalid, "invalid resource manifest").WithCause(err)
	}
	if obj.GetName() == "" {
		return nil, errcode.New(errcode.ParamInvalid, "metadata.name is required")
	}
	if ns := obj.GetNamespace(); ns != "" {
		namespace = ns
	}
	ref, err := s.resolve(rt, resource, namespace, false)
	if err != nil {
		return nil, err
	}
	if !ref.Namespaced {
		namespace = ""
	} else if namespace == "" {
		return nil, errcode.New(errcode.ParamInvalid, "namespace is required for namespaced resource "+resource)
	} else {
		obj.SetNamespace(namespace)
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	var created *unstructured.Unstructured
	if ref.Namespaced {
		created, err = rt.Dyn.Resource(ref.GVR).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	} else {
		created, err = rt.Dyn.Resource(ref.GVR).Create(ctx, obj, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	logx.Info(ctx, "resource created", "cluster", cluster, "namespace", created.GetNamespace(),
		"resource", resource, "resource_type", resource, "name", created.GetName())
	return created, nil
}

// RestartPod 通过删除 Pod 触发重建(由控制器重建,恢复现场)。
func (s *ResourceService) RestartPod(ctx context.Context, cluster, namespace, name string) error {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	err = rt.ClientSet.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return mapK8sError(cluster, err)
	}
	logx.Info(ctx, "pod restarted", "cluster", cluster, "namespace", namespace, "resource_type", "pod", "name", name)
	return nil
}

// RestartDeployment 通过 patch restartedAt 注解触发滚动重启(等价 kubectl rollout restart)。
func (s *ResourceService) RestartDeployment(ctx context.Context, cluster, namespace, name string) (*unstructured.Unstructured, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	// StrategicMergePatchType 要求 JSON 体;此前误用 yaml.Marshal 生成 YAML 文本,
	// 导致 K8s APIServer 拒绝解析、重启必定 500(BUG-01)。
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						restartedAtAnnotation: time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	updated, err := rt.ClientSet.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	logx.Info(ctx, "deployment restart patch applied", "cluster", cluster, "namespace", namespace, "resource_type", "deployment", "name", name)
	return toUnstructured(updated), nil
}

// ScaleDeployment 伸缩 Deployment 副本数。
func (s *ResourceService) ScaleDeployment(ctx context.Context, cluster, namespace, name string, replicas int32) (*unstructured.Unstructured, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	updated, err := rt.ClientSet.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{}, "scale")
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	logx.Info(ctx, "deployment scaled", "cluster", cluster, "namespace", namespace,
		"resource_type", "deployment", "name", name, "replicas", replicas)
	return toUnstructured(updated), nil
}

// PodLogsQuery 是历史日志查询参数。
type PodLogsQuery struct {
	Container    string
	TailLines    int64
	SinceSeconds int64
	Previous     bool
}

// PodLogs 返回 Pod 历史日志(非 follow)。
func (s *ResourceService) PodLogs(ctx context.Context, cluster, namespace, name string, q PodLogsQuery) (*model.PodLogs, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	opts := &corev1.PodLogOptions{Container: q.Container, Previous: q.Previous}
	if q.TailLines > 0 {
		opts.TailLines = &q.TailLines
	}
	if q.SinceSeconds > 0 {
		opts.SinceSeconds = &q.SinceSeconds
	}
	rd, err := rt.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(ctx)
	if err != nil {
		return nil, mapK8sError(cluster, err)
	}
	defer rd.Close() //nolint:errcheck // 只读流,关闭失败无业务影响
	data, err := readAllLimit(rd, maxLogBytes)
	if err != nil {
		return nil, errcode.New(errcode.UpstreamK8sError, "read pod logs failed").WithCause(err)
	}
	return &model.PodLogs{Logs: string(data), Container: q.Container}, nil
}

// FollowPodLogs 打开 follow 日志流;返回读取流与取消函数(调用方负责 cancel)。
// 流式转发供 WS logstream 端点使用。
func (s *ResourceService) FollowPodLogs(ctx context.Context, cluster, namespace, name, container string) (readStreamer, func(), error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	opts := &corev1.PodLogOptions{Container: container, Follow: true}
	rd, err := rt.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(streamCtx)
	if err != nil {
		cancel()
		return nil, nil, mapK8sError(cluster, err)
	}
	return rd, cancel, nil
}

// Events 按字段选择器列出事件(分页)。
func (s *ResourceService) Events(ctx context.Context, cluster, namespace, fieldSelector string, p pagination.Pagination) (pagination.ResultBody[model.EventItem], error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return pagination.ResultBody[model.EventItem]{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	list, err := rt.ClientSet.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: fieldSelector})
	if err != nil {
		return pagination.ResultBody[model.EventItem]{}, mapK8sError(cluster, err)
	}
	items := make([]model.EventItem, 0, len(list.Items))
	for i := range list.Items {
		ev := &list.Items[i]
		items = append(items, model.EventItem{
			Name:          ev.Name,
			Namespace:     ev.Namespace,
			Type:          ev.Type,
			Reason:        ev.Reason,
			Message:       ev.Message,
			Count:         ev.Count,
			InvolvedKind:  ev.InvolvedObject.Kind,
			InvolvedName:  ev.InvolvedObject.Name,
			FirstSeenAt:   ev.FirstTimestamp.Time,
			LastTimestamp: ev.LastTimestamp.Time,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LastTimestamp.After(items[j].LastTimestamp) })
	total := int64(len(items))
	start := (p.Page - 1) * p.Size
	if start > len(items) {
		start = len(items)
	}
	end := start + p.Size
	if end > len(items) {
		end = len(items)
	}
	return pagination.Result(items[start:end], total, p), nil
}
