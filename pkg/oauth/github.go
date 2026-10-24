package oauth

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// ---- GitHub（普通 OAuth2）----
// 邮箱只取 /user/emails 里“已验证的主邮箱”；没有则不带邮箱（不用 /user 里可被用户随意填写的 email 字段）。

type github struct {
	c     Config
	hc    httpDoer
	auth  string
	token string
	api   string
}

func newGithub(c Config, hc httpDoer) Provider {
	return &github{c: c, hc: hc, auth: "https://github.com/login/oauth/authorize",
		token: "https://github.com/login/oauth/access_token", api: "https://api.github.com"}
}

func (g *github) Name() string   { return "github" }
func (g *github) UsesPKCE() bool { return false }

func (g *github) AuthURL(state, redirect string, _ *PKCE) string {
	return withQuery(g.auth, url.Values{
		"client_id": {g.c.ClientID}, "redirect_uri": {redirect}, "scope": {"read:user user:email"},
		"state": {state}, "allow_signup": {"false"},
	})
}

func (g *github) Exchange(ctx context.Context, code, redirect string, _ *PKCE) (*Identity, error) {
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	err := postForm(ctx, g.hc, g.token, url.Values{
		"client_id": {g.c.ClientID}, "client_secret": {g.c.ClientSecret}, "code": {code}, "redirect_uri": {redirect},
	}, &tok)
	if err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fail("github token error=%q", tok.Error)
	}
	hdr := map[string]string{"Authorization": "Bearer " + tok.AccessToken, "X-GitHub-Api-Version": "2022-11-28"}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getJSON(ctx, g.hc, g.api+"/user", hdr, &u); err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, fail("github: no user id")
	}
	id := &Identity{Provider: "github", ExternalID: strconv.FormatInt(u.ID, 10), Name: firstNonEmpty(u.Name, u.Login), Avatar: u.AvatarURL}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, g.hc, g.api+"/user/emails", hdr, &emails); err != nil {
		return nil, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified && !strings.HasSuffix(e.Email, "@users.noreply.github.com") {
			id.Email, id.EmailVerified = e.Email, true
			break
		}
	}
	return id, nil
}
