package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/pkg/logx"
	"github.com/v911/backend/internal/pkg/wsx"
)

// RegisterPayload 是 register 帧(信令首帧)的负载。
type RegisterPayload struct {
	Token        string   `json:"token"` // Enrollment Token,一次性
	AgentVersion string   `json:"agentVersion,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// RegisteredPayload 是 registered 帧的负载。
type RegisteredPayload struct {
	Cluster    string `json:"cluster"`
	SessionKey string `json:"sessionKey"` // 数据通道 ?sessionKey= 鉴权
}

// DialPayload 是 dial 帧的负载:要求 Agent 向 addr 建立数据通道。
type DialPayload struct {
	ConnID string `json:"connId"`
	Addr   string `json:"addr"` // 目标 apiserver host:port
}

// pipe 是一次隧道拨号的待接入数据通道:DialContext 侧等待,Agent 数据
// WS 侧注入 wsNetConn;半开(超时未接入)由 DialWait 兜底回收。
type pipe struct {
	cluster    string
	sessionKey string
	ch         chan net.Conn // 缓冲 1,注入即成功
}

// ServeData 返回数据通道 HTTP handler(挂载 GET /api/v1/agent/tunnel)。
// 查询参数 connId + sessionKey 必须与一次进行中的拨号请求匹配;升级成功后
// 该连接被包装为 net.Conn 注入等待中的 TunnelDialer,此后由消费方
// (TLS/HTTP2 over tunnel)读写,Agent 只做字节透传。
func (h *Hub) ServeData() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID := r.URL.Query().Get("connId")
		sessionKey := r.URL.Query().Get("sessionKey")
		if connID == "" || sessionKey == "" {
			http.Error(w, "connId and sessionKey are required", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		p, ok := h.pipes[connID]
		if ok && p.sessionKey != sessionKey {
			ok = false
		}
		if ok {
			delete(h.pipes, connID)
		}
		h.mu.Unlock()
		if !ok {
			http.Error(w, "unknown or expired connId", http.StatusNotFound)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ws.SetReadLimit(h.opts.maxFrameSize())
		conn := newWSNetConn(ws)
		select {
		case p.ch <- conn:
		default:
			_ = conn.Close() //nolint:errcheck // 重复接入直接丢弃
			return
		}
		// conn 生命周期由消费方控制;此处兜底等待关闭,避免半开通道泄漏:
		// 消费方 Close、请求取消(断连)任一发生即退出。
		select {
		case <-r.Context().Done():
		case <-conn.closed:
		}
		_ = conn.Close() //nolint:errcheck // 关闭幂等
	}
}

// takePipe 登记一条待接入 pipe;session 必须在线。
func (h *Hub) takePipe(cluster, sessionKey string) (string, *pipe, error) {
	sess := h.SessionOf(cluster)
	if sess == nil {
		return "", nil, errcode.New(errcode.AgentTunnelUnavailable, "agent tunnel is not connected")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// 会话可能在取锁间隙被替换,复核。
	if cur := h.sessions[cluster]; cur == nil || cur.sessionKey != sessionKey {
		return "", nil, errcode.New(errcode.AgentTunnelUnavailable, "agent tunnel is not connected")
	}
	id := newSecret()
	h.pipes[id] = &pipe{
		cluster:    cluster,
		sessionKey: sessionKey,
		ch:         make(chan net.Conn, 1),
	}
	return id, h.pipes[id], nil
}

// dropPipe 移除待接入 pipe。
func (h *Hub) dropPipe(id string) {
	h.mu.Lock()
	delete(h.pipes, id)
	h.mu.Unlock()
}

// TunnelDialer 实现 k8s.Dialer:经信令通道请求 Agent 拨号目标 apiserver,
// 并等待 Agent 回连数据通道,返回以 WS 二进制帧为载体的 net.Conn。
// TLS 由上层(client-go transport)在此连接上端到端建立,Agent 不接触明文。
type TunnelDialer struct {
	hub     *Hub
	cluster string
}

// DialContext 实现 k8s.Dialer;仅支持 tcp。
func (d *TunnelDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("agent tunnel: unsupported network %q", network)
	}
	h := d.hub
	sess := h.SessionOf(d.cluster)
	if sess == nil {
		return nil, errcode.New(errcode.AgentTunnelUnavailable, "agent tunnel is not connected")
	}
	connID, p, err := h.takePipe(d.cluster, sess.sessionKey)
	if err != nil {
		return nil, err
	}
	defer h.dropPipe(connID)

	// 经信令通道通知 Agent 拨号;写失败说明信令已断。
	if err := sess.send(wsx.NewEnvelope(FrameDial, "", DialPayload{ConnID: connID, Addr: addr})); err != nil {
		return nil, errcode.New(errcode.AgentTunnelUnavailable, "agent signaling write failed").WithCause(err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, h.opts.dialWait())
	defer cancel()
	select {
	case conn := <-p.ch:
		logx.Debug(ctx, "agent tunnel established", "cluster", d.cluster, "addr", addr)
		return conn, nil
	case <-waitCtx.Done():
		// 半开通道:DialWait 内未回连,兜底回收并报 50200。
		return nil, errcode.New(errcode.AgentTunnelUnavailable,
			"agent did not open data channel in time").WithCause(waitCtx.Err())
	}
}

// wsNetConn 将 *websocket.Conn 的二进制帧适配为 net.Conn(全双工字节流)。
// 由平台与 Agent 两侧共用:Write 写 BinaryMessage,Read 聚合收到的帧;
// Text 控制帧被忽略;关闭幂等。
type wsNetConn struct {
	ws *websocket.Conn

	rmu    sync.Mutex
	rbuf   []byte // 当前帧剩余可读字节
	rerr   error  // 读侧终结错误(连接关闭/超时)
	wmu    sync.Mutex
	once   sync.Once
	closed chan struct{}
}

// newWSNetConn 包装 WS 连接。
func newWSNetConn(ws *websocket.Conn) *wsNetConn {
	return &wsNetConn{ws: ws, closed: make(chan struct{})}
}

// Read 实现 net.Conn。
func (c *wsNetConn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.rbuf) == 0 {
		if c.rerr != nil {
			return 0, c.rerr
		}
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			c.rerr = err
			return 0, err
		}
		switch mt {
		case websocket.BinaryMessage:
			if len(data) > 0 {
				c.rbuf = data
			}
		default:
			// Text/Ping/Pong 控制帧:透传语义之外无数据,忽略。
		}
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

// Write 实现 net.Conn:整段写入一帧二进制消息。
func (c *wsNetConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck // 超时错误由 WriteMessage 暴露
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close 实现 net.Conn(幂等)。
func (c *wsNetConn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.closed)
		err = c.ws.Close()
	})
	if err != nil && errors.Is(err, websocket.ErrCloseSent) {
		return nil
	}
	return err
}

// LocalAddr 实现 net.Conn。
func (c *wsNetConn) LocalAddr() net.Addr { return wsAddr{net: "ws", addr: "agent-tunnel-local"} }

// RemoteAddr 实现 net.Conn。
func (c *wsNetConn) RemoteAddr() net.Addr { return wsAddr{net: "ws", addr: "agent-tunnel-remote"} }

// SetDeadline 实现 net.Conn。
func (c *wsNetConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline 实现 net.Conn。
func (c *wsNetConn) SetReadDeadline(t time.Time) error { return c.ws.SetReadDeadline(t) }

// SetWriteDeadline 实现 net.Conn。
func (c *wsNetConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

// wsAddr 是隧道连接的占位地址。
type wsAddr struct {
	net  string
	addr string
}

// Network 实现 net.Addr。
func (a wsAddr) Network() string { return a.net }

// String 实现 net.Addr。
func (a wsAddr) String() string { return a.addr }
