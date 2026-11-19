package maps

// 宿主侧进程内 API（仿 plugins/pay/client.go 范式）：业务插件（如官网联系页的
// 地图渲染）import 本包读取「浏览器端可下发的配置」——publicKey 本来就是给浏览器
// JS 用的，可下发；服务端专用 Key（ServerKey）不出这个包。
// 结果按请求语言判定国内外由调用方决定，这里只提供事实（哪些厂商启用、浏览器 Key）。

import (
	"context"
)

var defaultSvc = make(chan *service, 1)

// SetDefault 由插件装配层调用，注册进程级默认服务。
func SetDefault(s *service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

func current() *service {
	select {
	case s := <-defaultSvc:
		defaultSvc <- s
		return s
	default:
		return nil
	}
}

// BrowserProvider 是公开站地图渲染所需的一家厂商配置：
// Enabled + PublicKey（浏览器 JS 用的 Key，天地图 tk / 谷歌 API Key）。
// 服务端专用 Key 永远不出这个包。
type BrowserProvider struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey"`
}

// BrowserConfig 是公开站地图渲染的浏览器端配置：厂商 id → 配置。
type BrowserConfig map[Provider]BrowserProvider

// BrowserConfig 读取浏览器端可下发的厂商配置（publicKey 可下发，服务端 Key 不出包）。
func (s *service) browserConfig(ctx context.Context) BrowserConfig {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil
	}
	out := BrowserConfig{}
	for p, c := range cfg {
		out[p] = BrowserProvider{Enabled: c.Enabled, PublicKey: c.PublicKey}
	}
	return out
}

// PublicBrowserConfig 供宿主插件调用的包级入口；插件未装配时返回 nil（调用方按
// 「地图不可用，只渲染地址」兜底）。
func PublicBrowserConfig(ctx context.Context) BrowserConfig {
	s := current()
	if s == nil {
		return nil
	}
	return s.browserConfig(ctx)
}
