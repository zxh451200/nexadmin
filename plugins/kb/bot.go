package kb

// IM 自动回答编排器：订阅 im.message.created（进程内事件总线，见 core/event），
// 访客提问 → 知识库检索 → 命中则以 AI 身份回答；未命中或调用失败则礼貌说明并转人工排队。
//
// 触发面与 plugins/im/notify.go 同款：handle 只做过滤与入队（不阻塞事件分发），
// 耗时的检索与模型调用在 worker 协程里做。多实例防重用共享缓存的 Incr 计数。

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"nexadmin/core"
	"nexadmin/core/ai"
	"nexadmin/core/cache"
	"nexadmin/core/event"
	"nexadmin/core/lifecycle"
	"nexadmin/core/tenant"
	im "nexadmin/plugins/im"
)

const (
	// answerQueue 是待答消息队列深度；满了丢弃并记日志（SSE/轮询不影响，访客可追问）。
	answerQueue = 64
	// seenTTL 是多实例防重键的存活期：覆盖检索+模型的耗时上限即可。
	seenTTL = 3 * time.Minute
	// historyLimit 是构建上下文时带入的最近消息数。
	historyLimit = 12
	// botRatePerMin / botDailyCap 是 bot 级全局频控：kb 的 Caller 是 Exempt（不受租户
	// 配额约束），且限流键按会话拆——没有这一层，匿名访客的 AI 调用就没有任何全局
	// 上限（embedding 在限流检查之前就已发出）。分钟 30 封顶并发问答、每日 500 封顶
	// 成本；超限走兜底转人工（单会话有 BotPaused 防刷，跨会话兜底量也有界）。
	botRatePerMin = 30
	botDailyCap   = 500
)

// botName 是 AI 回复显示的发送方名（挂件会加 AI 徽标）。
const botName = "AI"

// defaultFallbackText 是 IM 设置没配兜底话术时的默认文案。
const defaultFallbackText = "抱歉，这个问题我暂时回答不了，已为您转接人工客服。"

type bot struct {
	ctx   context.Context // 插件运行 ctx：停用后订阅者变成空操作、worker 退出
	svc   *Service
	gw    *ai.Gateway
	cache cache.Cache
	log   logger
	queue chan im.EventData
}

func newBot(ctx context.Context, d *core.Deps, svc *Service) *bot {
	if d == nil || d.DB == nil {
		return nil
	}
	b := &bot{ctx: ctx, svc: svc, gw: svc.gw, cache: d.Cache, log: svc.log, queue: make(chan im.EventData, answerQueue)}
	return b
}

func (b *bot) subscribe(rt *lifecycle.Runtime) { rt.Subscribe(b) }

// Handle 在事件总线分发协程里被调用：只做过滤、防重与入队。
func (b *bot) Handle(e event.Event) {
	if b.ctx.Err() != nil || e.Name != im.EventMessageCreated {
		return
	}
	var data im.EventData
	switch v := e.Data.(type) {
	case im.EventData:
		data = v
	case *im.EventData:
		if v == nil {
			return
		}
		data = *v
	default:
		return
	}
	// 只应答访客消息：bot/agent/system 消息一律忽略（也防住自己回复引发的死循环）。
	if data.SenderType != im.SenderVisitor || data.ConvNo == "" {
		return
	}
	if b.duplicate(data) {
		return
	}
	select {
	case b.queue <- data:
	default:
		if b.log != nil {
			b.log.Warn("kb: answer queue full, dropped", "convNo", data.ConvNo, "seq", data.Seq)
		}
	}
}

// duplicate 多实例防重：共享缓存上对 (convNo, seq) 做自增，拿到 v>1 的实例直接放弃。
// 无 Counter 的回落用 SetNX（原子占位）替代 Exists+Set 两步——两步之间另一实例
// 可同时通过，造成重复应答。
func (b *bot) duplicate(data im.EventData) bool {
	if b.cache == nil {
		return false
	}
	key := "kb:seen:" + data.ConvNo + ":" + strconv.FormatInt(data.Seq, 10)
	if c, ok := b.cache.(cache.Counter); ok {
		v, _, err := c.Incr(b.ctx, key, seenTTL)
		return err == nil && v > 1
	}
	if l, ok := cache.As[cache.Locker](b.cache); ok {
		won, err := l.SetNX(b.ctx, key, "1", seenTTL)
		if err != nil {
			return false // 缓存故障放行（与限流 fail-open 同一取向），漏答可追问
		}
		return !won
	}
	ok, _ := b.cache.Exists(b.ctx, key)
	if ok {
		return true
	}
	_ = b.cache.Set(b.ctx, key, "1", seenTTL)
	return false
}

// botAllow bot 级全局频控：分钟窗口 + 当日计数两层，cache 计数多实例共享。
// 超限返回 false，由调用方走兜底转人工。
func (b *bot) botAllow(ctx context.Context) bool {
	if b.cache == nil {
		return true // 无缓存不设防（单实例部署本来也没有共享状态可计）
	}
	now := time.Now()
	if c, ok := b.cache.(cache.Counter); ok {
		n, _, err := c.Incr(ctx, "kb:bot:rate:"+now.Format("200601021504"), 2*time.Minute)
		if err == nil && n > botRatePerMin {
			return false
		}
		n, _, err = c.Incr(ctx, "kb:bot:day:"+now.Format("20060102"), 26*time.Hour)
		if err == nil && n > botDailyCap {
			return false
		}
		return true
	}
	return true // 无 Counter 的回落不设第二层（与 duplicate 回落同样的边缘场景）
}

func (b *bot) run() {
	for {
		select {
		case data := <-b.queue:
			b.answerSafe(data)
		case <-b.ctx.Done():
			return
		}
	}
}

// answerSafe 单条处理包 recover：worker 是常驻协程，goroutine panic 不可被外层
// recover，一次 panic 就是整个进程崩溃；依赖链（svc/im/cache）出 panic 只该丢
// 这一条消息，日志留 panic 值。
func (b *bot) answerSafe(data im.EventData) {
	defer func() {
		if r := recover(); r != nil && b.log != nil {
			b.log.Error("kb: answer panic", "convNo", data.ConvNo, "panic", r)
		}
	}()
	b.answer(data)
}

// answer 处理一条访客消息。
func (b *bot) answer(data im.EventData) {
	ctx := tenant.Skip(b.ctx) // 会话是平台级表：跳过租户隔离读（与 im 后台同口径）
	st, err := im.LoadSettings(ctx)
	if err != nil || !st.AIEnabled {
		return // 未开启 AI 自动回答：行为与从前完全一致
	}
	if !b.svc.Ready(ctx) {
		// 模型没配完整：静默跳过（保持人工现状），不做兜底刷屏——这是配置问题，不是访客问题。
		return
	}
	if !b.botAllow(ctx) {
		// 全局频控超限：embedding/模型的成本入口先于调用收口，兜底转人工。
		b.fallback(data.ConvNo, st.AIFallbackText)
		return
	}
	conv, msgs, err := im.Messages(ctx, data.ConvNo, 0, historyLimit)
	if err != nil || conv == nil {
		if err != nil && b.log != nil {
			b.log.Warn("kb: load conversation failed", "convNo", data.ConvNo, "err", err.Error())
		}
		return
	}
	// AI 只在「无人接管且未被暂停」时回答；坐席认领或访客转人工后保持沉默。
	if conv.Status != im.StatusOpen || conv.AssigneeID != "" || conv.BotPaused {
		return
	}
	// 短时间里连发多条：只答最新一条，旧事件放弃（新事件会带各自的 seq 触发）。
	if conv.MsgSeq > data.Seq {
		return
	}
	question := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].SenderType == im.SenderVisitor {
			question = msgs[i].Content
			break
		}
	}
	if strings.TrimSpace(question) == "" {
		return
	}
	// im 消息上限 2000 字节而 kb 问答 500 runes：超长的粘贴型提问按头部截断，
	// 而不是必然 400 → 兜底转人工（前 500 字通常已含问题本体，完整原文坐席侧可见）。
	question = truncateRunes(question, maxAskRunes)
	// AI 侧的限流/配额键用会话号（访客匿名，没有用户身份），平台级调用不受租户开关约束。
	who := ai.Caller{UserID: "kb:" + data.ConvNo, Exempt: true}
	res, err := b.svc.Answer(ctx, who, question, false)
	if err != nil {
		if b.log != nil {
			b.log.Warn("kb: auto answer failed", "convNo", data.ConvNo, "err", err.Error())
		}
		b.fallback(data.ConvNo, st.AIFallbackText)
		return
	}
	if res.Handoff { // 知识库答不了：不编造，礼貌说明 + 转人工排队
		// 只有真正的检索未命中才进问题池——频控超限与模型调用失败在上面已 return，
		// 那是系统问题不是知识缺口，记进来只会淹没运营队列。写失败不影响访客兜底。
		if err := b.svc.RecordMiss(ctx, question, data.ConvNo); err != nil && b.log != nil {
			b.log.Warn("kb: record miss failed", "convNo", data.ConvNo, "err", err.Error())
		}
		b.fallback(data.ConvNo, st.AIFallbackText)
		return
	}
	// 命中来源进 meta：访客挂件据此展示「AI 依据了哪些资料」，答案可追溯、不像凭空捏造。
	// 同一文档多个块命中时标题只出现一次。
	meta := ""
	if len(res.Hits) > 0 {
		seen := map[string]bool{}
		titles := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			if seen[h.Title] {
				continue
			}
			seen[h.Title] = true
			titles = append(titles, h.Title)
		}
		if raw, merr := json.Marshal(map[string]any{"sources": titles}); merr == nil {
			meta = string(raw)
		}
	}
	if _, err := im.Send(ctx, data.ConvNo, im.SendInput{
		SenderType: im.SenderBot, SenderName: botName, Content: res.Answer, Meta: meta,
	}); err != nil && b.log != nil {
		b.log.Warn("kb: send answer failed", "convNo", data.ConvNo, "err", err.Error())
	}
}

// fallback 发兜底话术并把会话交给自动分配（在线坐席直接落人，没有就排队）。
func (b *bot) fallback(convNo, text string) {
	if strings.TrimSpace(text) == "" {
		text = defaultFallbackText
	}
	if _, err := im.Send(b.ctx, convNo, im.SendInput{
		SenderType: im.SenderBot, SenderName: botName, Content: text,
	}); err != nil && b.log != nil {
		b.log.Warn("kb: send fallback failed", "convNo", convNo, "err", err.Error())
	}
	if err := im.RequestAgent(b.ctx, convNo); err != nil && b.log != nil {
		b.log.Warn("kb: request agent failed", "convNo", convNo, "err", err.Error())
	}
}
