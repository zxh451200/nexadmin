import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  completionOf,
  initialState,
  localeFormReducer,
  marshalDoc,
  marshalFlags,
  publishableOf,
  valuesOf,
  type LocaleFormState,
} from './locale-form'
import type { Completion, EntitySchema, LocalizedFieldSpec, LocaleOption, TranslateBatch } from './types'

export interface LocaleFormProviderProps {
  schema: EntitySchema
  locales: LocaleOption[]
  defaultLocale: string
  /** 原文（默认语言）的字段初值，来自行的裸列 */
  base: Record<string, string>
  i18nJson?: string
  publishedJson?: string
  sourcePublished?: boolean
  /** 打开时定位到某语言（列表页点缺译徽标进来）；语言清单是异步的，到货后才生效 */
  initialLocale?: string
  /**
   * 批量机翻的传输函数：框架不认识任何插件路由，由消费方绑定自己的接口。
   * 缺省或 AI 未启用时，界面上不出现「AI 翻译空白」。
   */
  translate?: TranslateBatch
  children: ReactNode
}

export interface LocaleFormApi {
  schema: EntitySchema
  locales: LocaleOption[]
  defaultLocale: string
  /** 该内容是否有「按语言发布」，取自后端 schema，消费方无法自行声明 */
  allowPublish: boolean
  active: string
  setActive: (code: string) => void
  /** 当前是否在编辑原文；true 时表单与改造前完全一致 */
  isSource: boolean
  spec: (key: string) => LocalizedFieldSpec | undefined
  fieldLabel: (key: string) => string
  /** 当前语言的字段值 */
  field: (key: string) => string
  setField: (key: string, value: string) => void
  /** 原文值，永远来自 base，用于字段旁的对照 */
  source: (key: string) => string
  baseValues: () => Record<string, string>
  completion: (code?: string) => Completion
  publishable: (code?: string) => { ok: boolean; missing: string[] }
  publishOn: boolean
  setPublishOn: (on: boolean) => void
  publishedOf: (code: string) => boolean
  setPublishedOf: (code: string, on: boolean) => void
  fillBlankFromSource: () => void
  applyBatch: (values: Record<string, string>) => void
  /** provider 传入的机翻传输函数；未提供时界面不出现 AI 按钮 */
  translate?: TranslateBatch
  /** 该语言的译文是机翻、还没人工校对 */
  reviewOf: (code?: string) => boolean
  /** AI 批量写入后调用：把该语言记为「机翻未校对」 */
  markMachine: (code?: string) => void
  /** 人工确认译文：清掉该语言的机翻标记 */
  confirmReview: (code?: string) => void
  serialize: () => { i18nJson: string; publishedJson: string }
  dirty: boolean
}

const LocaleFormContext = createContext<LocaleFormApi | null>(null)

export function LocaleFormProvider({
  schema,
  locales,
  defaultLocale,
  base,
  i18nJson,
  publishedJson,
  sourcePublished = false,
  initialLocale,
  translate,
  children,
}: LocaleFormProviderProps) {
  const { t } = useTranslation()
  const [state, dispatch] = useReducer(
    localeFormReducer,
    undefined,
    (): LocaleFormState => initialState({ base, i18nJson, publishedJson, sourcePublished, defaultLocale, locales }),
  )

  // 语言清单异步到货，定位请求要等到对应语言真的存在再兑现；用户手动切过就作废。
  const [pending, setPending] = useState(initialLocale ?? null)
  useEffect(() => {
    if (!pending) return
    if (locales.some((l) => l.code === pending)) {
      dispatch({ type: 'activate', code: pending })
      setPending(null)
    }
  }, [locales, pending])

  const setActive = useCallback((code: string) => {
    setPending(null)
    dispatch({ type: 'activate', code })
  }, [])

  // 弹窗打开期间语言清单刷新（重启后回到页面）时，选中项不能悬空。
  const active = locales.some((l) => l.code === state.active) ? state.active : defaultLocale
  const isSource = active === defaultLocale

  const specs = useMemo(() => new Map(schema.translatable.map((f) => [f.key, f])), [schema])

  const value = useMemo<LocaleFormApi>(() => {
    const completion = (code = active) => completionOf(schema, state, code, defaultLocale)
    return {
      schema,
      locales,
      defaultLocale,
      allowPublish: schema.publishable,
      active,
      setActive,
      isSource,
      spec: (key) => specs.get(key),
      fieldLabel: (key) => {
        const f = specs.get(key)
        return f ? t(f.labelKey) : key
      },
      field: (key) => valuesOf(state, active, defaultLocale)[key] ?? '',
      setField: (key, v) => dispatch({ type: 'write', code: isSource ? null : active, key, value: v }),
      source: (key) => state.base[key] ?? '',
      baseValues: () => ({ ...state.base }),
      completion,
      publishable: (code = active) => publishableOf(schema, state, code, defaultLocale),
      publishOn: isSource ? state.sourcePublished : Boolean(state.published[active]),
      setPublishOn: (on) => dispatch({ type: 'publish', code: isSource ? null : active, on }),
      publishedOf: (code) => (code === defaultLocale ? state.sourcePublished : Boolean(state.published[code])),
      setPublishedOf: (code, on) => dispatch({ type: 'publish', code: code === defaultLocale ? null : code, on }),
      fillBlankFromSource: () => dispatch({ type: 'fillBlanks', code: isSource ? null : active, values: state.base }),
      applyBatch: (values) => dispatch({ type: 'fillBlanks', code: isSource ? null : active, values }),
      translate,
      reviewOf: (code = active) => state.review[code] === 'mt',
      markMachine: (code = active) => {
        if (!isSource) dispatch({ type: 'review', code, on: true })
      },
      confirmReview: (code = active) => {
        if (!isSource) dispatch({ type: 'review', code, on: false })
      },
      serialize: () => ({
        i18nJson: marshalDoc(state.doc, defaultLocale, state.review),
        publishedJson: marshalFlags(state.published, defaultLocale),
      }),
      dirty: state.dirty,
    }
  }, [active, defaultLocale, isSource, locales, schema, setActive, specs, state, t, translate])

  return <LocaleFormContext.Provider value={value}>{children}</LocaleFormContext.Provider>
}

export function useLocaleForm(): LocaleFormApi {
  const ctx = useContext(LocaleFormContext)
  if (!ctx) throw new Error('useLocaleForm 必须在 LocaleFormProvider 内使用')
  return ctx
}
