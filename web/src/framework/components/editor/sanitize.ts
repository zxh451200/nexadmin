import DOMPurify from 'dompurify'

/** 富文本 HTML 过滤：允许 video/source 与常用样式属性，去掉脚本与事件属性（服务端另有 bluemonday 二次过滤） */
export function sanitizeHtml(html: string): string {
  return DOMPurify.sanitize(html ?? '', {
    ADD_TAGS: ['video', 'source'],
    ADD_ATTR: ['controls', 'poster', 'preload', 'playsinline', 'colspan', 'rowspan', 'colwidth', 'target', 'rel', 'data-color'],
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick'],
  })
}
