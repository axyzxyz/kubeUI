package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/v911/backend/internal/k8s"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/pkg/wsx"
	"k8s.io/client-go/rest"
)

func init() { logx.Init("error", "text") }

// hubFixture 是一套进程内 Hub + httptest 服务 + 一次性 token 表。
type hubFixture struct {
	hub      *Hub
	srv      *httptest.Server
	tokens   map[string]string // token → cluster(消费即删)
	conns    chan string       // OnConnect 通知
	disconns chan string       // OnDisconnect 通知
}

func newHubFixture(t *testing.T, manager *k8s.Manager) *hubFixture {
	t.Helper()
	f := &hubFixture{
		tokens:   map[string]string{"tok-prod": "prod"},
		conns:    make(chan string, 8),
		disconns: make(chan string, 8),
	}
	h := NewHub(Options{
		Manager: manager,
		ValidateToken: func(ctx context.Context, token string) (string, error) {
			cluster, ok := f.tokens[token]
			if !ok {
				return "", errcode.New(errcode.EnrollTokenInvalid, "invalid enrollment token")
			}
			delete(f.tokens, token) // 一次性
			return cluster, nil
		},
		OnConnect:    func(ctx context.Context, cluster, _ string) { f.conns <- cluster },
		OnDisconnect: func(ctx context.Context, cluster string) { f.disconns <- cluster },
	})
	f.hub = h
	mux := http.NewServeMux()
	mux.Handle("/api/v1/agent/connect", h.ServeSignaling())
	mux.Handle("/api/v1/agent/tunnel", h.ServeData())
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// startAgent 启动一个受测 Client(模拟 Agent 端),返回取消函数。
func startAgent(t *testing.T, serverURL, token string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client := NewClient(ClientConfig{
		ServerURL:          serverURL,
		Token:              token,
		AgentVersion:       "test",
		HeartbeatInterval:  DefaultHeartbeatInterval,
		InsecureSkipVerify: true,
	})
	done := make(chan struct{})
	go func() { defer close(done); _ = client.Run(ctx) }() //nolint:errcheck // 测试 worker
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Log("agent client did not exit in time")
		}
	})
	return cancel
}

// waitCluster 等待指定集群的会话建立。
func waitCluster(t *testing.T, h *Hub, cluster string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.SessionOf(cluster) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session for cluster %s not established", cluster)
}

// tcpEchoServer 启动字节回显 TCP 服务,返回地址;Close 时停止。
func tcpEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()      //nolint:errcheck
				_, _ = io.Copy(c, c) //nolint:errcheck // 回显
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close() //nolint:errcheck
		<-done
	})
	return ln.Addr().String()
}

// waitResult 以超时方式从 channel 读取一个值。
func waitResult[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		var zero T
		return zero
	}
}

// TestSignalingRegisterAndDisconnect 覆盖:注册成功 → 会话可查;错误 token
// 拒绝;断开后 OnDisconnect 触发且会话移除。
func TestSignalingRegisterAndDisconnect(t *testing.T) {
	f := newHubFixture(t, nil)
	cancel := startAgent(t, f.srv.URL, "tok-prod")
	waitCluster(t, f.hub, "prod")
	if got := waitResult(t, f.conns, "on-connect"); got != "prod" {
		t.Fatalf("on-connect cluster=%q", got)
	}
	sess := f.hub.SessionOf("prod")
	if sess.AgentVersion() != "test" {
		t.Fatalf("agent version %q", sess.AgentVersion())
	}
	// 断开:会话移除 + OnDisconnect。
	cancel()
	if got := waitResult(t, f.disconns, "on-disconnect"); got != "prod" {
		t.Fatalf("on-disconnect cluster=%q", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && f.hub.SessionOf("prod") != nil {
		time.Sleep(10 * time.Millisecond)
	}
	if f.hub.SessionOf("prod") != nil {
		t.Fatal("session not removed after disconnect")
	}
}

// TestRegisterRejectsInvalidToken 覆盖非法 token 注册被拒。
func TestRegisterRejectsInvalidToken(t *testing.T) {
	f := newHubFixture(t, nil)
	cancel := startAgent(t, f.srv.URL, "bogus")
	defer cancel()
	// Client 会持续重连但始终被拒,会话不应建立。
	time.Sleep(300 * time.Millisecond)
	if f.hub.SessionCount() != 0 {
		t.Fatalf("unexpected sessions: %d", f.hub.SessionCount())
	}
}

// TestSetDialerInjection 覆盖:注册成功后 Manager.SetDialer 注入并重注册运行时。
func TestSetDialerInjection(t *testing.T) {
	mgr := k8s.NewManager(k8s.Options{HealthDisabled: true})
	if err := mgr.Register(context.Background(), "prod", "1.29", &rest.Config{Host: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("register cluster: %v", err)
	}
	f := newHubFixture(t, mgr)
	cancel := startAgent(t, f.srv.URL, "tok-prod")
	defer cancel()
	waitCluster(t, f.hub, "prod")

	if mgr.Dialer("prod") == nil {
		t.Fatal("tunnel dialer not injected into manager")
	}
	// 重注册后运行时仍在(状态 ready)。
	if snap, ok := mgr.Snapshot("prod"); !ok || snap.Status != "ready" {
		t.Fatalf("runtime snapshot after re-register: %+v ok=%v", snap, ok)
	}
}

// TestTunnelDataCopy 覆盖 TunnelDialer 全链路数据对拷:
// DialContext → dial 帧 → Agent 拨号 echo 服务并回连数据通道 → 字节往返。
func TestTunnelDataCopy(t *testing.T) {
	f := newHubFixture(t, nil)
	cancel := startAgent(t, f.srv.URL, "tok-prod")
	defer cancel()
	waitCluster(t, f.hub, "prod")
	echoAddr := tcpEchoServer(t)

	dialer := &TunnelDialer{hub: f.hub, cluster: "prod"}
	ctx, cancelDial := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDial()
	conn, err := dialer.DialContext(ctx, "tcp", echoAddr)
	if err != nil {
		t.Fatalf("dial over tunnel: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	payload := []byte("hello-through-tunnel")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("echo mismatch: %q", buf)
	}
}

// TestTunnelDialNoSession 覆盖无会话时 TunnelDialer 返回 50200。
func TestTunnelDialNoSession(t *testing.T) {
	f := newHubFixture(t, nil)
	dialer := &TunnelDialer{hub: f.hub, cluster: "ghost"}
	_, err := dialer.DialContext(context.Background(), "tcp", "127.0.0.1:1")
	if ec := errcode.From(err); ec == nil || ec.Code != errcode.AgentTunnelUnavailable {
		t.Fatalf("want AgentTunnelUnavailable, got %v", err)
	}
	// 非法 network。
	_, err = dialer.DialContext(context.Background(), "udp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("want error for udp network")
	}
}

// TestHalfOpenPipeRecovery 覆盖半开通道:无 Agent 回连时 DialWait 兜底返回 50200。
// 会话来自一次真实 WS 注册(注册后测试侧不响应 dial 帧)。
func TestHalfOpenPipeRecovery(t *testing.T) {
	f := newHubFixture(t, nil)
	f.tokens["tok-hang"] = "prod"
	h := f.hub

	ws, _, err := websocket.DefaultDialer.Dial(
		"ws://"+hostOf(f.srv.URL)+"/api/v1/agent/connect", nil)
	if err != nil {
		t.Fatalf("dial signaling: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() }) //nolint:errcheck
	if err := ws.WriteJSON(wsx.Envelope{Type: FrameRegister,
		Payload: mustRaw(t, RegisterPayload{Token: "tok-hang"})}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	var ack wsx.Envelope
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != FrameRegistered {
		t.Fatalf("registered frame: type=%q err=%v", ack.Type, err)
	}
	waitCluster(t, h, "prod")

	dialer := &TunnelDialer{hub: h, cluster: "prod"}
	h.opts.DialWait = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = dialer.DialContext(ctx, "tcp", "127.0.0.1:1")
	if ec := errcode.From(err); ec == nil || ec.Code != errcode.AgentTunnelUnavailable {
		t.Fatalf("want AgentTunnelUnavailable on half-open, got %v", err)
	}
}

// hostOf 去掉 URL scheme。
func hostOf(u string) string {
	return strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://")
}

// mustRaw 序列化测试负载。
func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}
