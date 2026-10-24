import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { api } from '@/framework/request/http'
import type { LanguageInfo } from '@/framework/request/types'
import { startResourceLoaders } from './dynamic'

type Dict = Record<string, unknown>

const LANG_KEY = 'nex-lang'
const DEFAULT_KEY = 'nex-default-lang'
const RTL = /^(ar|he|fa|ur)/

function isObject(v: unknown): v is Dict {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

function deepMerge(target: Dict, source: Dict): Dict {
  for (const [k, v] of Object.entries(source)) {
    target[k] = isObject(v) && isObject(target[k]) ? deepMerge(target[k] as Dict, v) : v
  }
  return target
}

/** 合并顺序：框架 → 插件 → 用户（src/locales、src/pages/**），后者同名 key 覆盖前者；与文件名字母序无关。 */
const localeRank = (p: string) => (p.startsWith('/src/framework/') ? 0 : p.startsWith('/src/plugins/') ? 1 : 2)

function loadResources() {
  const files = import.meta.glob('/src/**/locales/*.json', { eager: true, import: 'default' }) as Record<string, Dict>
  const resources: Record<string, { translation: Dict }> = {}
  const entries = Object.entries(files).sort(([a], [b]) => localeRank(a) - localeRank(b) || a.localeCompare(b))
  for (const [path, content] of entries) {
    const code = path.split('/').pop()!.replace(/\.json$/, '')
    resources[code] ??= { translation: {} }
    deepMerge(resources[code].translation, content)
  }
  return resources
}

function pickInitialLang(codes: string[], fallback: string) {
  const saved = localStorage.getItem(LANG_KEY)
  if (saved && codes.includes(saved)) return saved
  for (const nav of navigator.languages ?? [navigator.language]) {
    const hit = codes.find((c) => c.toLowerCase() === nav.toLowerCase()) ?? codes.find((c) => c.split('-')[0] === nav.split('-')[0])
    if (hit) return hit
  }
  return fallback
}

let languagesCache: LanguageInfo[] | null = null
let languagesPromise: Promise<LanguageInfo[]> | null = null

export function languageDir(code: string): 'ltr' | 'rtl' {
  const info = languagesCache?.find((l) => l.code === code)
  return info?.dir ?? (RTL.test(code) ? 'rtl' : 'ltr')
}

function applyDocument(code: string) {
  document.documentElement.lang = code
  document.documentElement.dir = languageDir(code)
  localStorage.setItem(LANG_KEY, code)
}

/** 拉取语言列表（带缓存）。失败时退化为已加载的语言包。 */
export function loadLanguages(): Promise<LanguageInfo[]> {
  if (languagesCache) return Promise.resolve(languagesCache)
  languagesPromise ??= api
    .get<LanguageInfo[]>('/i18n/languages', undefined, { silent: true })
    .catch((): LanguageInfo[] => Object.keys(i18n.options.resources ?? {}).map((code) => ({ code, name: code, dir: languageDir(code) })))
    .then((list) => {
      languagesCache = list
      const def = list.find((l) => l.default)?.code
      if (def) {
        localStorage.setItem(DEFAULT_KEY, def)
        // 回退链：当前语言 → 默认语言 → key
        i18n.options.fallbackLng = [def]
      }
      const codes = list.map((l) => l.code)
      if (!codes.includes(i18n.language) && (def ?? codes[0])) void i18n.changeLanguage(def ?? codes[0])
      // Rebuild the resolution hierarchy after the configured fallback changes.
      else void i18n.changeLanguage(i18n.language)
      return list
    })
  return languagesPromise
}

export function initI18n() {
  const resources = loadResources()
  const codes = Object.keys(resources)
  const savedDefault = localStorage.getItem(DEFAULT_KEY)
  const fallback = savedDefault && codes.includes(savedDefault) ? savedDefault : codes.includes('zh-CN') ? 'zh-CN' : codes[0]
  const lng = pickInitialLang(codes, fallback)

  i18n.on('languageChanged', applyDocument)
  void i18n.use(initReactI18next).init({
    resources,
    lng,
    fallbackLng: [fallback],
    interpolation: { escapeValue: false },
    react: { bindI18nStore: 'added removed' },
    initAsync: false,
  })
  applyDocument(lng)
  startResourceLoaders(i18n)
  void loadLanguages()
  return i18n
}

export default i18n
