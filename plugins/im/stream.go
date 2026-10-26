package im

// SSE 实时推送。鉴权说明：EventSource 带不了 Authorization 头，坐席先用
// POST /stream-ticket 换一张 30 秒一次性流票拼在流 URL 上；访客侧以不可枚举的
// 会话号作能力令牌直接订阅。断线时前端回落到增量轮询（既有 GET messages）。

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

const streamHeartbeat = 15 * time.Second

// POST /plugins/im/stream-ticket —— 坐席换一张一次性 SSE 入场票。
func (h *handler) streamTicket(c *gin.Context) {
	response.OK(c, gin.H{"ticket": h.tickets.issue()})
}

// GET /plugins/im/stream/:no?ticket=... —— 坐席实时流（票一次性，用后作废）。
func (h *handler) adminStream(c *gin.Context) {
	if !h.tickets.take(c.Query("ticket")) {
		response.Fail(c, response.Unauthorized("im.stream_ticket_invalid"))
		return
	}
	h.serveStream(c, c.Param("no"))
}

// GET /plugins/im/visitor/stream/:no —— 访客实时流。
func (h *handler) visitorStream(c *gin.Context) {
	h.serveStream(c, c.Param("no"))
}

func (h *handler) serveStream(c *gin.Context, convNo string) {
	w := c.Writer
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no") // 反代不缓冲
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	// 先发一行注释，让代理立刻把流建起来。
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, cancel := h.svc.Subscribe(convNo)
	defer cancel()
	ticker := time.NewTicker(streamHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done(): // 客户端断开
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
