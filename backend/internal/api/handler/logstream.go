package handler

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
)

// GET /api/v1/clusters/:cluster/pods/:name/logs/stream?namespace=&container=
// WS 端点:follow 实时日志,二进制帧 = 日志字节;结束发 {"type":"logstream:close"}。
// 断线重连由前端处理(websocket.md §3)。
func podLogStream(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := wsIdentity(c, d)
		if !ok {
			c.JSON(401, errUnauthorizedBody())
			return
		}
		if !middleware.HasPermScope(ident, "logs:read", c.Param("cluster"), nsOf(c)) {
			c.JSON(403, gin.H{"code": errcode.Forbidden, "message": "insufficient privilege: logs:read", "data": nil})
			return
		}
		conn, err := newWatchConn("logstream-"+c.GetString("requestId"), c.Writer, c.Request)
		if err != nil {
			return
		}
		defer conn.close()

		ctx := c.Request.Context()
		rd, cancel, err := d.Resources.FollowPodLogs(ctx, c.Param("cluster"), nsOf(c), c.Param("name"), c.Query("container"))
		if err != nil {
			sendWSError(conn, err)
			return
		}
		defer cancel()

		// 读泵:仅消费客户端关闭/心跳,无业务帧;连接断开即取消上游流。
		stopRead := make(chan struct{})
		go func() {
			defer close(stopRead)
			for {
				_ = conn.ws.SetReadDeadline(time.Now().Add(wsReadTimeout)) //nolint:errcheck // 超时由读错误暴露
				if _, _, err := conn.ws.ReadMessage(); err != nil {
					return
				}
			}
		}()

		buf := make([]byte, 8192)
		for {
			n, rerr := rd.Read(buf)
			if n > 0 {
				_ = conn.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck // 写错误在下一轮暴露
				if werr := conn.ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					break
				}
			}
			if rerr != nil {
				break
			}
			select {
			case <-stopRead:
				logx.Info(ctx, "logstream client disconnected",
					"cluster", c.Param("cluster"), "namespace", nsOf(c),
					"resource_type", "podlogs", "name", c.Param("name"))
				return
			default:
			}
		}
		// 流结束:发 close 帧后关闭。
		conn.Send(wsx.NewEnvelope("logstream:close", "", nil))
	}
}

// sendWSError 发送 error 帧后关闭。
func sendWSError(conn *watchConn, err error) {
	code := errcode.InternalError
	if ec := errcode.From(err); ec != nil {
		code = ec.Code
	}
	conn.Send(wsx.NewEnvelope(wsx.TypeError, "", wsx.ErrorPayload{Code: code, Message: err.Error()}))
}
