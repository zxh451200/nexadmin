import { enUS, zhCN, zhTW, ja, ko, fr, de, es, ru, ar } from 'date-fns/locale'
import type { Locale } from 'date-fns'
import { format, isValid, parseISO } from 'date-fns'

const LOCALES: Record<string, Locale> = { 'zh-CN': zhCN, 'zh-TW': zhTW, zh: zhCN, ja, ko, fr, de, es, ru, ar, en: enUS }

export function dateLocale(lang: string): Locale {
  return LOCALES[lang] ?? LOCALES[lang.split('-')[0]] ?? enUS
}

export function toDate(v: string | Date | null | undefined): Date | undefined {
  if (!v) return undefined
  const d = v instanceof Date ? v : parseISO(v)
  return isValid(d) ? d : undefined
}

export const fmtDate = (d: Date) => format(d, 'yyyy-MM-dd')
export const fmtDateTime = (d: Date) => format(d, 'yyyy-MM-dd HH:mm')
