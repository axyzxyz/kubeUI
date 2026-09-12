package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

// Agent 重连退避(01-architecture §4.2):1s 起步,指数翻倍,60s 封顶,
// 注册成功后重置。
const (
	ReconnectBackoffStart = time.Second
	ReconnectBackoffMax   = 60 * time.Second
)

// ClientConfig 是 Agent 客户端配置。
type ClientConfig struct {
	// ServerURL 平台基础地址,如 https://kubeUI.example.com。
	ServerURL string
	// Token 是 Enrollment Token,只在注册帧中出现,禁止打印与落日志。
	Token string
	// AgentVersion 上报版本。
	AgentVersion string
	// HeartbeatInterval 心跳周期,默认 25s。
	HeartbeatInterval time.Duration
	// DialTimeout Agent 拨号目标 apiserver 的超时,默认 10s。
	DialTimeout time.Duration
	// InsecureSkipVerify 跳过平台 TLS 校验(自签测试环境)。
	InsecureSkipVerify bool
}

// Client 是 Agent 反连客户端:维持信令通道、心跳与断线重连(受控 worker,
// 退出路径为 ctx 取消),按平台 dial 指令拨号 apiserver 并经数据通道透传。
type Client struct {
	cfg    ClientConfig
	dialer websocket.Dialer
}

// NewClient 构造 Agent 客户端。
func NewClient(cfg ClientConfig) *Client {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	return &Client{
		cfg: cfg,
		dialer: websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
			TLSClientConfig:  &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}, //nolint:gosec // 由配置显式开启
		},
	}
}

// Run 启动主循环:连接 → 注册 → 服务,断开后指数退避重连;ctx 取消时优雅
// 退出。Token 只用于注册帧,不写入任何日志。
func (c *Client) Run(ctx context.Context) error {
	backoff := ReconnectBackoffStart
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		connected, err := c.runOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logx.Warn(ctx, "agent connection lost, will retry", "err", err, "retry_in", backoff.String())
		}
		if connected {
			backoff = ReconnectBackoffStart // 注册成功过的会话,重置退避
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > ReconnectBackoffMax {
			backoff = ReconnectBackoffMax
		}
	}
}

// runOnce 建立一次信令会话直至断开;返回是否完成过注册。
func (c *Client) runOnce(ctx context.Context) (registered bool, err error) {
	ws, _, err := c.dialer.DialContext(ctx, c.signalingURL(), nil) //nolint:bodyclose // ws 由 sess.close() 统一关闭
	if err != nil {
		return false, fmt.Errorf("dial signaling channel: %w", err)
	}
	sess := &agentSession{client: c, ws: ws}
	defer sess.close()
	if err := sess.register(ctx); err != nil {
		return false, err
	}
	return true, sess.serve(ctx)
}

// signalingURL 拼接信令通道 WS 地址。
func (c *Client) signalingURL() string {
	base := strings.TrimSuffix(c.cfg.ServerURL, "/")
	return strings.Replace(base, "http://", "ws://", 1) + "/api/v1/agent/connect"
}

// dataURL 拼接数据通道 WS 地址。
func (c *Client) dataURL(connID, sessionKey string) string {
	base := strings.TrimSuffix(c.cfg.ServerURL, "/")
	base = strings.Replace(base, "http://", "ws://", 1)
	return fmt.Sprintf("%s/api/v1/agent/tunnel?connId=%s&sessionKey=%s",
		base, url.QueryEscape(connID), url.QueryEscape(sessionKey))
}

// agentSession 是一次信令会话的 Agent 侧状态。
type agentSession struct {
	client     *Client
	ws         *websocket.Conn
	sessionKey string
	wmu        sync.Mutex
	once       sync.Once
}

// register 发送注册帧并等待回执。
func (s *agentSession) register(ctx context.Context) error {
	if err := s.send(wsx.NewEnvelope(FrameRegister, "", RegisterPayload{
		Token:        s.client.cfg.Token,
		AgentVersion: s.client.cfg.AgentVersion,
		Capabilities: []string{"tunnel"},
	})); err != nil {
		return fmt.Errorf("send register frame: %w", err)
	}
	env, err := s.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("await registered frame: %w", err)
	}
	if env.Type == FrameError {
		var p wsx.ErrorPayload
		_ = json.Unmarshal(env.Payload, &p) //nolint:errcheck // 失败时字段为空
		return fmt.Errorf("register rejected: code=%d message=%s", p.Code, p.Message)
	}
	if env.Type != FrameRegistered {
		return fmt.Errorf("unexpected frame %q while registering", env.Type)
	}
	var p RegisteredPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil || p.SessionKey == "" {
		return fmt.Errorf("invalid registered payload: %w", err)
	}
	s.sessionKey = p.SessionKey
	logx.Info(ctx, "agent registered", "cluster", p.Cluster)
	return nil
}

// serve 是注册后的主循环:心跳定时器 + 信令读;收到 dial 帧时拨号 apiserver
// 并透传。所有 goroutine 在本函数返回前 join。
func (s *agentSession) serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// 心跳 worker:受控生命周期,serve 退出即停;写失败经 ctx 传导退出。
	go s.heartbeat(ctx)
	// ctx 取消联动关闭连接,解除读阻塞(受控 worker,serve 返回即停)。
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			s.close()
		case <-stopWatch:
		}
	}()

	for {
		env, err := s.readFrame(ctx)
		if err != nil {
			return fmt.Errorf("signaling read: %w", err)
		}
		switch env.Type {
		case FrameDial:
			var p DialPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil || p.ConnID == "" || p.Addr == "" {
				logx.Warn(ctx, "invalid dial frame", "err", err)
				continue
			}
			// 必须并发处理:handleDial 内的对拷协程持续到隧道关闭,
			// 同步执行会阻塞信令读循环,后续 dial 帧永远无法到达。
			go s.handleDial(ctx, p)
		case FrameError:
			var p wsx.ErrorPayload
			_ = json.Unmarshal(env.Payload, &p) //nolint:errcheck
			logx.Warn(ctx, "server error frame", "code", p.Code, "message", p.Message)
		default:
			// pong / 其他:忽略。
		}
	}
}

// heartbeat 周期发送 ping 帧;周期取 HeartbeatInterval(默认 25s)。
func (s *agentSession) heartbeat(ctx context.Context) {
	t := time.NewTicker(s.client.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.send(wsx.NewEnvelope(wsx.TypePing, "", nil)); err != nil {
				logx.Warn(ctx, "heartbeat send failed", "err", err)
				return
			}
		}
	}
}

// handleDial 响应一次拨号:本地拨 apiserver → 回连数据通道 → 双向对拷。
// 两条拷贝协程均由 dialOnce join(受控 worker,无泄漏)。
func (s *agentSession) handleDial(ctx context.Context, p DialPayload) {
	dialCtx, cancel := context.WithTimeout(ctx, s.client.cfg.DialTimeout)
	defer cancel()
	var d net.Dialer
	target, err := d.DialContext(dialCtx, "tcp", p.Addr)
	if err != nil {
		logx.Warn(ctx, "dial apiserver failed", "addr", p.Addr, "err", err)
		return
	}
	defer target.Close() //nolint:errcheck // 关闭错误无处理价值

	ws, _, err := s.client.dialer.DialContext(ctx, s.client.dataURL(p.ConnID, s.sessionKey), nil) //nolint:bodyclose // ws 由 handleDial 对拷结束后统一关闭
	if err != nil {
		logx.Warn(ctx, "open data channel failed", "err", err)
		return
	}
	tunnel := newWSNetConn(ws)
	logx.Debug(ctx, "data channel opened", "addr", p.Addr)

	// 双向对拷:任一方向结束即整体收敛;WaitGroup 保证 join。
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, tunnel) //nolint:errcheck // 对拷错误即断链
		_ = tunnel.Close()             //nolint:errcheck // 促使另一方向退出
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(tunnel, target) //nolint:errcheck
		_ = target.Close()             //nolint:errcheck
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		_ = tunnel.Close() //nolint:errcheck
		_ = target.Close() //nolint:errcheck
		<-done
	}
}

// send 线程安全发送一帧。
func (s *agentSession) send(env wsx.Envelope) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck
	return s.ws.WriteJSON(env)
}

// readFrame 读取一帧信令(依赖连接级读超时设置,见 readFrame 调用前刷新)。
func (s *agentSession) readFrame(ctx context.Context) (wsx.Envelope, error) {
	// 心跳 25s、服务端 30s 超时;读侧以 2 倍心跳为界兜底。
	_ = s.ws.SetReadDeadline(time.Now().Add(s.client.cfg.HeartbeatInterval * 2)) //nolint:errcheck
	var env wsx.Envelope
	if err := s.ws.ReadJSON(&env); err != nil {
		return wsx.Envelope{}, err
	}
	return env, nil
}

// close 关闭信令连接(幂等)。
func (s *agentSession) close() {
	s.once.Do(func() { _ = s.ws.Close() }) //nolint:errcheck // 关闭错误无处理价值
}
