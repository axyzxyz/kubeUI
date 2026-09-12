package handler

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/wsx"
	"github.com/axyzxyz/kubeui/backend/internal/service"
)

// terminal 协议帧(01-architecture §6.2):文本帧为控制,二进制帧为数据。
const (
	terminalExitType = "terminal:exit"
	terminalStdin    = "terminal:stdin"
	terminalResize   = "terminal:resize"
	defaultShell     = "/bin/sh"
)

// GET /api/v1/clusters/:cluster/pods/:name/terminal?namespace=&container=&shell=
// SPDY exec ↔ WS 桥接:文本帧 stdin/resize 上行,二进制帧 stdout 下行,
// 远端退出发 terminal:exit;会话开启/关闭写审计(01 §7.4)。
func podTerminal(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident, ok := wsIdentity(c, d)
		if !ok {
			c.JSON(401, errUnauthorizedBody())
			return
		}
		if !middleware.HasPermScope(ident, "terminal:use", c.Param("cluster"), nsOf(c)) {
			c.JSON(403, gin.H{"code": errcode.Forbidden, "message": "insufficient privilege: terminal:use", "data": nil})
			return
		}
		conn, err := newWatchConn("terminal-"+c.GetString("requestId"), c.Writer, c.Request)
		if err != nil {
			return
		}
		defer conn.close()

		var (
			cluster   = c.Param("cluster")
			namespace = nsOf(c)
			name      = c.Param("name")
			shell     = c.DefaultQuery("shell", defaultShell)
			container = c.Query("container")
		)
		sessionStart := time.Now()
		recordTerminalAudit(d, c, ident, "terminal-open", namespace, name, sessionStart)

		exec, err := newExecExecutor(d, cluster, namespace, name, container, shell)
		if err != nil {
			sendWSError(conn, err)
			return
		}

		sizeQueue := newTerminalSizeQueue()
		stdinReader, stdinWriter, closeStdin := newStdinPipe()
		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()

		// 读泵:解析上行控制帧;受控生命周期 worker,连接关闭或 exec 退出后退出。
		go pumpTerminalInput(ctx, conn, stdinWriter, sizeQueue)

		options := remotecommand.StreamOptions{
			Stdin:             stdinReader,
			Stdout:            &wsStdoutWriter{conn: conn, ctx: ctx},
			Stderr:            &wsStdoutWriter{conn: conn, ctx: ctx},
			Tty:               true,
			TerminalSizeQueue: sizeQueue,
		}
		exitCode := 0
		if err := exec.StreamWithContext(ctx, options); err != nil {
			exitCode = 1
			logx.Warn(ctx, "terminal exec ended with error",
				"cluster", cluster, "namespace", namespace, "resource_type", "pod", "name", name, "err", err)
		}
		closeStdin()
		conn.Send(wsx.NewEnvelope(terminalExitType, "", map[string]any{"code": exitCode}))
		recordTerminalAudit(d, c, ident, "terminal-close", namespace, name, sessionStart)
	}
}

// newExecExecutor 构建 SPDY exec 执行器(remotecommand.NewExecutor)。
func newExecExecutor(d Deps, cluster, namespace, name, container, shell string) (remotecommand.Executor, error) {
	rt, err := d.Manager.Get(cluster)
	if err != nil {
		return nil, errcode.New(errcode.ClusterNotFound, "cluster not found").WithCause(err)
	}
	execOptions := &corev1.PodExecOptions{
		Container: container,
		Command:   []string{shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}
	req := rt.ClientSet.CoreV1().RESTClient().
		Post().Resource("pods").Namespace(namespace).Name(name).SubResource("exec").
		VersionedParams(execOptions, runtime.NewParameterCodec(k8sscheme.Scheme))
		// SPDY exec:唯一官方 exec 通道;经 rest.Config.Dial 走 Agent 隧道(若注入)。
	return remotecommand.NewSPDYExecutor(rt.RestConfig, "POST", req.URL())
}

// newStdinPipe 创建 exec stdin 桥:返回读端(exec 侧)、写函数(WS 读泵侧)
// 与关闭函数(exec 退出后调用,避免管道泄漏)。
func newStdinPipe() (*io.PipeReader, func([]byte) error, func()) {
	pr, pw := io.Pipe()
	return pr,
		func(b []byte) error { _, err := pw.Write(b); return err },
		func() { _ = pw.Close() } //nolint:errcheck // 关闭错误无处理价值
}

// pumpTerminalInput 解析上行文本帧:stdin 写入 exec,resize 投递尺寸队列。
// 退出条件:ctx 取消或连接关闭;由 podTerminal 启动,无需外部 join。
func pumpTerminalInput(ctx context.Context, conn *watchConn, write func([]byte) error, sizeQueue *terminalSizeQueue) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = conn.ws.SetReadDeadline(time.Now().Add(wsReadTimeout)) //nolint:errcheck // 超时由读错误暴露
		mt, data, err := conn.ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.TextMessage:
			var frame struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			switch frame.Type {
			case terminalStdin:
				_ = write([]byte(frame.Data)) //nolint:errcheck // 管道已断时 exec 侧自然退出
			case terminalResize:
				sizeQueue.enqueue(remotecommand.TerminalSize{Width: frame.Cols, Height: frame.Rows})
			case wsx.TypePing:
				conn.Send(wsx.NewEnvelope(wsx.TypePong, "", nil))
			}
		}
	}
}

// terminalSizeQueue 实现 remotecommand.TerminalSizeQueue,经 channel 接收 resize。
type terminalSizeQueue struct {
	mu   sync.Mutex
	ch   chan remotecommand.TerminalSize
	done chan struct{}
}

// newTerminalSizeQueue 构造尺寸队列。
func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 8), done: make(chan struct{})}
}

// enqueue 投递一次 resize(非阻塞,丢弃积压)。
func (q *terminalSizeQueue) enqueue(s remotecommand.TerminalSize) {
	select {
	case q.ch <- s:
	default:
	}
}

// Next 实现 remotecommand.TerminalSizeQueue;阻塞等待下一次 resize,队列关闭返回 nil。
func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.done:
		return nil
	}
}

// wsStdoutWriter 把 exec 输出按二进制帧写回 WS。
type wsStdoutWriter struct {
	conn *watchConn
	ctx  context.Context
}

// Write 实现 io.Writer。
func (w *wsStdoutWriter) Write(p []byte) (int, error) {
	_ = w.conn.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck // 写错误由 WriteMessage 暴露
	if err := w.conn.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// recordTerminalAudit 记录终端会话开启/关闭审计(命令内容不记录,01 §7.4)。
func recordTerminalAudit(d Deps, c *gin.Context, ident middleware.Identity, action, namespace, name string, start time.Time) {
	if err := d.Audit.Record(c.Request.Context(), service.AuditEntry{
		RequestID:    c.GetString("requestId"),
		UserID:       ident.UserID,
		Username:     ident.Username,
		Action:       action,
		Resource:     "pods",
		ResourceType: "pod",
		Cluster:      c.Param("cluster"),
		Namespace:    namespace,
		Name:         name,
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		Result:       "allow",
	}); err != nil {
		// 审计失败不阻断业务,但必须可见。
		logx.Warn(c.Request.Context(), "terminal audit record failed", "err", err)
	}
}
