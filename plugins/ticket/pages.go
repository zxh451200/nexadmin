package ticket

// 客户自助页：无登录的提交与查询页面，挂在根路径 /ticket/new、/ticket/view。
// 页面只做渲染与表单校验，数据仍走公开 JSON 接口（/api/v1/plugins/ticket/visitor/*），
// 由 API 组的租户中间件按域名解析租户；这里不引入前端构建产物，保持插件自包含。

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const pageCSS = `
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;background:#f6f7f9;color:#111827;font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
.wrap{max-width:640px;margin:0 auto;padding:40px 20px}
h1{font-size:22px;margin:0 0 6px}
h2{font-size:17px;margin:24px 0 6px}
.muted{color:#6b7280;font-size:13px}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px;margin-top:18px}
label{display:block;font-size:13px;font-weight:600;margin:14px 0 6px}
input,textarea,select{width:100%;padding:9px 11px;border:1px solid #d1d5db;border-radius:8px;font:inherit;background:transparent;color:inherit}
textarea{resize:vertical}
.row{display:grid;gap:12px;grid-template-columns:1fr 1fr}
@media (max-width:520px){.row{grid-template-columns:1fr}}
button{margin-top:16px;padding:10px 18px;border:0;border-radius:8px;background:#111827;color:#fff;font:inherit;font-weight:600;cursor:pointer}
button:disabled{opacity:.5;cursor:default}
.ok{color:#047857}
.err{color:#b91c1c}
.reply{border-top:1px solid #e5e7eb;padding:12px 0}
.reply .meta{font-size:12px;color:#6b7280;margin-bottom:4px}
.reply.agent{background:#f0f9ff;border-radius:8px;padding:10px;border-top:0;margin-top:8px}
.stars{display:flex;gap:6px;margin:10px 0}
.star{background:transparent;color:#9ca3af;font-size:24px;line-height:1;padding:4px 7px;margin:0;border:1px solid #d1d5db;border-radius:8px;cursor:pointer}
.star.on{color:#f59e0b;border-color:#f59e0b}
a{color:#1d4ed8}
.att-del{margin:0 0 0 8px;padding:2px 6px;font-size:12px;border:1px solid #d1d5db;border-radius:6px;background:transparent;color:#b91c1c;cursor:pointer}
@media (prefers-color-scheme:dark){.att-del{border-color:#334155;color:#f87171}}
@media (prefers-color-scheme:dark){
 body{background:#0b0f14;color:#e5e7eb}
 .card{background:#111820;border-color:#243040}
 .muted,.reply .meta{color:#94a3b8}
 input,textarea,select{border-color:#334155}
 button{background:#e5e7eb;color:#0b0f14}
 a{color:#93c5fd}
 .reply{border-color:#243040}
 .reply.agent{background:#0f1c2e}
 .ok{color:#34d399}.err{color:#f87171}
}
`

type pageStrings struct {
	Required     string            `json:"required"`
	Unset        string            `json:"unset"`
	Yes          string            `json:"yes"`
	No           string            `json:"no"`
	Submitting   string            `json:"submitting"`
	Failed       string            `json:"failed"`
	Success      string            `json:"success"`
	NotFound     string            `json:"notFound"`
	NoReplies    string            `json:"noReplies"`
	Reporter     string            `json:"reporter"`
	RateTitle    string            `json:"rateTitle"`
	RateHint     string            `json:"rateHint"`
	RateComment  string            `json:"rateComment"`
	RateSubmit   string            `json:"rateSubmit"`
	RateThanks   string            `json:"rateThanks"`
	RatedLabel   string            `json:"ratedLabel"`
	RateFailed   string            `json:"rateFailed"`
	AttTitle     string            `json:"attTitle"`
	AttAdd       string            `json:"attAdd"`
	AttUploading string            `json:"attUploading"`
	AttFailed    string            `json:"attFailed"`
	AttDel       string            `json:"attDel"`
	AttDelAsk    string            `json:"attDelAsk"`
	AttDelFailed string            `json:"attDelFailed"`
	Timeline     string            `json:"timeline"`
	TimelineNew  string            `json:"timelineNew"`
	TimelineStat string            `json:"timelineStat"`
	Status       map[string]string `json:"status"`
	Priority     map[string]string `json:"priority"`
}

var formTmpl = template.Must(template.New("form").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>` + pageCSS + `</style>
</head>
<body>
<main class="wrap">
  <h1>{{.Title}}</h1>
  <p class="muted">{{.Intro}}</p>
  <div class="card">
    <form id="f">
      <label for="title">{{.Subject}}</label>
      <input id="title" maxlength="200" required>
      <label for="content">{{.Content}}</label>
      <textarea id="content" rows="5" maxlength="2000" required></textarea>
      <div class="row">
        <div>
          <label for="name">{{.Name}}</label>
          <input id="name" maxlength="64">
        </div>
        <div>
          <label for="contact">{{.Contact}}</label>
          <input id="contact" maxlength="64">
        </div>
        <div>
          <label for="email">{{.Email}}</label>
          <input id="email" type="email" maxlength="128">
        </div>
      </div>
      <label for="priority">{{.Priority}}</label>
      <select id="priority">
        <option value="normal">{{.PNormal}}</option>
        <option value="high">{{.PHigh}}</option>
        <option value="urgent">{{.PUrgent}}</option>
      </select>
      <div id="cf"></div>
      <button type="submit" id="submit">{{.Submit}}</button>
      <p id="msg" role="status"></p>
    </form>
  </div>
  <p class="muted"><a href="/ticket/view">{{.QueryLink}}</a></p>
</main>
<script>
const API = '/api/v1/plugins/ticket/visitor';
const S = {{.Strings}};
// 自定义字段：由管理员定义、这里按类型渲染。必填只标星号不设 required——
// 必填是坐席要补的信息，不该把客户挡在提交之外（后端同口径，见 CreateInput.EnforceRequiredFields）。
const FIELDS = {{.Fields}};
const cfBox = document.getElementById('cf');
FIELDS.forEach((f) => {
  const id = 'cf-' + f.code;
  const lab = document.createElement('label');
  lab.setAttribute('for', id);
  lab.textContent = f.label + (f.required ? ' *' : '');
  cfBox.appendChild(lab);
  let el;
  if (f.type === 'select' || f.type === 'bool') {
    el = document.createElement('select');
    const blank = document.createElement('option');
    blank.value = '';
    blank.textContent = S.unset;
    el.appendChild(blank);
    const opts = f.type === 'bool'
      ? [{ v: 'true', t: S.yes }, { v: 'false', t: S.no }]
      : (f.options || []).map((o) => ({ v: o, t: o }));
    opts.forEach((o) => {
      const op = document.createElement('option');
      op.value = o.v;
      op.textContent = o.t;
      el.appendChild(op);
    });
  } else {
    el = document.createElement('input');
    el.type = f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text';
    if (f.type === 'text') el.maxLength = 500;
  }
  el.id = id;
  el.name = f.code;
  cfBox.appendChild(el);
});

// 收集已填的字段：空值不提交（后端把「没填」与「填了空串」当两件事）。
function collectFields() {
  const out = {};
  FIELDS.forEach((f) => {
    const el = document.getElementById('cf-' + f.code);
    if (!el) return;
    const v = el.value === '' ? '' : String(el.value).trim();
    if (v === '') return;
    out[f.code] = f.type === 'bool' ? v === 'true' : f.type === 'number' ? Number(v) : v;
  });
  return out;
}
const form = document.getElementById('f');
form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const msg = document.getElementById('msg');
  const btn = document.getElementById('submit');
  const body = {
    title: document.getElementById('title').value.trim(),
    content: document.getElementById('content').value.trim(),
    reporterName: document.getElementById('name').value.trim(),
    reporterId: document.getElementById('contact').value.trim(),
    reporterEmail: document.getElementById('email').value.trim(),
    priority: document.getElementById('priority').value,
    customValues: collectFields()
  };
  if (!body.title || !body.content) { msg.textContent = S.required; msg.className = 'err'; return; }
  btn.disabled = true; msg.textContent = S.submitting; msg.className = 'muted';
  try {
    const res = await fetch(API + '/tickets', {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)
    });
    const data = await res.json();
    if (!res.ok || data.code !== 0) throw new Error(data.message || S.failed);
    form.hidden = true;
    msg.className = 'ok';
    msg.textContent = S.success + ' ';
    const a = document.createElement('a');
    a.href = '/ticket/view?no=' + encodeURIComponent(data.data.ticketNo);
    a.textContent = data.data.ticketNo;
    msg.appendChild(a);
  } catch (err) {
    msg.textContent = (err && err.message) || S.failed;
    msg.className = 'err';
    btn.disabled = false;
  }
});
</script>
</body>
</html>`))

var viewTmpl = template.Must(template.New("view").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>` + pageCSS + `</style>
</head>
<body>
<main class="wrap">
  <h1>{{.Title}}</h1>
  <p class="muted">{{.Intro}}</p>
  <div class="card">
    <form id="q">
      <label for="no">{{.NoLabel}}</label>
      <input id="no" value="{{.Prefill}}" maxlength="40" required>
      <button type="submit">{{.Query}}</button>
    </form>
  </div>
  <section id="out" hidden>
    <h2 id="ttl"></h2>
    <p id="meta" class="muted"></p>
    <div id="timeline" class="card" hidden></div>
    <div id="replies" class="card"></div>
    <div id="atts" class="card" hidden></div>
    <div id="rate" class="card"></div>
  </section>
  <p id="msg" role="status"></p>
  <p class="muted"><a href="/ticket/new">{{.NewLink}}</a></p>
</main>
<script>
const API = '/api/v1/plugins/ticket/visitor';
const S = {{.Strings}};
const input = document.getElementById('no');
const out = document.getElementById('out');
const msg = document.getElementById('msg');
const params = new URLSearchParams(location.search);
if (!input.value && params.get('no')) input.value = params.get('no');
function el(tag, text, cls) { const n = document.createElement(tag); if (text != null) n.textContent = text; if (cls) n.className = cls; return n; }
function stars(score) { return '★★★★★'.slice(0, score) + '☆☆☆☆☆'.slice(0, 5 - score); }
function renderRating(tk, no) {
  const box = document.getElementById('rate');
  box.innerHTML = '';
  if (tk.status !== 'resolved' && tk.status !== 'closed') return;
  box.appendChild(el('h2', S.rateTitle));
  if (tk.rating) {
    box.appendChild(el('p', S.ratedLabel + ': ' + stars(tk.rating) + ' (' + tk.rating + '/5)'));
    if (tk.ratingComment) box.appendChild(el('p', tk.ratingComment, 'muted'));
    return;
  }
  box.appendChild(el('p', S.rateHint, 'muted'));
  let score = 0;
  const row = el('div', null, 'stars');
  const btns = [];
  for (let i = 1; i <= 5; i++) {
    const b = el('button', '☆', 'star');
    b.type = 'button';
    b.setAttribute('aria-label', i + '/5');
    b.onclick = () => {
      score = i;
      btns.forEach((x, idx) => { x.textContent = idx < i ? '★' : '☆'; x.classList.toggle('on', idx < i); });
    };
    btns.push(b);
    row.appendChild(b);
  }
  box.appendChild(row);
  const comment = document.createElement('textarea');
  comment.rows = 3; comment.maxLength = 500; comment.placeholder = S.rateComment;
  box.appendChild(comment);
  const submit = el('button', S.rateSubmit);
  submit.type = 'button';
  const msg = el('p', '');
  submit.onclick = async () => {
    if (!score) { msg.textContent = S.rateHint; msg.className = 'err'; return; }
    submit.disabled = true;
    try {
      const res = await fetch(API + '/tickets/' + encodeURIComponent(no) + '/rating', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ score: score, comment: comment.value.trim() })
      });
      const data = await res.json();
      if (!res.ok || data.code !== 0) throw new Error(data.message || S.rateFailed);
      renderRating(data.data, no);
      box.appendChild(el('p', S.rateThanks, 'ok'));
    } catch (err) {
      msg.textContent = (err && err.message) || S.rateFailed;
      msg.className = 'err';
      submit.disabled = false;
    }
  };
  box.appendChild(submit);
  box.appendChild(msg);
}
function renderAttachments(tk, no, atts) {
  const box = document.getElementById('atts');
  box.innerHTML = '';
  box.hidden = false;
  box.appendChild(el('h2', S.attTitle));
  if (!atts.length) {
    box.appendChild(el('p', S.noReplies, 'muted'));
  }
  atts.forEach((a) => {
    const p = el('p');
    const link = document.createElement('a');
    link.href = API + '/tickets/' + encodeURIComponent(no) + '/attachments/' + encodeURIComponent(a.token);
    link.textContent = a.name + ' (' + Math.max(1, Math.round(a.size / 1024)) + ' KB)';
    p.appendChild(link);
    // 只有客户自己上传的公开附件能删；服务端同样判定，这里只是不摆出按下去必然失败的按钮。
    if (a.uploaderType === 'reporter') {
      const del = el('button', S.attDel, 'att-del');
      del.type = 'button';
      del.onclick = async () => {
        if (!confirm(S.attDelAsk)) return;
        del.disabled = true;
        try {
          const res = await fetch(API + '/tickets/' + encodeURIComponent(no) + '/attachments/' + encodeURIComponent(a.token), { method: 'DELETE' });
          const data = await res.json();
          if (!res.ok || data.code !== 0) throw new Error(data.message || S.attDelFailed);
          load(no);
        } catch (err) {
          msg.textContent = (err && err.message) || S.attDelFailed;
          msg.className = 'err';
          del.disabled = false;
        }
      };
      p.appendChild(del);
    }
    box.appendChild(p);
  });
  if (tk.status === 'closed') return;
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = 'image/png,image/jpeg,image/gif,image/webp,application/pdf,text/plain,.zip,.docx,.xlsx,.pptx';
  const btn = el('button', S.attAdd);
  btn.type = 'button';
  const note = el('p', '');
  btn.onclick = async () => {
    const f = input.files && input.files[0];
    if (!f) return;
    btn.disabled = true;
    note.className = 'muted';
    note.textContent = S.attUploading;
    try {
      const fd = new FormData();
      fd.append('file', f);
      const res = await fetch(API + '/tickets/' + encodeURIComponent(no) + '/attachments', { method: 'POST', body: fd });
      const data = await res.json();
      if (!res.ok || data.code !== 0) throw new Error(data.message || S.attFailed);
      load(no);
    } catch (err) {
      note.textContent = (err && err.message) || S.attFailed;
      note.className = 'err';
      btn.disabled = false;
    }
  };
  box.appendChild(input);
  box.appendChild(btn);
  box.appendChild(note);
}
function renderTimeline(acts) {
  const box = document.getElementById('timeline');
  box.innerHTML = '';
  box.hidden = !acts.length;
  if (!acts.length) return;
  box.appendChild(el('h2', S.timeline));
  acts.forEach((a) => {
    const text = a.action === 'created' ? S.timelineNew : S.timelineStat + ' ' + (S.status[a.toValue] || a.toValue);
    box.appendChild(el('p', new Date(a.createdAt).toLocaleString() + ' · ' + text));
  });
}
async function load(no) {
  msg.textContent = ''; out.hidden = true;
  try {
    const res = await fetch(API + '/tickets/' + encodeURIComponent(no));
    const data = await res.json();
    if (!res.ok || data.code !== 0) throw new Error(data.message || S.notFound);
    const tk = data.data.ticket;
    document.getElementById('ttl').textContent = tk.title;
    const meta = document.getElementById('meta');
    meta.textContent = tk.ticketNo + ' · ' + (S.status[tk.status] || tk.status) + ' · ' + (S.priority[tk.priority] || tk.priority) + ' · ' + (tk.reporterName || S.reporter);
    const box = document.getElementById('replies');
    box.innerHTML = '';
    const rows = data.data.replies || [];
    if (!rows.length) { box.appendChild(el('p', S.noReplies, 'muted')); }
    rows.forEach((r) => {
      const item = el('div', null, 'reply' + (r.replierType === 'agent' ? ' agent' : ''));
      item.appendChild(el('div', (r.replierName || S.reporter) + ' · ' + new Date(r.createdAt).toLocaleString(), 'meta'));
      item.appendChild(el('div', r.content));
      box.appendChild(item);
    });
    renderRating(tk, no);
    renderAttachments(tk, no, data.data.attachments || []);
    renderTimeline(data.data.activities || []);
    out.hidden = false;
  } catch (err) {
    msg.textContent = (err && err.message) || S.notFound;
    msg.className = 'err';
  }
}
document.getElementById('q').addEventListener('submit', (e) => {
  e.preventDefault();
  const no = input.value.trim();
  if (!no) return;
  history.replaceState(null, '', '/ticket/view?no=' + encodeURIComponent(no));
  load(no);
});
if (input.value) load(input.value.trim());
</script>
</body>
</html>`))

type formPageData struct {
	Lang, Title, Intro, Subject, Content, Name, Contact, Email, Priority string
	PNormal, PHigh, PUrgent, Submit, QueryLink                           string
	Strings                                                              template.JS
	// Fields 是启用的自定义字段（code/label/type/required/options），由页面脚本按类型渲染。
	Fields template.JS
}

type viewPageData struct {
	Lang, Title, Intro, NoLabel, Prefill, Query, NewLink string
	Strings                                              template.JS
}

func pageIsEN(c *gin.Context) bool {
	switch strings.ToLower(c.Query("lang")) {
	case "en":
		return true
	case "zh", "zh-cn":
		return false
	}
	return strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en")
}

func pageDict(en bool) pageStrings {
	if en {
		return pageStrings{
			Required: "Subject and description are required.", Submitting: "Submitting…",
			Unset: "Not filled", Yes: "Yes", No: "No",
			Failed: "Submission failed, please try again.", Success: "Submitted. Your ticket number is",
			NotFound: "Ticket not found.", NoReplies: "No replies yet.", Reporter: "Reporter",
			RateTitle: "Rate your support", RateHint: "How did we do?",
			RateComment: "Anything to add? (optional)", RateSubmit: "Submit rating",
			RateThanks: "Thanks for your feedback.", RatedLabel: "Your rating", RateFailed: "Rating failed, please try again.",
			AttTitle: "Attachments", AttAdd: "Upload file", AttUploading: "Uploading…", AttFailed: "Upload failed, please try again.",
			Timeline: "Progress", TimelineNew: "Ticket submitted", TimelineStat: "Status changed to",
			AttDel: "Delete", AttDelAsk: "Delete this attachment? It cannot be recovered.", AttDelFailed: "Delete failed, please try again.",
			Status:   map[string]string{"open": "Open", "processing": "Processing", "pending": "Waiting on customer", "resolved": "Resolved", "closed": "Closed"},
			Priority: map[string]string{"low": "Low", "normal": "Normal", "high": "High", "urgent": "Urgent"},
		}
	}
	return pageStrings{
		Required: "请填写标题和描述。", Submitting: "提交中…",
		Unset: "未填", Yes: "是", No: "否",
		Failed: "提交失败，请稍后重试。", Success: "已提交，工单号",
		NotFound: "未找到工单。", NoReplies: "暂无回复。", Reporter: "报告人",
		RateTitle: "服务评价", RateHint: "问题解决得怎么样？",
		RateComment: "补充说明（可选）", RateSubmit: "提交评价",
		RateThanks: "感谢你的评价。", RatedLabel: "你的评价", RateFailed: "评价提交失败，请稍后重试。",
		AttTitle: "附件", AttAdd: "上传附件", AttUploading: "上传中…", AttFailed: "上传失败，请稍后重试。",
		Timeline: "处理进度", TimelineNew: "已提交工单", TimelineStat: "状态变更为",
		AttDel: "删除", AttDelAsk: "确定删除这个附件？删除后无法恢复。", AttDelFailed: "删除失败，请稍后重试。",
		Status:   map[string]string{"open": "待处理", "processing": "处理中", "pending": "待客户回复", "resolved": "已解决", "closed": "已关闭"},
		Priority: map[string]string{"low": "低", "normal": "普通", "high": "高", "urgent": "紧急"},
	}
}

func stringsJSON(s pageStrings) template.JS {
	b, err := json.Marshal(s)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(b)
}

// formPage GET /ticket/new —— 客户提交工单。
func (h *handler) formPage(c *gin.Context) {
	en := pageIsEN(c)
	// 自定义字段读失败不该让客户页打不开：退化成「没有附加字段」，日志里留痕。
	fields := make([]map[string]any, 0, 4)
	if defs, err := h.svc.EnabledFields(c.Request.Context()); err != nil {
		h.svc.logWarn("ticket: load custom fields for form failed", "err", err.Error())
	} else {
		for _, f := range defs {
			fields = append(fields, map[string]any{
				"code": f.Code, "label": f.Label, "type": f.Type,
				"required": f.Required, "options": f.OptionList,
			})
		}
	}
	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		fieldsJSON = []byte("[]")
	}
	d := formPageData{
		Lang: "zh-CN", Title: "提交工单", Intro: "描述你遇到的问题，我们会尽快处理；无需登录。",
		Subject: "标题", Content: "问题描述", Name: "称呼", Contact: "联系方式（可选）", Email: "邮箱（选填，用于接收处理进展通知）", Priority: "优先级",
		PNormal: "普通", PHigh: "高", PUrgent: "紧急", Submit: "提交", QueryLink: "查询已有工单",
		Strings: stringsJSON(pageDict(en)),
		Fields:  template.JS(fieldsJSON),
	}
	if en {
		d.Lang = "en"
		d.Title, d.Intro = "Submit a ticket", "Describe the issue and we will get back to you. No sign-in required."
		d.Subject, d.Content, d.Name, d.Contact, d.Email, d.Priority = "Subject", "Description", "Your name", "Contact (optional)", "Email (optional, for progress updates)", "Priority"
		d.PNormal, d.PHigh, d.PUrgent, d.Submit, d.QueryLink = "Normal", "High", "Urgent", "Submit", "Track an existing ticket"
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", render(formTmpl, d))
}

// viewPage GET /ticket/view?no= —— 客户查询工单进度（只含公开回复）。
func (h *handler) viewPage(c *gin.Context) {
	en := pageIsEN(c)
	d := viewPageData{
		Lang: "zh-CN", Title: "查询工单", Intro: "输入工单号查看处理进度与回复。",
		NoLabel: "工单号", Prefill: strings.TrimSpace(c.Query("no")), Query: "查询", NewLink: "提交新工单",
		Strings: stringsJSON(pageDict(en)),
	}
	if en {
		d.Lang = "en"
		d.Title, d.Intro = "Track a ticket", "Enter the ticket number to see status and replies."
		d.NoLabel, d.Query, d.NewLink = "Ticket number", "Track", "Submit a new ticket"
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", render(viewTmpl, d))
}

func render(t *template.Template, data any) []byte {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return []byte("template error")
	}
	return buf.Bytes()
}
