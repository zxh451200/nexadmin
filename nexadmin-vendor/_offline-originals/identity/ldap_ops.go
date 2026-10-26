package identity

import (
	"errors"
	"fmt"
	"strings"
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
	if password == "" {
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
func (c *ldapConn) Search(base string, scope int, filter string, attrs []string, sizeLimit int) ([]ldapEntry, error) {
	f, err := compileFilter(filter)
	if err != nil {
		return nil, err
	}
	if sizeLimit <= 0 || sizeLimit > maxLDAPEntries {
		sizeLimit = maxLDAPEntries
	}
	var as [][]byte
	for _, a := range attrs {
		as = append(as, encStr(tagOctet, a))
	}
	id := c.nextID()
	req := enc(tagSearchReq,
		encStr(tagOctet, base), encInt(tagEnum, scope), encInt(tagEnum, 0), // 不跟随别名
		encInt(tagInt, sizeLimit), encInt(tagInt, searchTimeLimit), encBool(false), f, enc(tagSeq, as...))
	if err := c.write(id, req); err != nil {
		return nil, err
	}
	var out []ldapEntry
	for {
		got, tag, val, err := c.readMsg()
		if err != nil {
			return nil, err
		}
		if got != id {
			return nil, fmt.Errorf("ldap: unexpected message id %d", got)
		}
		switch tag {
		case tagSearchEnt:
			if len(out) >= maxLDAPEntries {
				return nil, errors.New("ldap: too many search results")
			}
			e, err := parseEntry(val)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		case tagSearchRef: // 引用不跟随
		case tagSearchDone:
			code, diag, err := parseResult(val)
			if err != nil {
				return nil, err
			}
			if code != ldapSuccess && code != ldapSizeLimitExceeded {
				return nil, &ldapError{Code: code, Msg: diag}
			}
			return out, nil
		default:
			return nil, fmt.Errorf("ldap: unexpected search response tag 0x%02x", tag)
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
