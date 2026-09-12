// Package errcode 定义平台统一的业务错误类型与错误码表。
package errcode

import (
	"errors"
	"net/http"
)

// Error 是带业务错误码的错误类型,handler 层据此映射 HTTP 状态码。
type Error struct {
	// Code 业务错误码,含义见 codes.go 错误码表。
	Code int
	// Message 面向用户的稳定文案(可国际化),不包含内部细节。
	Message string
	// Cause 内部原因,不出现在 HTTP 响应中,仅供日志与 errors.Unwrap 使用。
	Cause error
}

// Error 实现 error 接口,只返回稳定文案。
func (e *Error) Error() string { return e.Message }

// Unwrap 返回内部原因,支持 errors.Is/As 链式判定。
func (e *Error) Unwrap() error { return e.Cause }

// WithCause 返回一个携带内部原因的新 *Error,原错误保持不变。
func (e *Error) WithCause(err error) *Error {
	e2 := *e
	e2.Cause = err
	return &e2
}

// New 创建一个无内部原因的业务错误。
func New(code int, message string) *Error { return &Error{Code: code, Message: message} }

// HTTPStatusOf 将业务错误码映射为 HTTP 状态码,规则见 04-coding-standards §2.2。
func HTTPStatusOf(code int) int {
	switch {
	case code == ResourceConflict:
		// 40406 语义为并发修改冲突,映射 409 而非 404。
		return http.StatusConflict
	case code >= 40000 && code < 40100:
		return http.StatusBadRequest
	case code >= 40100 && code < 40300:
		return http.StatusUnauthorized
	case code >= 40300 && code < 40400:
		return http.StatusForbidden
	case code >= 40400 && code < 40500:
		return http.StatusNotFound
	case code >= 50000:
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

// From 提取错误链中的 *Error;不是业务错误时返回 nil。
func From(err error) *Error {
	var ec *Error
	if errors.As(err, &ec) {
		return ec
	}
	return nil
}
