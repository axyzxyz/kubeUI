// Package service 实现平台业务逻辑:认证、用户、审计、集群注册与注册表。
package service

import "errors"

// 服务层哨兵错误;handler 层应转成 errcode.Error 之外的语义判断,
// 本层直接返回 errcode.Error 以便统一映射。
var (
	// ErrInvalidCredentials 用户名或密码错误。
	ErrInvalidCredentials = errors.New("invalid username or password")
)
