import { Info } from 'lucide-react'
import { usePermission } from '@/framework/auth/usePermission'
import { useSettings, useThemes } from './api'

/**
 * 主题能力提示：有些主题模板里根本没有某些字段，运营填了也不会出现在官网上。
 * 后端 `/themes` 下发 caps；null = 第三方主题未声明，按「全部支持」处理，
 * 与 website.normalizeThemeCaps 的 nil 语义一致。
 *
 * @param themeId 传表单里正在编辑的主题（还没保存也能预览提示）；不传则取已保存的站点设置。
 *   设置与主题清单接口都要 website:setting 权限，内容页只有 feature/faq 权限时不去请求，
 *   免得为一条提示给无权限的人弹 403。
 */
export function useThemeCap(themeId?: string) {
  const { has } = usePermission()
  const allowed = has('website:setting')
  const { data: settings } = useSettings(allowed)
  const { data: themes } = useThemes(allowed)
  const activeId = themeId ?? settings?.theme ?? ''
  const selected = themes?.items.find((x) => x.id === (activeId || 'default'))
  const caps = selected?.caps ?? null
  return {
    themeLabel: selected?.label ?? '',
    capOk: (cap: string) => !Array.isArray(caps) || caps.includes(cap),
  }
}

/** 纯展示组件：文字由调用方拼好，便于在各内容页复用同一套样式。 */
export function ThemeCapNote({ text }: { text: string }) {
  return (
    <p className="flex items-start gap-1.5 rounded-md border border-dashed p-2.5 text-xs text-muted-foreground">
      <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      <span>{text}</span>
    </p>
  )
}
