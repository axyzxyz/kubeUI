// Package agent 实现 Agent 反连模式(01-architecture §4):
//
//   - TunnelHub 维护集群 ↔ Agent 的信令 WebSocket 会话,Agent 用一次性
//     Enrollment Token 首帧注册;成功后为该集群注入 TunnelDialer 并重注册
//     运行时,此后 client-go 全链路(含 exec SPDY)经 rest.Config.Dial 走隧道;
//   - 数据通道为第二条 WS 连接,两端二进制帧对拷即等价于原始 TCP 流,
//     TLS 端到端保持在平台与目标 APIServer 之间,Agent 只透传字节。
//
// 并发约定:Hub 并发安全;Session 的写侧经互斥锁串行;所有 goroutine 由
// 连接关闭路径负责退出,不启动裸协程。
package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/axyzxyz/kubeui/backend/internal/k8s"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

// 心跳与超时约定(01-architecture §4.2):Agent 25s 心跳,30s 无帧断开;
// 数据通道建立等待 30s(半开通道兜底回收)。
const (
	DefaultHeartbeatInterval = 25 * time.Second
	DefaultHeartbeatTimeout  = 30 * time.Second
	DefaultDialWait          = 30 * time.Second
	DefaultRegisterTimeout   = 10 * time.Second
	DefaultMaxFrameSize      = 512 << 10 // 二进制帧上限 512KB
)

// 信令帧类型(wsx.Envelope.Type 取值)。
const (
	FrameRegister   = "register"   // Agent → 平台:首帧注册
	FrameRegistered = "registered" // 平台 → Agent:注册成功
	FrameDial       = "dial"       // 平台 → Agent:请求拨号 apiserver
	FrameError      = "error"      // 平台 → Agent:错误
)

// wsWriteTimeout 是信令写超时。
const wsWriteTimeout = 10 * time.Second

// Options 是 TunnelHub 的构造参数;nil/零值字段取默认实现。
type Options struct {
	// Manager 为集群运行时管理器,用于注入 TunnelDialer 并重注册运行时。
	Manager *k8s.Manager
	// ConfirmToken 在注册回执成功后消费一次性 Enrollment Token(可空);
	// 与 ValidateToken 分离:握手失败不烧 token,重试无需重新签发。
	ConfirmToken func(ctx context.Context, token string) error
	// ValidateToken 校验 Enrollment Token 并返回绑定的集群名
	// (注册成功后失效)由实现方保证。
	ValidateToken func(ctx context.Context, token string) (cluster string, err error)
	// OnConnect 会话建立成功后回调(标记 accessMode=agent、置 Ready);可空。
	OnConnect func(ctx context.Context, cluster, agentVersion string)
	// OnDisconnect 会话断开后回调(置 Degraded,提示 Agent 反连中);可空。
	OnDisconnect func(ctx context.Context, cluster string)
	// HeartbeatTimeout 信令通道读超时(无任何帧即断开),默认 30s。
	HeartbeatTimeout time.Duration
	// DialWait 数据通道建立等待上限(半开兜底回收),默认 30s。
	DialWait time.Duration
	// RegisterTimeout 首帧注册等待上限,默认 10s。
	RegisterTimeout time.Duration
	// MaxFrameSize 单帧大小上限,默认 512KB。
	MaxFrameSize int64
}

func (o *Options) heartbeatTimeout() time.Duration {
	if o.HeartbeatTimeout > 0 {
		return o.HeartbeatTimeout
	}
	return DefaultHeartbeatTimeout
}

func (o *Options) dialWait() time.Duration {
	if o.DialWait > 0 {
		return o.DialWait
	}
	return DefaultDialWait
}

func (o *Options) registerTimeout() time.Duration {
	if o.RegisterTimeout > 0 {
		return o.RegisterTimeout
	}
	return DefaultRegisterTimeout
}

func (o *Options) maxFrameSize() int64 {
	if o.MaxFrameSize > 0 {
		return o.MaxFrameSize
	}
	return DefaultMaxFrameSize
}

// Session 是一条 Agent 信令会话:写侧互斥,关闭幂等。
type Session struct {
	hub          *Hub
	cluster      string
	agentVersion string
	sessionKey   string // 数据通道鉴权密钥,只经 registered 帧下发给 Agent
	capabilities []string

	wmu  sync.Mutex
	ws   *websocket.Conn
	done chan struct{}
	once sync.Once
}

// Cluster 返回会话绑定的集群名。
func (s *Session) Cluster() string { return s.cluster }

// AgentVersion 返回 Agent 上报的版本号。
func (s *Session) AgentVersion() string { return s.agentVersion }

// Capabilities 返回 Agent 上报的能力列表。
func (s *Session) Capabilities() []string { return s.capabilities }

// Done 返回会话结束信号 channel。
func (s *Session) Done() <-chan struct{} { return s.done }

// Hub 是全部 Agent 反连会话的中心:维护 集群名 → 会话 与 connId → 待接入
// 数据通道 的注册表,并实现信令/数据两个 WS 端点。并发安全。
type Hub struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*Session // cluster → 当前会话(每集群仅一个 Agent)
	pipes    map[string]*pipe    // connId → 待接入数据通道
}

// NewHub 构造 TunnelHub。
func NewHub(opts Options) *Hub {
	return &Hub{
		opts:     opts,
		sessions: make(map[string]*Session),
		pipes:    make(map[string]*pipe),
	}
}

// SessionCount 返回当前在线 Agent 会话数(测试与观测用)。
func (h *Hub) SessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// SessionOf 返回指定集群当前会话;不在线返回 nil。
func (h *Hub) SessionOf(cluster string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[cluster]
}

// Close 断开全部 Agent 会话;由 server 关闭路径调用。
func (h *Hub) Close(ctx context.Context) {
	h.mu.Lock()
	ss := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		ss = append(ss, s)
	}
	h.sessions = make(map[string]*Session)
	h.mu.Unlock()
	for _, s := range ss {
		s.close()
	}
	logx.Info(ctx, "agent hub closed", "sessions", len(ss))
}

// ServeSignaling 返回信令通道 HTTP handler(挂载 GET /api/v1/agent/connect)。
// 协议:Agent 首帧 {type:"register", payload:{token, agentVersion, capabilities}},
// 成功回执 {type:"registered", payload:{cluster, sessionKey}},此后 25s 心跳、
// 30s 无帧断开;平台侧 {type:"dial", payload:{connId, addr}} 请求拨号。
func (h *Hub) ServeSignaling() http.HandlerFunc {
	return h.handleSignaling
}

// handleSignaling 是 GET /api/v1/agent/connect 的 gin 适配回调,见 ServeSignaling。
func (h *Hub) handleSignaling(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrade 失败已由 upgrader 写回 HTTP 错误
	}
	ws.SetReadLimit(h.opts.maxFrameSize())

	// 首帧注册:RegisterTimeout 内必须完成,否则断开。
	_ = ws.SetReadDeadline(time.Now().Add(h.opts.registerTimeout()))
	mt, raw, err := ws.ReadMessage()
	if err != nil {
		_ = ws.Close() //nolint:errcheck // 关闭错误无处理价值
		return
	}
	if mt != websocket.TextMessage {
		h.reject(ws, errcode.New(errcode.ParamInvalid, "first frame must be a register text frame"))
		return
	}
	var env wsx.Envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Type != FrameRegister {
		h.reject(ws, errcode.New(errcode.ParamInvalid, "first frame must be a register frame"))
		return
	}
	sess, err := h.register(ctx, ws, env.Payload)
	if err != nil {
		logx.Warn(ctx, "agent register rejected", "err", err)
		h.reject(ws, err)
		return
	}
	logx.Info(ctx, "agent registered", "cluster", sess.cluster, "agent_version", sess.agentVersion)
	sess.run(ctx)
}

// register 处理首帧注册:校验 token → 抢占集群会话 → 注入拨号器并重注册运行时。
func (h *Hub) register(ctx context.Context, ws *websocket.Conn, payloadRaw []byte) (*Session, error) {
	if h.opts.ValidateToken == nil {
		return nil, errcode.New(errcode.InternalError, "agent enrollment is not configured")
	}
	var p RegisterPayload
	if err := json.Unmarshal(payloadRaw, &p); err != nil || p.Token == "" {
		return nil, errcode.New(errcode.ParamInvalid, "invalid register payload")
	}
	cluster, err := h.opts.ValidateToken(ctx, p.Token)
	if err != nil {
		return nil, err
	}
	sess := &Session{
		hub:          h,
		cluster:      cluster,
		agentVersion: p.AgentVersion,
		sessionKey:   newSecret(),
		ws:           ws,
		done:         make(chan struct{}),
		capabilities: p.Capabilities,
	}

	h.mu.Lock()
	if old, ok := h.sessions[cluster]; ok && old != sess {
		// 同集群旧 Agent 在线:断开旧会话(新会话接管)。
		h.mu.Unlock()
		old.close()
		h.mu.Lock()
	}
	h.sessions[cluster] = sess
	h.mu.Unlock()

	// 先回执 registered(携带 sessionKey),再消费 token / 挂拨号器:
	// attachDialer 触发的健康探测会立即经隧道下发 dial 帧,若先于回执,
	// Agent 尚处注册等待态会视为协议错误断开(并连带烧掉一次性 token)。
	if err := sess.send(wsx.NewEnvelope(FrameRegistered, "", RegisteredPayload{
		Cluster:    cluster,
		SessionKey: sess.sessionKey,
	})); err != nil {
		sess.close()
		return nil, fmt.Errorf("send registered frame: %w", err)
	}
	if h.opts.ConfirmToken != nil {
		if err := h.opts.ConfirmToken(ctx, p.Token); err != nil {
			sess.close()
			return nil, fmt.Errorf("confirm enrollment token: %w", err)
		}
	}

	// 注册完成即标记接入,后续失败走 OnDisconnect 恢复路径。
	if h.opts.OnConnect != nil {
		h.opts.OnConnect(ctx, cluster, sess.agentVersion)
	}
	h.attachDialer(ctx, cluster)
	return sess, nil
}

// attachDialer 为集群注入 TunnelDialer;运行时已在注册表中时重注册以使
// rest.Config.Dial 生效(client-go 全链路零改动,01 §4.3)。
func (h *Hub) attachDialer(ctx context.Context, cluster string) {
	if h.opts.Manager == nil {
		return
	}
	h.opts.Manager.SetDialer(cluster, &TunnelDialer{hub: h, cluster: cluster})
	if rt, err := h.opts.Manager.Get(cluster); err == nil {
		if err := h.opts.Manager.Register(ctx, rt.Name, rt.Version, rt.RestConfig); err != nil {
			logx.Warn(ctx, "re-register cluster runtime over tunnel failed", "cluster", cluster, "err", err)
		}
	}
}

// run 是注册完成后的信令读循环:刷新读超时(心跳)、回复 ping;退出时清理
// 会话并触发 OnDisconnect。由 handleSignaling 调用,连接关闭即退出。
func (s *Session) run(ctx context.Context) {
	defer s.onExit(ctx)
	for {
		_ = s.ws.SetReadDeadline(time.Now().Add(s.hub.opts.heartbeatTimeout())) //nolint:errcheck // 超时由读错误暴露
		mt, raw, err := s.ws.ReadMessage()
		if err != nil {
			logx.Info(ctx, "agent signaling closed", "cluster", s.cluster, "err", err)
			return
		}
		if mt != websocket.TextMessage {
			continue // 信令通道不接受二进制帧
		}
		var env wsx.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		switch env.Type {
		case wsx.TypePing:
			s.send(wsx.NewEnvelope(wsx.TypePong, env.RequestID, nil)) //nolint:errcheck // 写失败由下轮读暴露
		case wsx.TypePong, FrameRegistered, FrameError:
			// 忽略:pong 为心跳回执,其余帧在信令通道无意义。
		default:
			// 写失败由下轮读超时暴露,错误值无需处理
			_ = s.send(wsx.NewEnvelope(FrameError, env.RequestID,
				wsx.ErrorPayload{Code: errcode.ParamInvalid, Message: "unknown frame type " + env.Type}))
		}
	}
}

// onExit 在会话退出时清理注册表并通知下游。
func (s *Session) onExit(ctx context.Context) {
	s.close()
	h := s.hub
	h.mu.Lock()
	if cur, ok := h.sessions[s.cluster]; ok && cur == s {
		delete(h.sessions, s.cluster)
	}
	h.mu.Unlock()
	if h.opts.OnDisconnect != nil {
		h.opts.OnDisconnect(ctx, s.cluster)
	}
	logx.Warn(ctx, "agent disconnected", "cluster", s.cluster)
}

// send 线程安全地发送一帧信令。
func (s *Session) send(env wsx.Envelope) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck // 超时错误由 WriteJSON 暴露
	return s.ws.WriteJSON(env)
}

// close 关闭底层连接(幂等)。
func (s *Session) close() {
	s.once.Do(func() {
		close(s.done)
		_ = s.ws.Close() //nolint:errcheck // 关闭错误无处理价值
	})
}

// reject 以 error 帧回应并关闭连接。
func (h *Hub) reject(ws *websocket.Conn, err error) {
	code, msg := errcode.InternalError, err.Error()
	if ec := errcode.From(err); ec != nil {
		code, msg = ec.Code, ec.Message
	}
	env := wsx.NewEnvelope(FrameError, "", wsx.ErrorPayload{Code: code, Message: msg})
	_ = ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck
	_ = ws.WriteJSON(env)                                   //nolint:errcheck // 拒绝路径写失败无处理价值
	_ = ws.Close()                                          //nolint:errcheck
}

// newSecret 生成 256bit 随机 base64url 字符串(会话密钥/connId 熵源)。
func newSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属系统级故障,直接 panic 拒绝服务。
		panic(fmt.Errorf("agent: generate secret: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// upgrader 允许反代后的 Agent 接入;鉴权依赖 Enrollment Token / sessionKey。
var upgrader = websocket.Upgrader{
	CheckOrigin:     func(*http.Request) bool { return true },
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
}
