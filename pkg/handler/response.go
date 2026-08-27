package handler

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
)

// 业务错误码。0 表示成功、1 表示通用失败；以下是需要前端分支处理的场景，
// 定义见 docs/plan/05-api-design.md §1.2。
const (
	CodeOK      = 0
	CodeFailure = 1

	CodeQuotaExceeded    = 1001 // 超出配额，data 为 null，上限与已用量在 message 里
	CodeAccountDisabled  = 1003 // 用户已被管理员禁用
	CodeMailAccountExist = 1004 // 邮箱账号已存在
	CodeUpstreamMailErr  = 1005 // 上游邮件服务失败，data 带 {error_kind, channel}
)

func success(c *echo.Context, data any, message string) error {
	return c.JSON(http.StatusOK, map[string]any{"code": CodeOK, "data": data, "message": message})
}
func failure(c *echo.Context, status int, err error) error {
	message := err.Error()
	// 5xx 属于内部错误，原始信息（SQL、驱动报错等）只记日志，不回传给客户端。
	if status >= http.StatusInternalServerError {
		// 带上 request_id，便于和访问日志中的同一条请求对应起来。
		slog.Error("请求处理失败",
			"status", status,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
			"request_id", c.Response().Header().Get(echo.HeaderXRequestID),
			"error", err)
		message = "服务器内部错误"
	}
	return c.JSON(status, map[string]any{"code": CodeFailure, "data": nil, "message": message})
}
