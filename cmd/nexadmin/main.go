// Command nexadmin 是 NexAdmin 的命令行工具（plugin / upgrade / new / add / i18n / release 子命令）。
//
//	nexadmin plugin list
//	nexadmin plugin install [--force] <目录或zip>
//	nexadmin plugin remove <id>
//	nexadmin plugin keygen [--out .dev-keys/private.pem]
//	nexadmin plugin sign --key <私钥> --plugin <id> --customer <名称> --expires <2027-01-01> [--domains a.com,b.com] [--serial <序列号>] [--out 文件]
//	nexadmin plugin revoke --key <私钥> --serial <序列号> --reason <原因> [--plugin <id>] [--list 现有清单] [--out config/license-revoked.json]
//	nexadmin upgrade --from <源码包目录或zip> [--to <版本>] [--dry-run] [--manifest .nexadmin.manifest.json] [--allow-dirty]
//	nexadmin new <名称> [--out <目录>] [--module <go模块路径>] [--with-plugins] [--git]
//	nexadmin add module <名称> [--fields "title:string,status:dict(sys_status)"] [--sortable]
//	nexadmin i18n add <locale> [--name <显示名>] [--dir ltr|rtl]
//	nexadmin i18n check [--all]
//	nexadmin release check [--strict]
//	nexadmin release pack [--out <路径.zip>] [--force]
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nexadmin/core/plugin"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

const usage = `用法:
  nexadmin plugin list
  nexadmin plugin install [--force] <目录或zip>
  nexadmin plugin remove [--purge] <id>
  nexadmin plugin keygen [--out .dev-keys/private.pem]
  nexadmin plugin sign --key <私钥> --plugin <id> --customer <名称> --expires <日期> [--domains a.com,b.com] [--serial <序列号,默认随机>] [--out 文件]
  nexadmin plugin revoke --key <私钥> --serial <序列号> --reason <原因> [--plugin <id>] [--list <现有清单>] [--out <清单文件,默认 license-revoked.json>]
  nexadmin plugin market sync [--docs <目录>]
                      导出市场目录 JSON（省略 --docs 打印到标准输出；--docs 写入 <目录>/data/plugins.json）
  nexadmin plugin pack [--out <路径.zip>] <id>
                      把已安装插件打成发行包（plugin.yaml + backend/ + web/ 的 zip，与 install 输入格式互逆）
  nexadmin plugin verify --sig <签名文件> --key <公钥文件> <zip>
                      校验官网市场下载包的 ed25519 签名
  nexadmin plugin lint <插件目录（含 plugin.yaml）>
                      上架前自检：清单校验、迁移三方言齐全、菜单权限码前缀
  nexadmin upgrade --from <源码包目录或zip> [--to <版本>] [--dry-run] [--manifest .nexadmin.manifest.json] [--allow-dirty]
  nexadmin new <名称> [--out <目录,默认 ../名称>] [--module <go模块路径,默认 名称>] [--with-plugins] [--git]
  nexadmin add module <名称> [--fields "title:string,status:dict(sys_status),sort:int,cover:image,published_at:datetime,owner_id:ref(user)"] [--sortable]
  nexadmin i18n add <locale> [--name <显示名>] [--dir ltr|rtl] [--translate]
  nexadmin i18n check [--all]
  nexadmin release check [--strict]
  nexadmin release pack [--out <路径.zip>] [--force]`

func run(args []string, out io.Writer) error {
	if len(args) >= 1 && args[0] == "release" {
		return cmdRelease(args[1:], out)
	}
	if len(args) >= 1 && args[0] == "new" {
		return cmdNew(args[1:], out)
	}
	if len(args) >= 1 && args[0] == "upgrade" {
		return cmdUpgrade(args[1:], out)
	}
	if len(args) >= 1 && args[0] == "add" {
		return cmdAdd(args[1:], out)
	}
	if len(args) >= 1 && args[0] == "i18n" {
		return cmdI18n(args[1:], out)
	}
	if len(args) < 2 || args[0] != "plugin" {
		return errors.New(usage)
	}
	cmd, rest := args[1], args[2:]
	switch cmd {
	case "list":
		return cmdList(out)
	case "install":
		return cmdInstall(rest, out)
	case "remove":
		return cmdRemove(rest, out)
	case "keygen":
		return cmdKeygen(rest, out)
	case "sign":
		return cmdSign(rest, out)
	case "revoke":
		return cmdRevoke(rest, out)
	case "market":
		return cmdMarket(rest, out)
	case "pack":
		return cmdPack(rest, out)
	case "verify":
		return cmdVerify(rest, out)
	case "lint":
		return cmdLint(rest, out)
	}
	return errors.New(usage)
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func cmdList(out io.Writer) error {
	root, err := findRoot()
	if err != nil {
		return err
	}
	disabled := map[string]bool{}
	for _, id := range plugin.LoadDisabledPlugins(root) {
		disabled[id] = true
	}
	ents, err := os.ReadDir(filepath.Join(root, "plugins"))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%-20s %-10s %-8s %-12s %s\n", "ID", "VERSION", "EDITION", "LICENSE", "NAME")
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		man, err := plugin.LoadManifest(filepath.Join(root, "plugins", e.Name()))
		if err != nil {
			fmt.Fprintf(out, "%-20s (清单无效: %v)\n", e.Name(), err)
			continue
		}
		lic := "-"
		if disabled[man.ID] {
			lic = "disabled"
		} else if man.Edition == plugin.Pro {
			lic = "active"
			if _, err := plugin.CheckFile(root, man.ID, time.Now()); err != nil {
				var le *plugin.LicenseError
				if errors.As(err, &le) {
					lic = le.Status
				} else {
					lic = plugin.StatusInvalid
				}
			}
		}
		fmt.Fprintf(out, "%-20s %-10s %-8s %-12s %s\n", man.ID, man.Version, man.Edition, lic, man.Name)
		n++
	}
	if n == 0 {
		fmt.Fprintln(out, "(没有已安装的插件)")
	}
	return nil
}

func cmdKeygen(args []string, out io.Writer) error {
	fs := newFlags("keygen")
	path := fs.String("out", filepath.Join(".dev-keys", "private.pem"), "私钥输出路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // 不覆盖已有私钥
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, "私钥已写入 %s（请妥善保管，不要提交到仓库）\nkid: %s\n公钥（追加到 config/license-keys.pub 一行，或加入 core/plugin/pubkey.go 的 builtinPublicKeysB64）:\n%s\n",
		*path, plugin.KeyID(pub), base64.StdEncoding.EncodeToString(pub))
	return nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return nil, errors.New("私钥文件不是 PEM 格式")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("不是 ed25519 私钥")
	}
	return priv, nil
}

// parseExpires 接受 RFC3339，或 YYYY-MM-DD（当天结束，UTC）。
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--expires 格式应为 2027-01-01 或 RFC3339: %q", s)
	}
	return t.Add(24*time.Hour - time.Second).UTC(), nil
}

func cmdSign(args []string, out io.Writer) error {
	fs := newFlags("sign")
	key := fs.String("key", "", "私钥文件")
	id := fs.String("plugin", "", "插件 id")
	customer := fs.String("customer", "", "客户名称")
	expires := fs.String("expires", "", "到期时间")
	domains := fs.String("domains", "", "授权域名，逗号分隔（可选）")
	serial := fs.String("serial", "", "授权序列号（吊销清单靠它命中）；默认随机生成")
	dst := fs.String("out", "", "输出文件（默认打印到标准输出）")
	ledger := fs.String("ledger", "", "签发台账 issued.json（追加一条记录，保持与 licensor 后台对账一致；默认自动探测 nexadmin-vendor/licensor/data/issued.json）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" || *id == "" || *customer == "" || *expires == "" {
		return errors.New("sign 需要 --key --plugin --customer --expires")
	}
	if !plugin.ValidID(*id) {
		return fmt.Errorf("非法的插件 id %q", *id)
	}
	exp, err := parseExpires(*expires)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !exp.After(now) {
		return errors.New("--expires 必须晚于当前时间")
	}
	priv, err := readPrivateKey(*key)
	if err != nil {
		return err
	}
	if *serial == "" {
		*serial = plugin.NewSerial()
	} else if !plugin.ValidSerial(*serial) {
		return fmt.Errorf("非法的 --serial %q（小写字母/数字/连字符，6~64 位）", *serial)
	}
	l := plugin.License{Plugin: *id, Customer: *customer, IssuedAt: now.Format(time.RFC3339), ExpiresAt: exp.Format(time.RFC3339), Serial: *serial}
	for _, d := range strings.Split(*domains, ",") {
		if d = strings.TrimSpace(d); d != "" {
			l.Domains = append(l.Domains, d)
		}
	}
	sort.Strings(l.Domains)
	raw, err := plugin.Sign(priv, l)
	if err != nil {
		return err
	}
	if *dst == "" {
		_, err = fmt.Fprintln(out, string(raw))
	} else {
		err = os.WriteFile(*dst, append(raw, '\n'), 0o600)
		if err == nil {
			fmt.Fprintf(out, "授权文件已写入 %s（序列号 %s，请记录；吊销时需要）\n", *dst, *serial)
		}
	}
	if err != nil {
		return err
	}
	// 台账：与 licensor 后台共用 issued.json，保证续费提醒与分成对账不分叉。
	if *ledger == "" {
		if root, rerr := findRoot(); rerr == nil {
			p := filepath.Join(root, "nexadmin-vendor", "licensor", "data", "issued.json")
			if _, serr := os.Stat(p); serr == nil {
				*ledger = p
			}
		}
	}
	if *ledger != "" {
		if lerr := appendLedger(*ledger, l, plugin.KeyID(priv.Public().(ed25519.PublicKey))); lerr != nil {
			fmt.Fprintf(out, "警告：写签发台账失败（%v）；请手工把本次签发补进 licensor 的 issued.json\n", lerr)
		} else {
			fmt.Fprintf(out, "台账已追加 %s\n", *ledger)
		}
	}
	return nil
}

// ledgerRecord 与 nexadmin-vendor/licensor 的 store.Record JSON 格式一致（跨模块不能直接引用，复制字段）。
type ledgerRecord struct {
	ID        string   `json:"id"`
	IssuedAt  string   `json:"issued_at"`
	Plugin    string   `json:"plugin"`
	Customer  string   `json:"customer"`
	ExpiresAt string   `json:"expires_at"`
	Domains   []string `json:"domains,omitempty"`
	KID       string   `json:"kid"`
	Note      string   `json:"note,omitempty"`
	Serial    string   `json:"serial,omitempty"`
}

// appendLedger 向 issued.json（JSON 数组）原子追加一条记录；文件不存在则创建。
func appendLedger(path string, l plugin.License, kid string) error {
	var recs []ledgerRecord
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &recs); err != nil {
			return fmt.Errorf("台账格式不是 JSON 数组: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	recs = append(recs, ledgerRecord{
		ID: l.Serial, IssuedAt: l.IssuedAt, Plugin: l.Plugin, Customer: l.Customer,
		ExpiresAt: l.ExpiresAt, Domains: l.Domains, KID: kid, Serial: l.Serial,
	})
	nw, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".issued-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(append(nw, '\n')); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// cmdRevoke 在现有吊销清单上追加一条并重新签发整份清单。
// 清单默认写到 license-revoked.json；发给客户时放到其项目的 config/license-revoked.json（随升级包分发）。
// 现有清单（--list，缺省时若输出文件已存在则用它）必须能用 --key 对应的公钥验签，否则拒绝追加。
func cmdRevoke(args []string, out io.Writer) error {
	fs := newFlags("revoke")
	key := fs.String("key", "", "私钥文件")
	serial := fs.String("serial", "", "要吊销的授权序列号")
	reason := fs.String("reason", "", "吊销原因（会显示在客户的插件管理页）")
	id := fs.String("plugin", "", "插件 id（可选，仅供人读）")
	list := fs.String("list", "", "现有吊销清单（可选；缺省时若输出文件已存在则在其上追加）")
	dst := fs.String("out", "", "输出清单文件（默认 --list，否则 license-revoked.json）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" || *serial == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("revoke 需要 --key --serial --reason")
	}
	if *dst == "" {
		*dst = *list
	}
	if *dst == "" {
		*dst = "license-revoked.json"
	}
	if *list == "" {
		*list = *dst
	}
	priv, err := readPrivateKey(*key)
	if err != nil {
		return err
	}
	prev, err := os.ReadFile(*list)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := plugin.AddRevocation(priv, prev,
		plugin.RevokedEntry{Serial: *serial, Plugin: *id, Reason: strings.TrimSpace(*reason)}, time.Now())
	if err != nil {
		return err
	}
	if err := os.WriteFile(*dst, append(raw, '\n'), 0o644); err != nil { // 清单是公开内容（只含序列号与原因），无需 0600
		return err
	}
	fmt.Fprintf(out, "吊销清单已写入 %s（已追加序列号 %s）；把它放到客户项目的 config/license-revoked.json 才会生效\n", *dst, *serial)
	return nil
}
