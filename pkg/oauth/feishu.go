package oauth

import (
	"context"
	"net/url"
)

// ---- 飞书（扫码 / 网页授权，v2 OAuth）----
// client_id = App ID，client_secret = App Secret。ExternalID 用 union_id。

type feishu struct {
	c    Config
	hc   httpDoer
	auth string
	api  string
}

func newFeishu(c Config, hc httpDoer) Provider {
	return &feishu{c: c, hc: hc, auth: "https://accounts.feishu.cn/open-apis/authen/v1/authorize", api: "https://open.feishu.cn"}
}

func (f *feishu) Name() string   { return "feishu" }
func (f *feishu) UsesPKCE() bool { return false }

func (f *feishu) AuthURL(state, redirect string, _ *PKCE) string {
	return withQuery(f.auth, url.Values{"client_id": {f.c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {state}})
}

func (f *feishu) Exchange(ctx context.Context, code, redirect string, _ *PKCE) (*Identity, error) {
	var tok struct {
		Code        int    `json:"code"`
		AccessToken string `json:"access_token"`
	}
	err := postJSON(ctx, f.hc, f.api+"/open-apis/authen/v2/oauth/token", map[string]string{
		"grant_type": "authorization_code", "client_id": f.c.ClientID, "client_secret": f.c.ClientSecret,
		"code": code, "redirect_uri": redirect,
	}, &tok)
	if err != nil {
		return nil, err
	}
	if tok.Code != 0 || tok.AccessToken == "" {
		return nil, fail("feishu token code=%d", tok.Code)
	}
	var me struct {
		Code int `json:"code"`
		Data struct {
			Name            string `json:"name"`
			UnionID         string `json:"union_id"`
			OpenID          string `json:"open_id"`
			AvatarURL       string `json:"avatar_url"`
			Email           string `json:"email"`
			EnterpriseEmail string `json:"enterprise_email"`
			Mobile          string `json:"mobile"`
			EmployeeNo      string `json:"employee_no"`
		} `json:"data"`
	}
	if err := getJSON(ctx, f.hc, f.api+"/open-apis/authen/v1/user_info", map[string]string{"Authorization": "Bearer " + tok.AccessToken}, &me); err != nil {
		return nil, err
	}
	d := me.Data
	ext := firstNonEmpty(d.UnionID, d.OpenID)
	if me.Code != 0 || ext == "" {
		return nil, fail("feishu user_info code=%d", me.Code)
	}
	id := &Identity{Provider: "feishu", ExternalID: ext, Name: d.Name, Phone: d.Mobile, Avatar: d.AvatarURL, EmployeeNo: d.EmployeeNo}
	if d.EnterpriseEmail != "" { // 企业邮箱由租户管理员分配，视为已验证；个人邮箱不可信
		id.Email, id.EmailVerified = d.EnterpriseEmail, true
	} else {
		id.Email = d.Email
	}
	return id, nil
}
