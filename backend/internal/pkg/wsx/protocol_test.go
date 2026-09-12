package wsx

import (
	"encoding/json"
	"testing"
)

func TestNewEnvelopeSubscribe(t *testing.T) {
	env := NewEnvelope(TypeSubscribe, "req-1", SubscribePayload{Cluster: "prod", Resource: "pods"})
	if env.Type != TypeSubscribe || env.RequestID != "req-1" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var p SubscribePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Cluster != "prod" || p.Resource != "pods" {
		t.Fatalf("unexpected payload: %+v", p)
	}
}

func TestEnvelopeRoundTripJSON(t *testing.T) {
	env := NewEnvelope(TypeError, "", ErrorPayload{Code: 40401, Message: "cluster not found"})
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Envelope
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != TypeError {
		t.Fatalf("type = %q, want error", back.Type)
	}
	var ep ErrorPayload
	if err := json.Unmarshal(back.Payload, &ep); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	if ep.Code != 40401 || ep.Message != "cluster not found" {
		t.Fatalf("unexpected error payload: %+v", ep)
	}
}

func TestFrameTypeConstants(t *testing.T) {
	// 协议常量与 04 §4.5 对齐,防止误改。
	for _, tc := range []struct{ got, want string }{
		{TypeSubscribe, "subscribe"}, {TypeUnsubscribe, "unsubscribe"},
		{TypeEvent, "event"}, {TypePing, "ping"}, {TypePong, "pong"}, {TypeError, "error"},
		{ActionAdded, "added"}, {ActionModified, "modified"}, {ActionDeleted, "deleted"},
	} {
		if tc.got != tc.want {
			t.Fatalf("got %q, want %q", tc.got, tc.want)
		}
	}
}
