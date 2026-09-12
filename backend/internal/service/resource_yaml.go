package service

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/v911/backend/internal/model"
)

// yamlMarshal 将任意 K8s 对象编码为 YAML。
func yamlMarshal(v any) ([]byte, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// yamlUnmarshal 将 YAML 解码为 unstructured 对象。
func yamlUnmarshal(data []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &u.Object); err != nil {
		return nil, err
	}
	return u, nil
}

// NodeMetrics 返回节点资源快照;metrics-server 未安装时映射 50100,
// message 注明 metrics-server not available。
func (s *ResourceService) NodeMetrics(ctx context.Context, cluster string) ([]model.NodeMetrics, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	return s.nodeMetricsList(ctx, rt)
}

// PodMetrics 返回 Pod 资源快照;namespace 为空取全部命名空间。
func (s *ResourceService) PodMetrics(ctx context.Context, cluster, namespace string) ([]model.PodMetrics, error) {
	rt, err := s.runtimeOf(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()
	return s.podMetricsList(ctx, rt, namespace)
}
