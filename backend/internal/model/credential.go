package model

import "time"

// EnrollTokenView 是 Enrollment Token 的脱敏 DTO(不含 token 原文)。
type EnrollTokenView struct {
	ID        int64     `json:"id"`
	Cluster   string    `json:"cluster"`
	ExpiresAt time.Time `json:"expiresAt"`
	// NoExpiry 长期 token(永不过期);展示时优先于 ExpiresAt。
	NoExpiry  bool      `json:"noExpiry"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"createdAt"`
}

// EnrollTokenCreated 是创建 Enrollment Token 的响应:token 原文只在创建时返回一次。
type EnrollTokenCreated struct {
	EnrollTokenView
	Token string `json:"token"`
}

// IssuedKubeconfigView 是已签发客户端凭证的 DTO(不含 token 原文)。
type IssuedKubeconfigView struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	Cluster     string    `json:"cluster"`
	Description string    `json:"description,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Revoked     bool      `json:"revoked"`
	CreatedAt   time.Time `json:"createdAt"`
}

// IssuedKubeconfigCreated 是签发响应:token 与一次性下载链接只返回一次。
type IssuedKubeconfigCreated struct {
	IssuedKubeconfigView
	Token       string `json:"token"`
	DownloadURL string `json:"downloadUrl"`
	// LinkExpiresAt 一次性下载链接的失效时间(签发后 24h)。
	LinkExpiresAt time.Time `json:"linkExpiresAt"`
}

// AgentManifest 是 GET /agent/manifest 的响应。
type AgentManifest struct {
	YAML string `json:"yaml"`
}
