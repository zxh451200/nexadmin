package identity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LDAP 搜索范围。
const (
	scopeBase = 0
	scopeOne  = 1
	scopeSub  = 2
)

// searchTimeLimit 是发给服务端的搜索时限（秒）；连接本身另有总超时。
const searchTimeLimit = 30

// Bind 做简单绑定。口令为空一律拒绝：RFC 4513 的“未认证绑定”会让 DN + 空口令得到成功响应，
// 若不拦截，任何人输入空密码都会被当成通过。DN 为空同理（那是匿名绑定）。
func (c *ldapConn) Bind(dn, password string) error {
	if strings.TrimSpace(password) == "" { // 空串与全空白一律拒绝，永远不发出未认证 bind
		return errEmptyPassword
	}
	if dn == "" {
		return errEmptyDN
	}
	id := c.nextID()
	req := enc(tagBindReq, encInt(tagInt, 3), encStr(tagOctet, dn), encStr(tagSimpleAuth, password))
	if err := c.write(id, req); err != nil {
		return err
	}
	op, err := c.readOp(id, tagBindResp)
	if err != nil {
		return err
	}
	return resultError(op)
}

// Search 发起搜索并收集结果。sizeLimit 是发给服务端的条数上限（客户端另有 maxLDAPEntries 硬上限）。
// 结果被截断（sizeLimitExceeded 等任何非成功码）一律按错误处理：登录要求“恰好一条”，同步要求完整枚举。
func (c *ldapConn) Search(base string, scope int, filter string, attrs []string, sizeLimit int) ([]ldapEntry, error) {
	if sizeLimit <= 0 || sizeLimit > maxLDAPEntries {
		sizeLimit = maxLDAPEntries
	}
	out, _, err := c.searchOnce(base, scope, filter, attrs, sizeLimit, maxLDAPEntries, nil)
	return out, err
}

// RFC 2696 简单分页结果控制。criticality 取默认 false：不支持分页的服务端会忽略它，
// 此时一次返回的条数超过 ldapPageSize 即 fail-closed，不会静默截断。
const (
	pagedResultsOID   = "1.2.840.113556.1.4.319"
	ldapPageSize      = 100
	maxLDAPPages      = 100
	maxLDAPPagedBytes = 32 << 20
	tagControls       = 0xa0
)

func pagedControl(size int, cookie []byte) []byte {
	val := enc(tagSeq, encInt(tagInt, size), enc(tagOctet, cookie))
	return enc(tagControls, enc(tagSeq, encStr(tagOctet, pagedResultsOID), enc(tagOctet, val)))
}

// pagedCookie 从 SearchResultDone 报文的 controls 里取分页 cookie；没有该 control 返回 (nil, false)。
func pagedCookie(ctls []byte) ([]byte, bool) {
	t, list, _, err := parseTLV(ctls)
	if err != nil || t != tagControls {
		return nil, false
	}
	for len(list) > 0 {
		var ctl []byte
		if t, ctl, list, err = parseTLV(list); err != nil || t != tagSeq {
			return nil, false
		}
		ot, oid, rest, err := parseTLV(ctl)
		if err != nil || ot != tagOctet || string(oid) != pagedResultsOID {
			continue
		}
		vt, v, rest, err := parseTLV(rest)
		if err == nil && vt == tagBool { // 可选的 criticality
			vt, v, _, err = parseTLV(rest)
		}
		if err != nil || vt != tagOctet {
			return nil, false
		}
		st, sv, _, err := parseTLV(v)
		if err != nil || st != tagSeq {
			return nil, false
		}
		if _, _, sv, err = parseTLV(sv); err != nil { // size
			return nil, false
		}
		ct, cookie, _, err := parseTLV(sv)
		if err != nil || ct != tagOctet {
			return nil, false
		}
		return cookie, true
	}
	return nil, false
}

// SearchPaged 用 RFC 2696 分页枚举，累计条数超过 max 即报错（不返回部分结果）。
// 目录同步用它：普通 Search 的 50 条硬上限只适合“按登录名找唯一用户”。
func (c *ldapConn) SearchPaged(base string, scope int, filter string, attrs []string, max int) ([]ldapEntry, error) {
	if max <= 0 {
		return nil, errors.New("ldap: invalid result limit")
	}
	var (
		all    []ldapEntry
		cookie []byte
		bytes  int
	)
	for page := 0; ; page++ {
		if page >= maxLDAPPages {
			return nil, errors.New("ldap: too many result pages")
		}
		if page > 0 && c.timeout > 0 { // 每一页各自计时；整体上限由页数和条数约束
			if c.ctx != nil && c.ctx.Err() != nil {
				return nil, c.ctx.Err()
			}
			_ = c.nc.SetDeadline(time.Now().Add(c.timeout))
		}
		ents, next, err := c.searchOnce(base, scope, filter, attrs, 0, ldapPageSize, pagedControl(ldapPageSize, cookie))
		if err != nil {
			return nil, err
		}
		if bytes += c.rx; bytes > maxLDAPPagedBytes {
			return nil, errors.New("ldap: search response too large")
		}
		if len(all)+len(ents) > max {
			return nil, errors.New("ldap: too many search results")
		}
		all = append(all, ents...)
		if len(next) == 0 {
			return all, nil
		}
		cookie = next
	}
}

// searchOnce 发一次搜索请求。maxEntries 是本次响应允许的条数；ctl 是已编码的 controls（可为空）。
// 返回 SearchResultDone 里的分页 cookie（没有则为空）。第一个 referral 就 fail-closed。
func (c *ldapConn) searchOnce(base string, scope int, filter string, attrs []string, sizeLimit, maxEntries int, ctl []byte) ([]ldapEntry, []byte, error) {
	f, err := compileFilter(filter)
	if err != nil {
		return nil, nil, err
	}
	var as [][]byte
	for _, a := range attrs {
		as = append(as, encStr(tagOctet, a))
	}
	id := c.nextID()
	req := enc(tagSearchReq,
		encStr(tagOctet, base), encInt(tagEnum, scope), encInt(tagEnum, 0), // 不跟随别名
		encInt(tagInt, sizeLimit), encInt(tagInt, searchTimeLimit), encBool(false), f, enc(tagSeq, as...))
	if err := c.writeCtl(id, req, ctl); err != nil {
		return nil, nil, err
	}
	var out []ldapEntry
	c.rx = 0
	for msgs := 1; ; msgs++ {
		if msgs > 4*maxEntries {
			return nil, nil, errors.New("ldap: too many search response messages")
		}
		got, tag, val, mctl, err := c.readMsgCtl()
		if err != nil {
			return nil, nil, err
		}
		if c.rx > maxLDAPSearchBytes {
			return nil, nil, errors.New("ldap: search response too large")
		}
		if got != id {
			return nil, nil, fmt.Errorf("ldap: unexpected message id %d", got)
		}
		switch tag {
		case tagSearchEnt:
			if len(out) >= maxEntries {
				return nil, nil, errors.New("ldap: too many search results")
			}
			e, err := parseEntry(val)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, e)
		case tagSearchRef:
			return nil, nil, errors.New("ldap: referrals prevent complete enumeration")
		case tagSearchDone:
			code, diag, err := parseResult(val)
			if err != nil {
				return nil, nil, err
			}
			if code != ldapSuccess {
				return nil, nil, &ldapError{Code: code, Msg: diag}
			}
			cookie, _ := pagedCookie(mctl)
			return out, cookie, nil
		default:
			return nil, nil, fmt.Errorf("ldap: unexpected search response tag 0x%02x", tag)
		}
	}
}

func parseEntry(b []byte) (ldapEntry, error) {
	t, dn, rest, err := parseTLV(b)
	if err != nil || t != tagOctet {
		return ldapEntry{}, errBER
	}
	e := ldapEntry{DN: string(dn), Attrs: map[string][]string{}}
	t, list, _, err := parseTLV(rest)
	if err != nil || t != tagSeq {
		return ldapEntry{}, errBER
	}
	for len(list) > 0 {
		var attr []byte
		if t, attr, list, err = parseTLV(list); err != nil || t != tagSeq {
			return ldapEntry{}, errBER
		}
		nt, name, vals, err := parseTLV(attr)
		if err != nil || nt != tagOctet {
			return ldapEntry{}, errBER
		}
		st, set, _, err := parseTLV(vals)
		if err != nil || st != tagSet {
			return ldapEntry{}, errBER
		}
		key := strings.ToLower(string(name))
		for len(set) > 0 {
			var v []byte
			if t, v, set, err = parseTLV(set); err != nil || t != tagOctet {
				return ldapEntry{}, errBER
			}
			e.Attrs[key] = append(e.Attrs[key], string(v))
		}
	}
	return e, nil
}
