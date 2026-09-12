package model

import "time"

// ResourceItem 是资源浏览列表的通用摘要 DTO,覆盖全部内置资源与 CRD;
// Status 字段按资源类型填充(Pod phase、workload ready 数、Node Ready 等)。
type ResourceItem struct {
	Kind      string            `json:"kind,omitempty"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Status    string            `json:"status,omitempty"`
	CreatedAt *time.Time        `json:"createdAt,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// EventItem 是 K8s Event 列表条目。
type EventItem struct {
	Name          string    `json:"name"`
	Namespace     string    `json:"namespace,omitempty"`
	Type          string    `json:"type,omitempty"` // Normal|Warning
	Reason        string    `json:"reason,omitempty"`
	Message       string    `json:"message,omitempty"`
	Source        string    `json:"source,omitempty"`
	Count         int32     `json:"count,omitempty"`
	InvolvedKind  string    `json:"involvedKind,omitempty"`
	InvolvedName  string    `json:"involvedName,omitempty"`
	FirstSeenAt   time.Time `json:"firstSeenAt,omitempty"`
	LastTimestamp time.Time `json:"lastTimestamp,omitempty"`
}

// PodLogs 是历史日志响应。
type PodLogs struct {
	Logs      string `json:"logs"`
	Container string `json:"container,omitempty"`
}

// NodeMetrics 是 metrics-server 的节点资源快照。
type NodeMetrics struct {
	Name   string `json:"name"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	CPUPct string `json:"cpuPct,omitempty"`
	MemPct string `json:"memPct,omitempty"`
}

// PodMetrics 是 metrics-server 的 Pod 资源快照。
type PodMetrics struct {
	Namespace  string          `json:"namespace"`
	Name       string          `json:"name"`
	Containers []ContainerStat `json:"containers"`
}

// ContainerStat 是单个容器的资源用量。
type ContainerStat struct {
	Name   string `json:"name"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// ScaleRequest 是副本伸缩请求体。
type ScaleRequest struct {
	Replicas int32 `json:"replicas" binding:"gte=0"`
}
