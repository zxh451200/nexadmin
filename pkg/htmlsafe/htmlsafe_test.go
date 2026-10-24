package htmlsafe

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// assertSafe 用独立的浏览器式解析器重新解析输出：不得出现白名单外的元素、事件属性或危险协议。
func assertSafe(t *testing.T, in, out string) {
	t.Helper()
	low := strings.ToLower(out)
	for _, bad := range []string{"<script", "<svg", "<math", "<iframe", "<object", "<embed", "<form", "<style", "javascript:", "vbscript:", "data:text", "data:image/svg"} {
		if strings.Contains(low, bad) {
			t.Errorf("input %q -> %q contains %q", in, out, bad)
		}
	}
	doc, err := xhtml.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("reparse %q: %v", out, err)
	}
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "html", "head", "body":
			default:
				if !allowedTags[n.Data] {
					t.Errorf("input %q -> %q has disallowed element <%s>", in, out, n.Data)
				}
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(a.Key, "on") {
					t.Errorf("input %q -> %q has attribute %s", in, out, a.Key)
				}
				if a.Key == "href" && !safeURL(a.Val, "http", "https", "mailto") {
					t.Errorf("input %q -> %q has unsafe href %q", in, out, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if again := Sanitize(out); again != out {
		t.Errorf("not idempotent: %q -> %q -> %q", in, out, again)
	}
}

func TestXSSVectors(t *testing.T) {
	vectors := []string{
		`<script>alert(1)</script>`,
		`<SCRIPT SRC=//evil.example/x.js></SCRIPT>`,
		`<ScRiPt>alert(1)</sCrIpT>`,
		`<scr<script>ipt>alert(1)</scr</script>ipt>`,
		`<img src=x onerror=alert(1)>`,
		`<IMG SRC=x ONERROR="alert(1)">`,
		`<img src="x"onerror="alert(1)">`,
		`<img/src=x/onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<a href="JaVaScRiPt:alert(1)">x</a>`,
		"<a href=\"java\tscript:alert(1)\">x</a>",
		"<a href=\"java&#x09;script:alert(1)\">x</a>",
		`<a href="&#106;&#97;&#118;&#97;&#115;&#99;&#114;&#105;&#112;&#116;&#58;alert(1)">x</a>`,
		`<a href="&#x6A;avascript&colon;alert(1)">x</a>`,
		`<a href=" javascript:alert(1)">x</a>`,
		`<a href="vbscript:msgbox(1)">x</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">x</a>`,
		`<img src="data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9YWxlcnQoMSk+">`,
		`<img src="javascript:alert(1)">`,
		`<svg onload=alert(1)>`,
		`<svg><script>alert(1)</script></svg>`,
		`<svg><a xlink:href="javascript:alert(1)"><text>x</text></a></svg>`,
		`<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
		`<iframe src="javascript:alert(1)"></iframe>`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe>`,
		`<object data="javascript:alert(1)"></object>`,
		`<embed src="javascript:alert(1)">`,
		`<form action="javascript:alert(1)"><input type=submit></form>`,
		`<style>@import 'javascript:alert(1)';</style>`,
		`<p style="background:url(javascript:alert(1))">x</p>`,
		`<p style="color:red;background-color:expression(alert(1))">x</p>`,
		`<p style="color:\72 ed">x</p>`,
		`<div onmouseover="alert(1)">x</div>`,
		`<body onload=alert(1)>`,
		`<video src=x onerror=alert(1)></video>`,
		`<video><source src="javascript:alert(1)" onerror=alert(1)></video>`,
		`<video poster="javascript:alert(1)" src="x"></video>`,
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>">`,
		`<template><script>alert(1)</script></template>`,
		`<textarea></textarea><img src=x onerror=alert(1)>`,
		`<!--><img src=x onerror=alert(1)>-->`,
		`<![CDATA[<script>alert(1)</script>]]>`,
		`<p title="x" onclick="alert(1)" title="y">x</p>`,
		`<a href="x" target="_top">x</a>`,
		`<p><b><i>unclosed <u>tags`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`<a href="//evil.example"onfocus="alert(1)" autofocus>x</a>`,
		`<<script>script>alert(1)<</script>/script>`,
		`<plaintext><script>alert(1)</script>`,
	}
	if len(vectors) < 20 {
		t.Fatal("need at least 20 vectors")
	}
	for _, v := range vectors {
		assertSafe(t, v, Sanitize(v))
	}
}

func TestSanitizeKeepsAllowedContent(t *testing.T) {
	cases := []struct{ in, want string }{
		{``, ``},
		{`<p style="text-align:center;color:#f00;position:fixed">hi</p>`, `<p style="text-align: center; color: #f00">hi</p>`},
		{`<span style="background-color: rgb(1, 2, 3)">x</span>`, `<span style="background-color: rgb(1, 2, 3)">x</span>`},
		{`<a href="https://example.com/a?b=1&c=2" target="_blank">l</a>`, `<a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer">l</a>`},
		{`<a href="/docs/a.html#top" target="_self" rel="opener">l</a>`, `<a href="/docs/a.html#top" rel="noopener noreferrer">l</a>`},
		{`<a href="mailto:a@example.com">m</a>`, `<a href="mailto:a@example.com" rel="noopener noreferrer">m</a>`},
		{`<img src="/files/1.png" alt="a" width="100">`, `<img src="/files/1.png" alt="a" width="100">`},
		{`<img src="data:image/png;base64,iVBORw0KGgo=">`, `<img src="data:image/png;base64,iVBORw0KGgo=">`},
		{`<video src="https://cdn.example.com/v.mp4" controls></video>`, `<video src="https://cdn.example.com/v.mp4" controls></video>`},
		{`<table><tr><td colspan="2">a</td></tr></table>`, `<table><tr><td colspan="2">a</td></tr></table>`},
		{`<h1>t</h1><ul><li>a</li></ul><blockquote>q</blockquote><pre><code class="language-go">x &lt; y</code></pre>`,
			`<h1>t</h1><ul><li>a</li></ul><blockquote>q</blockquote><pre><code class="language-go">x &lt; y</code></pre>`},
		{`<p>a<b>b</p>`, `<p>a<b>b</b></p>`},
		{`</div>x`, `x`},
		{`<custom>keep me</custom>`, `keep me`},
		{`1 < 2 & 3 > 2`, `1 &lt; 2 &amp; 3 &gt; 2`},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q)\n got  %q\n want %q", c.in, got, c.want)
		}
	}
}

func TestDeepNestingIsCapped(t *testing.T) {
	in := strings.Repeat("<div>", 5000) + "x"
	out := Sanitize(in)
	if strings.Count(out, "<div>") > maxDepth {
		t.Fatalf("nesting not capped: %d", strings.Count(out, "<div>"))
	}
	if strings.Count(out, "<div>") != strings.Count(out, "</div>") {
		t.Fatal("unbalanced output")
	}
}
