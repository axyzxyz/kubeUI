// Package wsx 定义 /api/v1/watch WebSocket 端点的 JSON 帧协议类型(04 §4.5)。
package wsx

import "encoding/json"

// 帧类型常量,type 判别联合的取值。
const (
	TypeSubscribe   = "subscribe"
	TypeUnsubscribe = "unsubscribe"
	TypeEvent       = "event"
	TypePing        = "ping"
	TypePong        = "pong"
	TypeError       = "error"
)

// 事件动作常量,对应 K8s watch event。
const (
	ActionAdded    = "added"
	ActionModified = "modified"
	ActionDeleted  = "deleted"
)

// Envelope 是 WebSocket 帧信封,Type 决定 Payload 的结构。
type Envelope struct {
	Type      string          `json:"type"`                // subscribe|unsubscribe|event|ping|pong|error
	RequestID string          `json:"requestId,omitempty"` // subscribe 回执关联
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// SubscribePayload 是 subscribe/unsubscribe 帧的负载。
type SubscribePayload struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace,omitempty"` // 空 = 全部
	Resource  string `json:"resource"`            // pods|deployments|events|clusters|audit|podlogs ...
	// Name/Container 仅 podlogs 订阅使用:指定目标 Pod 与容器(容器空取默认)。
	Name      string `json:"name,omitempty"`
	Container string `json:"container,omitempty"`
}

// EventPayload 是 event 帧的负载。
type EventPayload struct {
	Resource  string `json:"resource"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Action    string `json:"action"` // added|modified|deleted
	Object    any    `json:"object"` // 与 REST 返回的 DTO 相同结构
}

// ErrorPayload 是 error 帧的负载,code 沿用 errcode 错误码表。
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewEnvelope 将任意负载序列化为一帧 Envelope。
func NewEnvelope(typ, requestID string, payload any) Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{Type: TypeError, Payload: mustRaw(ErrorPayload{Code: 50000, Message: "internal error"})}
	}
	return Envelope{Type: typ, RequestID: requestID, Payload: raw}
}

func mustRaw(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
