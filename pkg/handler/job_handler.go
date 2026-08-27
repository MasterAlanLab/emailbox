package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// eventBatch 是一次从库里补齐事件的上限。5000 个账号的任务会产生同样多的
// item 事件，一次全读出来既占内存又让首帧迟迟不发。
const eventBatch = 200

// sseIdleTick 是没有任何新事件时的轮询间隔。
// 广播信号是主要的唤醒源，这个 tick 只是兜底：万一某次 Notify 因为
// 订阅者正忙而被丢掉，最多晚一秒补上，不会永久卡住。
const sseIdleTick = time.Second

type JobHandler struct {
	jobs    *service.JobService
	refresh *service.RefreshService
}

func NewJobHandler(jobs *service.JobService, refresh *service.RefreshService) *JobHandler {
	return &JobHandler{jobs: jobs, refresh: refresh}
}

func (h *JobHandler) List(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.JobFilter{
		Type:   q.Get("type"),
		Status: q.Get("status"),
		Page:   atoiOrZero(q.Get("page")),
		Limit:  atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.jobs.List(c.Request().Context(), c.Param("tenantID"), filter)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
}

func (h *JobHandler) Get(c *echo.Context) error {
	v, err := h.jobs.Get(c.Request().Context(), c.Param("tenantID"), c.Param("jobID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "获取成功")
}

func (h *JobHandler) Items(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.JobItemFilter{
		Status: q.Get("status"),
		Page:   atoiOrZero(q.Get("page")),
		Limit:  atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.jobs.Items(c.Request().Context(),
		c.Param("tenantID"), c.Param("jobID"), filter)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
}

func (h *JobHandler) Stop(c *echo.Context) error {
	err := h.jobs.Stop(c.Request().Context(), c.Param("tenantID"), c.Param("jobID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, nil, "已请求停止，正在处理的账号会先完成")
}

// Stream 是任务进度的 SSE 端点。
//
// 实时推送与断线重连走的是同一条代码路径：都是「从 last_seq 之后读 job_events」。
// 广播只用来唤醒，事件内容一律从库里取。这样做的好处是慢客户端不会丢事件——
// 它只是晚一点读到，而不是永远错过（把事件直接推给订阅者的话，
// 处理慢订阅者的唯一办法就是丢，一丢流就断档了）。
func (h *JobHandler) Stream(c *echo.Context) error {
	ctx := c.Request().Context()
	tenantID, jobID := c.Param("tenantID"), c.Param("jobID")

	// 先确认任务存在且属于该租户，再升级成 SSE。
	// 顺序反了的话，越权请求会拿到一个 200 的空流，而不是 404。
	job, err := h.jobs.Get(ctx, tenantID, jobID)
	if err != nil {
		return mailError(c, err)
	}

	// 解析不了就从 0 开始，即把整段历史补一遍。客户端给了个坏值时，
	// 重发一遍远好过静默跳过——SSE 的事件都是幂等的展示数据。
	lastSeq, err := strconv.ParseInt(LastEventID(c), 10, 64)
	if err != nil {
		lastSeq = 0
	}

	// 先订阅再补历史：反过来的话，两步之间产生的事件谁都不会通知，
	// 而它已经不在刚才那批历史里了——表现为进度条卡在中间不动。
	signal, unsubscribe := h.jobs.Subscribe(jobID)
	defer unsubscribe()

	writer, err := NewSSEWriter(c)
	if err != nil {
		// 取消写超时失败不致命，仍然可以推流，只是可能被 WriteTimeout 掐断。
		c.Logger().Warn("SSE 写超时未能取消", "error", err)
	}

	keepalive := time.NewTicker(SSEKeepaliveInterval)
	defer keepalive.Stop()
	idle := time.NewTicker(sseIdleTick)
	defer idle.Stop()

	terminal := model.IsTerminalJobStatus(job.Status)

	for {
		sent, err := h.drain(ctx, tenantID, jobID, &lastSeq, writer)
		if err != nil {
			return nil // 客户端断开是常态，不当错误
		}

		// 任务已经终结且事件全部发完，可以收流了。
		// 必须先确认发完再退出：finished 事件恰恰是客户端最需要的那一条。
		if terminal && sent == 0 {
			return nil
		}
		if !terminal {
			current, err := h.jobs.Get(ctx, tenantID, jobID)
			if err == nil {
				terminal = model.IsTerminalJobStatus(current.Status)
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-signal:
		case <-idle.C:
		case <-keepalive.C:
			if err := writer.Keepalive(); err != nil {
				return nil
			}
		}
	}
}

// drain 把 lastSeq 之后的事件一次性发完，返回本轮发送的条数。
func (h *JobHandler) drain(
	ctx context.Context, tenantID, jobID string, lastSeq *int64, writer *SSEWriter,
) (int, error) {
	sent := 0
	for {
		events, err := h.jobs.Events(ctx, tenantID, jobID, *lastSeq, eventBatch)
		if err != nil || len(events) == 0 {
			return sent, err
		}
		for _, event := range events {
			if err := writer.Send(event.Seq, event.Kind, event.Payload); err != nil {
				return sent, err
			}
			*lastSeq = event.Seq
			sent++
		}
		if len(events) < eventBatch {
			return sent, nil
		}
	}
}

// ---------- 令牌刷新 ----------

type RefreshHandler struct{ service *service.RefreshService }

func NewRefreshHandler(s *service.RefreshService) *RefreshHandler {
	return &RefreshHandler{service: s}
}

func (h *RefreshHandler) RefreshOne(c *echo.Context) error {
	err := h.service.RefreshOne(c.Request().Context(), c.Param("tenantID"), c.Param("accountID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, nil, "刷新成功")
}

type submitRefreshRequest struct {
	Scope      string   `json:"scope"`
	AccountIDs []string `json:"account_ids"`
	// scope=group 时生效：只刷这些分组下的账号。
	GroupIDs []string `json:"group_ids"`
}

func (h *RefreshHandler) SubmitBatch(c *echo.Context) error {
	var req submitRefreshRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	job, err := h.service.SubmitBatch(c.Request().Context(),
		c.Param("tenantID"), middleware.UserID(c), req.Scope, req.AccountIDs, req.GroupIDs)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, job, "任务已提交")
}

func (h *RefreshHandler) Stats(c *echo.Context) error {
	v, err := h.service.Stats(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "获取成功")
}

func (h *RefreshHandler) Logs(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.RefreshLogFilter{
		Status:    q.Get("status"),
		AccountID: q.Get("account_id"),
		JobID:     q.Get("job_id"),
		Page:      atoiOrZero(q.Get("page")),
		Limit:     atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.service.Logs(c.Request().Context(), c.Param("tenantID"), filter)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
}
