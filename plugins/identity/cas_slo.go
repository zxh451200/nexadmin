package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
	"nexadmin/pkg/idgen"
)

const (
	maxSLOXML      = 8 << 10
	maxSLOBody     = 32 << 10
	sloProtocolNS  = "urn:oasis:names:tc:SAML:2.0:protocol"
	sloAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
)

// sloKnownTTL 是“本系统登录时登记过的 ticket”的保留期，需不短于会话最长寿命（刷新令牌 TTL）。
const sloKnownTTL = 31 * 24 * time.Hour

func sloKnownKey(tid, source idgen.ID, index string) string {
	h := sha256.Sum256([]byte(tid.String() + "\x00" + source.String() + "\x00" + index))
	return cache.GlobalKey("identity", "cas", "slo-known", hex.EncodeToString(h[:]))
}

// markSLOKnown 在 ticket 校验成功、会话登记之后记下这个 ticket；SLO 只对登记过的 ticket 生效，
// 这样公开的 SLO 接口不能被用来往缓存里灌任意 index。
func (s *Service) markSLOKnown(ctx context.Context, tid, source idgen.ID, index string) error {
	if s.cache == nil {
		return errors.New("identity: cache unavailable")
	}
	return s.cache.Set(ctx, sloKnownKey(tid, source, index), "1", sloKnownTTL)
}

func (s *Service) sloKnown(ctx context.Context, tid, source idgen.ID, index string) (bool, error) {
	if s.cache == nil {
		return false, errors.New("identity: cache unavailable")
	}
	_, err := s.cache.Get(ctx, sloKnownKey(tid, source, index))
	if errors.Is(err, cache.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// A ticket is the SLO bearer secret. Require a long opaque component rather
// than accepting short predictable ticket counters. Entropy itself must be
// guaranteed by the configured CAS server.
func validSLOIndex(ticket string) bool {
	if !strings.HasPrefix(ticket, "ST-") || len(ticket) > maxTicketLen || !ticketRe.MatchString(ticket) {
		return false
	}
	for _, opaque := range strings.Split(ticket[3:], "-") {
		if len(opaque) < 32 {
			continue
		}
		seen := map[byte]bool{}
		for i := range opaque {
			seen[opaque[i]] = true
		}
		if len(seen) >= 8 {
			return true
		}
	}
	return false
}

// Parse a single bounded SAML LogoutRequest, without DTDs, nested fields,
// duplicate indices, namespace confusion or trailing documents. Issuer is
// syntax only; it never authenticates the sender.
func parseSLOIndex(body []byte) (string, error) {
	invalid := errors.New("cas: invalid logout request")
	if len(body) == 0 || len(body) > maxSLOXML {
		return "", invalid
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = true
	depth, roots, indices := 0, 0, 0
	field, index := "", ""
	var text strings.Builder
	seen := map[string]bool{}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", invalid
		}
		switch v := tok.(type) {
		case xml.Directive:
			return "", invalid
		case xml.ProcInst:
			if v.Target != "xml" || roots != 0 {
				return "", invalid
			}
		case xml.StartElement:
			depth++
			if depth == 1 {
				roots++
				if roots != 1 || v.Name != (xml.Name{Space: sloProtocolNS, Local: "LogoutRequest"}) {
					return "", invalid
				}
			} else if depth == 2 {
				field = v.Name.Local
				ns := sloAssertionNS
				if field == "SessionIndex" {
					ns = sloProtocolNS
					indices++
				} else if field != "Issuer" && field != "NameID" {
					return "", invalid
				}
				if v.Name.Space != ns || seen[field] {
					return "", invalid
				}
				seen[field] = true
				text.Reset()
			} else {
				return "", invalid
			}
		case xml.CharData:
			if depth == 2 {
				text.Write(v)
			} else if strings.TrimSpace(string(v)) != "" {
				return "", invalid
			}
		case xml.EndElement:
			if depth == 2 && field == "SessionIndex" {
				index = text.String()
			}
			depth--
		}
	}
	if depth != 0 || roots != 1 || indices != 1 || !validSLOIndex(index) {
		return "", invalid
	}
	return index, nil
}

func (h *casHandler) slo(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	tid, err := tenantOf(ctx)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	// Require explicit source: never infer a source from an unverified Issuer.
	source := c.Query("source")
	if source == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	sourceID, err := idgen.Parse(source)
	if err != nil || sourceID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	// Disabled sources must still be able to revoke sessions issued earlier.
	src, err := h.svc.st.get(ctx, tid, sourceID)
	if err != nil || src.Type != TypeCAS {
		c.Status(http.StatusBadRequest)
		return
	}
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/x-www-form-urlencoded" {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSLOBody)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil || len(form) != 1 || len(form["logoutRequest"]) != 1 {
		c.Status(http.StatusBadRequest)
		return
	}
	index, err := parseSLOIndex([]byte(form.Get("logoutRequest")))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if h.svc.auth == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	known, err := h.svc.sloKnown(ctx, tid, src.ID, index)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !known {
		// 不是本系统登记过的 ticket：什么都不写（也不暴露它是否存在），按幂等成功应答，CAS 不会重试。
		c.Status(http.StatusOK)
		return
	}
	if h.svc.auth.RevokeExternalSession(ctx, tid, casSessionScope(src.ID), index) != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusOK)
}
