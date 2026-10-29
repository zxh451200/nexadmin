import type { TFunction } from 'i18next'

const PREFIX = 'plugin_report.builtin.'

/**
 * 内置报表内容存的是 i18n 键（与菜单名同一套机制），用户自建内容存的是原文。
 * 只翻译我们自己的前缀，所以用户把报表起名叫任何中文/英文都不会被误翻；
 * 编辑框里仍显示库里存的原始值，保存时键会原样写回。
 */
export function builtinText(t: TFunction, v?: string | null): string {
  if (!v) return ''
  return v.startsWith(PREFIX) ? String(t(v)) : v
}
