package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

// SSEKeepaliveInterval 是心跳注释帧的间隔。中间代理常在 30~60s 无数据时断开连接，
// 15s 留了足够余量。
const SSEKeepaliveInterval = 15 * time.Second

// SSEWriter 把一个普通响应改造成 Server-Sent Events 流。
//
// 有两处必须显式处理，否则表现为「进度条卡住 30 秒后断开」，且极难排查：
//  1. 服务端设了 WriteTimeout=30s，长连接会被整体掐断——这里按连接取消写截止时间
//  2. Gzip 中间件会缓冲输出——由 main.go 的 Skipper 跳过 SSE 路径负责
type SSEWriter struct {
	c    *echo.Context
	ctrl *http.ResponseController
}

// NewSSEWriter 写出 SSE 响应头并取消该连接的写超时。
func NewSSEWriter(c *echo.Context) (*SSEWriter, error) {
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// 禁用 Nginx 的响应缓冲，否则事件会攒够一批才下发。
	h.Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	ctrl := http.NewResponseController(c.Response())
	// 零值截止时间表示「不设截止」。这里只影响当前连接，不改全局 WriteTimeout。
	if err := ctrl.SetWriteDeadline(time.Time{}); err != nil {
		// 部分 ResponseWriter 不支持设置截止时间；此时仍可推流，只是会受 WriteTimeout 限制。
		// 不作为致命错误，但要让调用方知道。
		return &SSEWriter{c: c, ctrl: ctrl}, fmt.Errorf("取消写超时失败，长连接可能被 WriteTimeout 掐断: %w", err)
	}
	return &SSEWriter{c: c, ctrl: ctrl}, nil
}

// Send 推送一个带 id 与事件名的数据帧。id 供客户端断线后用 Last-Event-ID 续传。
func (w *SSEWriter) Send(id int64, event, data string) error {
	if _, err := fmt.Fprintf(w.c.Response(), "id: %d\nevent: %s\ndata: %s\n\n", id, event, data); err != nil {
		return err
	}
	return w.ctrl.Flush()
}

// Keepalive 推送一个注释帧。它不会触发客户端的 onmessage，只用于保活。
func (w *SSEWriter) Keepalive() error {
	if _, err := fmt.Fprint(w.c.Response(), ": keepalive\n\n"); err != nil {
		return err
	}
	return w.ctrl.Flush()
}

// LastEventID 取客户端的续传位置。浏览器原生 EventSource 自动带 Last-Event-ID 头，
// 而手工重连或 fetch 实现只能用查询参数，两者都要支持。
func LastEventID(c *echo.Context) string {
	if v := c.Request().Header.Get("Last-Event-ID"); v != "" {
		return v
	}
	return c.Request().URL.Query().Get("last_event_id")
}

// IsSSEPath 判断请求是否指向流式接口，供 Gzip 中间件的 Skipper 使用。
// Gzip 会缓冲输出，套在 SSE 上会让事件迟迟不下发。
func IsSSEPath(path string) bool {
	const suffix = "/stream"
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}
