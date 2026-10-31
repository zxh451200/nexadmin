import { useEffect, useMemo, useRef, useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { OptionList, toArray, type Option, type PickerChange } from './pickers/OptionList'
import { useDebounce } from './pickers/useDebounce'

export interface RemoteSelectProps {
  url: string
  labelKey?: string
  valueKey?: string
  value?: string | string[] | null
  onChange?: PickerChange
  multiple?: boolean
  params?: Record<string, unknown>
  pageSize?: number
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  /** 调用方已持有的行数据（如编辑态的 name/nickname），优先用于回显，避免显示原始 id */
  initialOptions?: Option[]
  /**
   * 已选项在已加载结果与 initialOptions 里都未命中时，按 id 解析展示名的扩展点。
   * 同一批 id 只拉一次；组件卸载或值变化会通过 AbortSignal 取消并丢弃过期响应。
   * 失败（如用户已删除）静默回退显示原值。
   */
  resolveLabel?: (id: string, signal: AbortSignal) => Promise<string | null | undefined>
}

type Row = Record<string, unknown>

export function RemoteSelect({
  url,
  labelKey = 'name',
  valueKey = 'id',
  value,
  onChange,
  multiple,
  params,
  pageSize = 20,
  allowClear = true,
  placeholder,
  disabled,
  className,
  id,
  initialOptions,
  resolveLabel,
}: RemoteSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const keyword = useDebounce(search, 300)
  const labelCache = useRef(new Map<string, string>())
  const [resolvedLabels, setResolvedLabels] = useState<Record<string, string>>({})
  const selected = toArray(value)
  // 调用方已持有的回显数据（过滤掉空 label，避免整屏显示空白）；memo 化以稳定 effect 依赖
  const initialMap = useMemo(
    () => new Map((initialOptions ?? []).filter((o) => o.label).map((o) => [o.value, o.label])),
    [initialOptions],
  )

  const q = useInfiniteQuery({
    queryKey: ['remote-select', url, params, keyword, pageSize],
    // 关闭时也取第一页，用于回显已选项的名称
    enabled: open || selected.length > 0,
    initialPageParam: 1,
    queryFn: async ({ pageParam }) => {
      const res = await api.get<Page<Row> | Row[]>(url, { ...params, keyword: keyword || undefined, page: pageParam, pageSize })
      return Array.isArray(res) ? { list: res, total: res.length, page: 1, pageSize: res.length } : res
    },
    getNextPageParam: (last) => (last.page * last.pageSize < last.total ? last.page + 1 : undefined),
  })

  const options: Option[] = (q.data?.pages ?? []).flatMap((p) => p.list).map((r) => {
    const v = String(r[valueKey])
    const label = String(r[labelKey] ?? v)
    labelCache.current.set(v, label)
    return { value: v, label }
  })

  // 回显兜底：已选项在「已加载结果 / 调用方 initialOptions / 已解析缓存」里都未命中时，
  // 用 resolveLabel 按 id 拉一次名称。selectedKey 是稳定字符串（值数组每次渲染都是新引用，
  // join 后才能作依赖），值不变则同一批 id 只发一次请求；卸载/值变化时 abort 并丢弃过期响应。
  const selectedKey = selected.join('\u0000')
  useEffect(() => {
    if (!resolveLabel) return
    const missing = selectedKey
      .split('\u0000')
      .filter((v) => v && !labelCache.current.get(v) && !resolvedLabels[v] && !initialMap.get(v))
    if (!missing.length) return
    const ac = new AbortController()
    let stale = false
    void Promise.all(
      missing.map((v) =>
        resolveLabel(v, ac.signal)
          .then((label) => (label && label.trim() ? { value: v, label: String(label) } : null))
          .catch(() => null),
      ),
    ).then((entries) => {
      if (stale) return
      const found = entries.filter((e): e is { value: string; label: string } => e !== null)
      if (!found.length) return
      setResolvedLabels((prev) => {
        const next = { ...prev }
        for (const e of found) next[e.value] = e.label
        return next
      })
    })
    return () => {
      stale = true
      ac.abort()
    }
  }, [selectedKey, resolveLabel, resolvedLabels, initialMap])

  const labelOf = (v: string) => labelCache.current.get(v) ?? resolvedLabels[v] ?? initialMap.get(v) ?? v

  const pick = (v: string) => {
    if (multiple) {
      onChange?.(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v])
    } else {
      onChange?.(v)
      setOpen(false)
    }
  }

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('common.pleaseSelect')}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder}
          hasValue={selected.length > 0}
          allowClear={allowClear}
          onClear={() => onChange?.(multiple ? [] : '')}
          display={selected.map(labelOf).join(', ')}
        />
      }
    >
      <OptionList
        options={options}
        value={selected}
        multiple={multiple}
        onSelect={pick}
        search={search}
        onSearchChange={setSearch}
        loading={q.isFetching}
        footer={
          q.hasNextPage ? (
            <div className="border-t border-border p-1">
              <Button type="button" variant="ghost" size="sm" className="w-full" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>
                {q.isFetchingNextPage ? t('common.loading') : t('picker.loadMore')}
              </Button>
            </div>
          ) : undefined
        }
      />
    </PickerShell>
  )
}
