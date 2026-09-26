package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"emailbox/pkg/mailer"
)

// batchLimit 是 Graph 的 $batch 单次请求上限。超过会整批 400。
const batchLimit = 20

// batchRequest 是 $batch 请求里的一条子请求。
type batchRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

type batchResponse struct {
	Responses []struct {
		ID     string          `json:"id"`
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"responses"`
}

// MarkRead 实现 mailer.Client。
func (c *Client) MarkRead(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return c.batchOp(ctx, cred, items, "PATCH", map[string]any{"isRead": true})
}

// Delete 实现 mailer.Client。
func (c *Client) Delete(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return c.batchOp(ctx, cred, items, "DELETE", nil)
}

// batchOp 用 $batch 对一批邮件执行同一个操作。
//
// Graph 没有原生的批量标已读/删除接口，outlookEmail 是逐条循环。改走 $batch 后
// 每 20 条一个往返——批量场景下这是 20 倍的往返差距，也是本方案对它的一处明确优化。
// $batch 本身失败时回退逐条，避免因为这个优化反而比原来更不可用。
func (c *Client) batchOp(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef, method string, body any,
) (mailer.BatchResult, error) {
	if len(items) == 0 {
		return mailer.BatchResult{}, nil
	}
	return withSession(ctx, c, cred, func(ctx context.Context, s *session) (mailer.BatchResult, error) {
		result := mailer.BatchResult{Items: make([]mailer.ItemResult, 0, len(items))}
		for start := 0; start < len(items); start += batchLimit {
			end := min(start+batchLimit, len(items))
			chunk := items[start:end]

			chunkResults, err := c.runBatch(ctx, s, chunk, method, body)
			if err != nil {
				// 整批失败分两种：一种是这批请求本身不被接受（回退逐条还有机会），
				// 一种是令牌/账号问题（逐条也是一样的结果，直接把错误抛上去，
				// 让回退链决定要不要换通道）。
				if !batchWorthFallback(err) {
					return mailer.BatchResult{}, err
				}
				chunkResults = c.runOneByOne(ctx, s, chunk, method, body)
			}
			for _, r := range chunkResults {
				if r.OK {
					result.Succeeded++
				} else {
					result.Failed++
				}
				result.Items = append(result.Items, r)
			}
		}
		return result, nil
	})
}

// batchWorthFallback 判断 $batch 整批失败后逐条重试是否还有意义。
func batchWorthFallback(err error) bool {
	switch mailer.KindOf(err) {
	case mailer.ErrKindAuthFailed, mailer.ErrKindBanned, mailer.ErrKindConsentRequired,
		mailer.ErrKindCanceled, mailer.ErrKindRateLimited:
		return false
	default:
		return true
	}
}

func (c *Client) runBatch(
	ctx context.Context, s *session, chunk []mailer.MessageRef, method string, body any,
) ([]mailer.ItemResult, error) {
	requests := make([]batchRequest, 0, len(chunk))
	for i, item := range chunk {
		req := batchRequest{
			ID:     fmt.Sprint(i),
			Method: method,
			URL:    "/me/messages/" + url.PathEscape(item.ID),
		}
		if body != nil {
			req.Body = body
			req.Headers = map[string]string{"Content-Type": "application/json"}
		}
		requests = append(requests, req)
	}

	var payload batchResponse
	if err := c.doJSON(ctx, s, "POST", "/$batch",
		map[string]any{"requests": requests}, &payload); err != nil {
		return nil, err
	}

	// 按 id 索引：Graph 不保证 responses 的顺序与 requests 一致。
	byID := make(map[string]int, len(payload.Responses))
	for i, r := range payload.Responses {
		byID[r.ID] = i
	}
	out := make([]mailer.ItemResult, 0, len(chunk))
	for i, item := range chunk {
		idx, ok := byID[fmt.Sprint(i)]
		if !ok {
			out = append(out, mailer.ItemResult{Ref: item, Error: "Graph 未返回该条的结果"})
			continue
		}
		resp := payload.Responses[idx]
		if resp.Status >= 200 && resp.Status < 300 {
			out = append(out, mailer.ItemResult{Ref: item, OK: true})
			continue
		}
		out = append(out, mailer.ItemResult{
			Ref:   item,
			Error: classifyAPIError(resp.Status, resp.Body).Error(),
		})
	}
	return out, nil
}

// runOneByOne 是 $batch 不可用时的回退：逐条发请求。
func (c *Client) runOneByOne(
	ctx context.Context, s *session, chunk []mailer.MessageRef, method string, body any,
) []mailer.ItemResult {
	out := make([]mailer.ItemResult, 0, len(chunk))
	for _, item := range chunk {
		if err := ctx.Err(); err != nil {
			out = append(out, mailer.ItemResult{Ref: item, Error: "请求已取消"})
			continue
		}
		path := "/me/messages/" + url.PathEscape(item.ID)
		if err := c.doJSON(ctx, s, method, path, body, nil); err != nil {
			out = append(out, mailer.ItemResult{Ref: item, Error: err.Error()})
			continue
		}
		out = append(out, mailer.ItemResult{Ref: item, OK: true})
	}
	return out
}
