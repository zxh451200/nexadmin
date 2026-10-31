import DOMPurify from 'dompurify'

/** 允许嵌入的视频平台（与后端 pkg/htmlsafe 的 safeEmbedURL 保持一致）。 */
const EMBED_SRC = [
  /^https:\/\/www\.youtube\.com\/embed\//,
  /^https:\/\/www\.youtube-nocookie\.com\/embed\//,
  /^https:\/\/player\.bilibili\.com\/player\.html/,
  /^https:\/\/customer-[a-z0-9-]+\.cloudflarestream\.com\/[A-Za-z0-9_-]+\/iframe$/,
]

/** 把用户粘贴的 B 站/YouTube 链接规范成可嵌入地址；不认识的返回 null。 */
export function normalizeEmbedUrl(input: string): string | null {
  const raw = input.trim()
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return null
  }
  const host = u.hostname.toLowerCase()
  if (host === 'youtu.be') {
    const id = u.pathname.slice(1)
    return id ? `https://www.youtube.com/embed/${id}` : null
  }
  if (host === 'www.youtube.com' || host === 'youtube.com' || host === 'm.youtube.com') {
    const id = u.searchParams.get('v')
    if (id) return `https://www.youtube.com/embed/${id}`
    if (u.pathname.startsWith('/embed/')) return `https://www.youtube.com${u.pathname}`
    return null
  }
  if (host === 'www.bilibili.com' || host === 'm.bilibili.com') {
    const bv = /\/video\/(BV[0-9A-Za-z]+)/.exec(u.pathname)
    return bv ? `https://player.bilibili.com/player.html?bvid=${bv[1]}&page=1` : null
  }
  if (host.startsWith('customer-') && host.endsWith('.cloudflarestream.com')) {
    // Cloudflare Stream 的播放器地址原样使用（服务端同样是白名单校验）。
    return /\/[A-Za-z0-9_-]+\/iframe$/.test(u.pathname) ? `${u.origin}${u.pathname}` : null
  }
  if (host === 'player.bilibili.com' && u.pathname === '/player.html') {
    const bv = u.searchParams.get('bvid')
    return bv ? `https://player.bilibili.com/player.html?bvid=${bv}&page=1` : null
  }
  return null
}

/** 富文本 HTML 过滤：允许 video/source 与受控 iframe（仅视频平台），去掉脚本与事件属性。 */
export function sanitizeHtml(html: string): string {
  const clean = DOMPurify.sanitize(html ?? '', {
    ADD_TAGS: ['video', 'source', 'iframe'],
    ADD_ATTR: ['controls', 'poster', 'preload', 'playsinline', 'colspan', 'rowspan', 'colwidth', 'target', 'rel', 'data-color', 'allow', 'allowfullscreen', 'frameborder', 'referrerpolicy'],
    FORBID_TAGS: ['script', 'object', 'embed', 'form'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick'],
  })
  // DOMPurify 只做通用过滤；iframe 的 src 必须再过一遍平台白名单，与服务端规则一致。
  const tpl = document.createElement('template')
  tpl.innerHTML = clean
  tpl.content.querySelectorAll('iframe').forEach((f) => {
    const src = f.getAttribute('src') ?? ''
    if (!EMBED_SRC.some((re) => re.test(src))) f.remove()
  })
  // 新窗口打开的外链补 noopener/noreferrer：历史内容可能只写了 target。
  tpl.content.querySelectorAll('a[target="_blank"]').forEach((a) => {
    const rel = (a.getAttribute('rel') ?? '').split(/\s+/).filter(Boolean)
    for (const token of ['noopener', 'noreferrer']) {
      if (!rel.includes(token)) rel.push(token)
    }
    a.setAttribute('rel', rel.join(' '))
  })
  return tpl.innerHTML
}
