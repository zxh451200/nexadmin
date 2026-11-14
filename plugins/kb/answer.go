package kb

// 回答编排：检索命中 → 严格受限的系统提示 → core/ai 网关对话。
// 「只依据资料、没有就说不知道」的硬约束写死在这里，设置页的 SystemPrompt 只能追加要求，
// 不能解除约束——这是「AI 不瞎回答」的第二道闸（第一道是检索阈值）。

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nexadmin/core/ai"
	"nexadmin/core/response"
)

// AnswerResult 是一次问答的结果。Handoff 为 true 表示检索未命中，没有调用模型，
// 调用方（测试框 / IM 机器人）应走兜底话术并转人工。
type AnswerResult struct {
	Answer  string `json:"answer"`
	Hits    []Hit  `json:"hits"`
	Handoff bool   `json:"handoff"`
}

// answerTimeout 单轮问答的超时：知识问答不需要工具循环，60s 足够。
const answerTimeout = 60 * time.Second

// Answer 基于知识库回答一个问题（测试提问与 IM 机器人共用）。
func (s *Service) Answer(ctx context.Context, who ai.Caller, question string) (*AnswerResult, error) {
	if s.gw == nil {
		return nil, response.Unavailable("kb.plugin_inactive")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, response.BadRequest("kb.question_required")
	}
	if utf8.RuneCountInString(question) > maxAskRunes {
		return nil, response.BadRequest("kb.question_too_long", maxAskRunes)
	}
	hits, err := s.Retrieve(ctx, question)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return &AnswerResult{Hits: hits, Handoff: true}, nil
	}
	cfg, err := s.gw.Load(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Ready() {
		return nil, response.Unavailable("kb.chat_unavailable")
	}
	st := s.Settings(ctx)
	run, err := s.gw.Run(ctx, who, ai.RunRequest{
		Messages:  []ai.Message{{Role: "system", Content: buildSystemPrompt(st, hits)}, {Role: "user", Content: question}},
		MaxRounds: 1, Timeout: answerTimeout,
	}, nil)
	if err != nil {
		if s.log != nil {
			s.log.Warn("kb: answer call failed", "err", err.Error())
		}
		switch {
		case errors.Is(err, ai.ErrDisabled):
			return nil, response.Unavailable("kb.chat_unavailable")
		case errors.Is(err, ai.ErrRateLimited):
			return nil, response.New(429, "kb.rate_limited")
		default:
			return nil, response.New(502, "kb.answer_failed")
		}
	}
	answer := ""
	for _, m := range run.Messages { // 取最后一条 assistant 输出
		if m.Role == "assistant" {
			answer = m.Content
		}
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, response.New(502, "kb.answer_failed")
	}
	return &AnswerResult{Answer: truncateRunes(answer, 1800), Hits: hits}, nil
}

// buildSystemPrompt 组装系统提示：硬约束 + 知识块（带序号与文档标题）+ 管理员追加要求。
func buildSystemPrompt(st Settings, hits []Hit) string {
	var b strings.Builder
	b.WriteString("你是客服助手。请只依据下面的知识资料回答用户问题；")
	b.WriteString("资料里没有的信息一律不要猜测、不要编造、不要用通用知识补充，")
	b.WriteString("直接回答「抱歉，这个问题我暂时回答不了」。回答简洁、礼貌，用与用户相同的语言。\n\n知识资料：\n")
	for i, h := range hits {
		b.WriteString("[")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("] ")
		b.WriteString(h.Title)
		b.WriteString("\n")
		b.WriteString(h.Excerpt)
		b.WriteString("\n\n")
	}
	if st.SystemPrompt != "" {
		b.WriteString("回答风格要求：\n")
		b.WriteString(st.SystemPrompt)
	}
	return b.String()
}
