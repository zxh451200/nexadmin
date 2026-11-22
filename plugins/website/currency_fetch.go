package website

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"nexadmin/core/response"
)

// 汇率表「一键拉取」：管理员主动从公开汇率源取最新价，填进站点设置的汇率编辑器。
//
// 设计取舍：
//   - 不定时、不自动保存：拉取结果先回到编辑器，管理员确认后再随站点设置保存。
//     内网部署拉不到外网时报错即可，手动录入这条路始终可用。
//   - 只更新当前表里已有的币种（USD 恒 1），不擅自增删——表里有哪些币种是管理员
//     选定的展示口径，拉取只是刷新数值。
//   - 数据源写死在代码里（不接受前端传入 URL，杜绝 SSRF）：主源 open.er-api.com
//     （免 key、每小时更新、160+ 币种），失败回退 Frankfurter（欧洲央行官方参考汇率、
//     每日更新、稳定）。两个源都返回「1 USD = N 目标币种」，与汇率表口径一致。

const (
	fxPrimaryURL  = "https://open.er-api.com/v6/latest/USD"
	fxFallbackURL = "https://api.frankfurter.dev/v1/latest?base=USD"
	fxPrimaryName = "ExchangeRate-API"
	fxFallback    = "Frankfurter (ECB)"
	fxMaxBody     = 1 << 20 // 汇率 JSON 上限 1MB，防止异常大响应吃内存
	fxTimeout     = 8 * time.Second
)

// fxFetchClient 是拉取汇率用的 HTTP 客户端：短超时 + 限制重定向。
// 目标 URL 是代码常量（非用户输入），不存在 SSRF；这里主要防慢响应与大响应。
var fxFetchClient = &http.Client{
	Timeout: fxTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// currencyFetchView 是拉取接口回给前端的结果，前端据此更新编辑器（不自动保存）。
type currencyFetchView struct {
	Rates   string `json:"rates"`
	Updated int    `json:"updated"`
	Source  string `json:"source"`
}

// fetchCurrencyRates 从公开源拉取当前表内币种的最新汇率，返回新的汇率表文本、更新条数与来源名。
// codes 来自当前编辑器已有的币种（含 USD）；不在公开源里的币种保持原值不动。
func fetchCurrencyRates(ctx context.Context, currentRaw string, codes []string) (*currencyFetchView, error) {
	current, err := parseCurrencyRates(currentRaw)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, response.BadRequest("website.currency_fetch_empty")
	}

	live, source, sourceErr := fetchUSDollarRates(ctx)
	if sourceErr != nil {
		return nil, response.BadRequest("website.currency_fetch_failed")
	}

	updated := 0
	for _, code := range codes {
		if code == baseCurrency {
			continue
		}
		if rate, ok := live[code]; ok && rate > 0 {
			if current[code] != rate {
				updated++
			}
			current[code] = rate
		}
	}
	current[baseCurrency] = 1

	b, err := json.Marshal(current)
	if err != nil {
		return nil, response.Internal()
	}
	return &currencyFetchView{Rates: string(b), Updated: updated, Source: source}, nil
}

// fetchUSDollarRates 依次尝试主源与备源，返回「1 USD = N 币种」的汇率表与来源名。
// 结果至少要有 10 个主流币种才认为有效，避免把半截/错误响应写进汇率表。
func fetchUSDollarRates(ctx context.Context) (map[string]float64, string, error) {
	sources := []struct {
		url  string
		name string
	}{{fxPrimaryURL, fxPrimaryName}, {fxFallbackURL, fxFallback}}
	var lastErr error
	for _, s := range sources {
		rates, err := fetchOneSource(ctx, s.url)
		if err == nil && len(rates) >= 10 && rates[baseCurrency] == 1 {
			return rates, s.name, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("source %s returned too few rates", s.name)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no exchange rate source")
	}
	return nil, "", lastErr
}

// fetchOneSource 读取并解析单个汇率源，统一成 map[币种] = 每 1 USD 可兑换的单位数。
func fetchOneSource(ctx context.Context, url string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := fxFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fxMaxBody))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	// open.er-api 成功时 result=success；Frankfurter 没有该字段。
	if payload.Result != "" && payload.Result != "success" {
		return nil, fmt.Errorf("source result=%s", payload.Result)
	}
	out := make(map[string]float64, len(payload.Rates))
	for code, rate := range payload.Rates {
		if !currencyCodeRe.MatchString(code) || rate <= 0 || rate > 1e9 {
			continue
		}
		out[code] = rate
	}
	out[baseCurrency] = 1
	return out, nil
}

// codesFromRates 从当前汇率表文本取币种码（升序）。只更新表里已经存在的币种，
// 与前端编辑器保持同一口径——拉取不擅自给管理员加币种植。
func codesFromRates(raw string) []string {
	rates, err := parseCurrencyRates(raw)
	if err != nil || rates == nil {
		return nil
	}
	codes := make([]string, 0, len(rates))
	for code := range rates {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
