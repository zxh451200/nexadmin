import { useRef, useState } from 'react'
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
}: RemoteSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const keyword = useDebounce(search, 300)
  const labelCache = useRef(new Map<string, string>())
  const selected = toArray(value)

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
          display={selected.map((v) => labelCache.current.get(v) ?? v).join(', ')}
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
