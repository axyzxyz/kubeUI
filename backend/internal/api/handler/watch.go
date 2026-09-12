package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

// WS 心跳约定(04 §4.5):客户端 25s 发 ping,服务端 30s 无任何帧断开。
const (
	wsReadTimeout  = 30 * time.Second
	wsWriteTimeout = 10 * time.Second
	wsSendBuffer   = 64
)

// upgrader 允许同源代理下的跨域升级;鉴权由 handler 内 JWT 校验保证。
var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // 部署于同域反代之后,鉴权不依赖 Origin
}

// watchConn 是一条 /api/v1/watch 连接:并发安全发送 + 生命周期管理。
// 发送经带缓冲 channel,慢消费者由 hub 侧丢弃(hub.Send 返回 false)。
type watchConn struct {
	id   string
	ws   *websocket.Conn
	mu   sync.Mutex
	send chan wsx.Envelope
	done chan struct{}
	once sync.Once
}

// newWatchConn 升级 HTTP 为 WS 并包装;失败返回错误。
func newWatchConn(id string, w http.ResponseWriter, r *http.Request) (*watchConn, error) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	return &watchConn{
		id:   id,
		ws:   ws,
		send: make(chan wsx.Envelope, wsSendBuffer),
		done: make(chan struct{}),
	}, nil
}

// ID 实现 service.WatchConn。
func (c *watchConn) ID() string { return c.id }

// Send 实现 service.WatchConn,非阻塞投递。
func (c *watchConn) Send(env wsx.Envelope) bool {
	select {
	case c.send <- env:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

// close 关闭连接(幂等)。
func (c *watchConn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.Close() //nolint:errcheck // 关闭错误无处理价值
	})
}

// runWriter 写泵:将 send channel 的帧写回 WS;由 serveWatch 启动,连接关闭时退出。
func (c *watchConn) runWriter(ctx context.Context) {
	for {
		select {
		case <-c.done:
			return
		case env := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck // 超时错误由下一次写暴露
			if err := c.ws.WriteJSON(env); err != nil {
				logx.Warn(ctx, "watch write failed", "resource_type", "watch", "err", err)
				c.close()
				return
			}
		}
	}
}

// wsTokenOf 从 query(?token=)或 Authorization header 取 JWT;浏览器 WS 无法设 header。
func wsTokenOf(c *gin.Context) string {
	if t := c.Query("token"); t != "" {
		return t
	}
	h := c.GetHeader("Authorization")
	if t, ok := cutBearer(h); ok {
		return t
	}
	return ""
}

// cutBearer 与 middleware.Auth 一致的前缀裁剪。
func cutBearer(h string) (string, bool) {
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):], true
	}
	return "", false
}

// wsIdentity 校验 WS 端点的 JWT(支持 ?token= 与 header),失败返回 nil。
func wsIdentity(c *gin.Context, d Deps) (middleware.Identity, bool) {
	token := wsTokenOf(c)
	if token == "" {
		return middleware.Identity{}, false
	}
	ident, err := d.Auth.VerifyAccessToken(c.Request.Context(), token)
	if err != nil {
		return middleware.Identity{}, false
	}
	return middleware.Identity{UserID: ident.UserID, Username: ident.Username, Role: ident.Role}, true
}

// GET /api/v1/watch:事件流端点(wsx 协议,04 §4.5)。
// 认证:?token= 或 Authorization header;心跳 ping/pong;30s 无帧断开。
func watchEndpoint(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := wsIdentity(c, d); !ok {
			c.JSON(http.StatusUnauthorized, errUnauthorizedBody())
			return
		}
		conn, err := newWatchConn("watch-"+c.GetString("requestId"), c.Writer, c.Request)
		if err != nil {
			return
		}
		defer conn.close()
		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		hub := d.Watch

		// 写泵:受控生命周期 worker,由 conn.close 经 done channel 退出。
		go conn.runWriter(ctx)
		defer hub.RemoveConn(conn)

		for {
			_ = conn.ws.SetReadDeadline(time.Now().Add(wsReadTimeout)) //nolint:errcheck // 超时由读错误暴露
			var env wsx.Envelope
			if err := conn.ws.ReadJSON(&env); err != nil {
				return
			}
			switch env.Type {
			case wsx.TypePing:
				conn.Send(wsx.NewEnvelope(wsx.TypePong, env.RequestID, nil))
			case wsx.TypeSubscribe:
				var p wsx.SubscribePayload
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					conn.Send(errorEnvelope(env.RequestID, errcode.ParamInvalid, "invalid subscribe payload"))
					continue
				}
				if err := hub.Subscribe(conn, p); err != nil {
					code := errcode.InternalError
					if ec := errcode.From(err); ec != nil {
						code = ec.Code
					}
					conn.Send(errorEnvelope(env.RequestID, code, err.Error()))
					continue
				}
			case wsx.TypeUnsubscribe:
				var p wsx.SubscribePayload
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					conn.Send(errorEnvelope(env.RequestID, errcode.ParamInvalid, "invalid unsubscribe payload"))
					continue
				}
				hub.Unsubscribe(conn, p.Cluster, p.Resource, p.Namespace, p.Name)
			default:
				conn.Send(errorEnvelope(env.RequestID, errcode.ParamInvalid, "unknown frame type "+env.Type))
			}
		}
	}
}

func errUnauthorizedBody() wsx.ErrorPayload {
	return wsx.ErrorPayload{Code: errcode.Unauthorized, Message: "invalid or expired token"}
}

func errorEnvelope(requestID string, code int, message string) wsx.Envelope {
	return wsx.NewEnvelope(wsx.TypeError, requestID, wsx.ErrorPayload{Code: code, Message: message})
}
