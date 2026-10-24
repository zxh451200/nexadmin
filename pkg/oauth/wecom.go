package oauth

import (
	"context"
	"net/url"
)

// ---- 企业微信（扫码登录，企业自建应用）----
// client_id = CorpID，client_secret = 应用 Secret，agent_id = 应用 AgentId。

type wecom struct {
	c    Config
	hc   httpDoer
	auth string // 扫码页
	api  string // qyapi 根
}

func newWecom(c Config, hc httpDoer) (Provider, error) {
	if c.AgentID == "" {
		return nil, fail("wecom: agent_id is required")
	}
	return &wecom{c: c, hc: hc, auth: "https://login.work.weixin.qq.com/wwlogin/sso/login", api: "https://qyapi.weixin.qq.com"}, nil
}

func (w *wecom) Name() string   { return "wecom" }
func (w *wecom) UsesPKCE() bool { return false }

func (w *wecom) AuthURL(state, redirect string, _ *PKCE) string {
	return withQuery(w.auth, url.Values{
		"login_type": {"CorpApp"}, "appid": {w.c.ClientID}, "agentid": {w.c.AgentID},
		"redirect_uri": {redirect}, "state": {state},
	})
}

type wecomResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	UserID      string `json:"userid"`
	Name        string `json:"name"`
	Mobile      string `json:"mobile"`
	Email       string `json:"email"`
	BizMail     string `json:"biz_mail"`
	Avatar      string `json:"avatar"`
}

func (w *wecom) Exchange(ctx context.Context, code, _ string, _ *PKCE) (*Identity, error) {
	var tok wecomResp
	if err := getJSON(ctx, w.hc, withQuery(w.api+"/cgi-bin/gettoken", url.Values{"corpid": {w.c.ClientID}, "corpsecret": {w.c.ClientSecret}}), nil, &tok); err != nil {
		return nil, err
	}
	if tok.ErrCode != 0 || tok.AccessToken == "" {
		return nil, fail("wecom gettoken errcode=%d", tok.ErrCode)
	}
	var who wecomResp
	if err := getJSON(ctx, w.hc, withQuery(w.api+"/cgi-bin/auth/getuserinfo", url.Values{"access_token": {tok.AccessToken}, "code": {code}}), nil, &who); err != nil {
		return nil, err
	}
	if who.ErrCode != 0 || who.UserID == "" { // 非企业成员（只有 openid）不允许登录
		return nil, fail("wecom getuserinfo errcode=%d", who.ErrCode)
	}
	id := &Identity{Provider: "wecom", ExternalID: who.UserID, Name: who.UserID, EmployeeNo: who.UserID}
	var d wecomResp // 通讯录详情需要应用有可见范围/权限，拿不到就只用 userid
	err := getJSON(ctx, w.hc, withQuery(w.api+"/cgi-bin/user/get", url.Values{"access_token": {tok.AccessToken}, "userid": {who.UserID}}), nil, &d)
	if err == nil && d.ErrCode == 0 {
		id.Name, id.Phone, id.Avatar = firstNonEmpty(d.Name, who.UserID), d.Mobile, d.Avatar
		if d.BizMail != "" { // 企业邮箱由管理员分配，视为已验证；成员自填的 email 不可信
			id.Email, id.EmailVerified = d.BizMail, true
		} else {
			id.Email = d.Email
		}
	}
	return id, nil
}
