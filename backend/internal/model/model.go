// Package model 定义对外 API 的领域 DTO,显式 lowerCamel json tag。
package model

import "time"

// Role 内置平台角色名。
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// User 用户 DTO。
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AuditLog 审计日志 DTO。
type AuditLog struct {
	ID           int64     `json:"id"`
	RequestID    string    `json:"requestId"`
	UserID       int64     `json:"userId"`
	Username     string    `json:"username"`
	Action       string    `json:"action"`
	Resource     string    `json:"resource"`
	ResourceType string    `json:"resourceType"`
	Cluster      string    `json:"cluster,omitempty"`
	Namespace    string    `json:"namespace,omitempty"`
	Name         string    `json:"name,omitempty"`
	SourceIP     string    `json:"sourceIp"`
	UserAgent    string    `json:"userAgent"`
	Result       string    `json:"result"` // allow|deny
	CreatedAt    time.Time `json:"createdAt"`
}

// 集群连接状态。
const (
	ClusterStatusReady        = "ready"
	ClusterStatusDegraded     = "degraded"
	ClusterStatusReconnecting = "reconnecting"
	ClusterStatusOffline      = "offline"
)

// 集群接入模式。
const (
	AccessModeDirect = "direct"
	AccessModeAgent  = "agent"
)

// ClusterInfo 集群 DTO(不含任何凭证内容)。
type ClusterInfo struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Version     string    `json:"version,omitempty"`
	AccessMode  string    `json:"accessMode"`
	NodeCount   int       `json:"nodeCount,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ClusterStatus 集群连接状态详情。
type ClusterStatus struct {
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	Version            string    `json:"version,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
	Message            string    `json:"message,omitempty"`
}

// MePermissions 是 GET /users/me/permissions 的响应 DTO。
type MePermissions struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

// ResetPasswordResult 是 admin 重置密码的响应:一次性新密码只出现一次。
type ResetPasswordResult struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
