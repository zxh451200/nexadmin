import { useEffect, useMemo, useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { ChevronDown, Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'
import { EmptyState } from '@/framework/components/EmptyState'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDebounce } from '@/framework/components/pickers/useDebounce'
import { FileThumb } from '@/framework/modules/file/FileThumb'
import type { FileItem } from '@/framework/modules/file/api'
import { formatSize } from '@/framework/upload/precheck'
import type { UploadedFile, UploadKind } from '@/framework/upload/types'
import { cn } from '@/lib/utils'

const PAGE_SIZE = 18

/** kind → /files 的 category 过滤。附件（file）不筛类别：上传本来就不限类型（图片/压缩包/音视频都能挂），
 *  选择器如果只看 doc,other，库里只有图片时就是永久空列表。 */
const CATEGORY_BY_KIND: Record<UploadKind, string | undefined> = {
  image: 'image',
  video: 'video',
  file: undefined,
}

/** 库文件 → 上传组件通用值（公告附件、表单 file 等直接吃 UploadedFile 的入口用） */
export function toUploadedFile(f: FileItem): UploadedFile {
  return { id: f.id, url: f.url, name: f.name, size: f.size, type: f.type, posterUrl: f.posterUrl }
}

/** 「从文件库选择」触发按钮：RemoteSelect 触发器同款外观（同宽同高 + 尾部箭头），各入口共用 */
export function PickFromLibraryButton({
  onClick,
  disabled,
  className,
}: {
  onClick: () => void
  disabled?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <Button
      type="button"
      variant="outline"
      className={cn('h-11 w-36 justify-between text-base font-normal md:h-8 md:text-sm sm:w-44', className)}
      disabled={disabled}
      onClick={onClick}
    >
      {t('uploader.pickFromLibrary')}
      <ChevronDown className="size-4 shrink-0 opacity-50" aria-hidden="true" />
    </Button>
  )
}

export interface FilePickerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  kind: UploadKind
  multiple?: boolean
  /** 弹窗打开瞬间的已选 url（作为初始勾选；打开期间调用方的变化不回灌） */
  value?: string[]
  /**
   * 点确定回调。urls = 最终勾选的 url（沿用 value 原顺序、新勾按点击顺序追加）；
   * items = 其中能在已加载列表里命中的 FileItem（即本次新勾的文件，弹窗外的回显值调用方自己持有）。
   */
  onConfirm: (urls: string[], items: FileItem[]) => void
  disabled?: boolean
}

/** 文件库选择弹窗：搜索 + 缩略图网格勾选（对标 WordPress/Shopify 的媒体库选择器）。
 *  只管浏览勾选——上传和地址导入在各入口行的 Uploader / URL 字段里已有。 */
export function FilePickerDialog({ open, onOpenChange, kind, multiple, value = [], onConfirm, disabled }: FilePickerDialogProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const keyword = useDebounce(search, 300)
  const [selected, setSelected] = useState<string[]>([])

  // 每次打开以调用方当前值为初始勾选（打开瞬间快照，value 引用频繁变化不回灌）
  useEffect(() => {
    if (open) {
      setSelected(value)
      setSearch('')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 快照语义：只在打开瞬间取一次 value
  }, [open])

  const q = useInfiniteQuery({
    queryKey: ['file-picker', kind, keyword],
    enabled: open,
    initialPageParam: 1,
    queryFn: async ({ pageParam }) => {
      const res = await api.get<Page<FileItem>>('/files', {
        category: CATEGORY_BY_KIND[kind],
        keyword: keyword || undefined,
        page: pageParam,
        pageSize: PAGE_SIZE,
      })
      return res
    },
    getNextPageParam: (last) => (last.page * last.pageSize < last.total ? last.page + 1 : undefined),
  })

  const items = useMemo(() => (q.data?.pages ?? []).flatMap((p) => p.list), [q.data])
  // url → FileItem，用于把勾选映射回文件行（弹窗外的回显值可能不在已加载页里，映射不到就算了）
  const byUrl = useMemo(() => new Map(items.map((f) => [f.url, f])), [items])

  const toggle = (url: string) => {
    setSelected((s) => (s.includes(url) ? s.filter((x) => x !== url) : multiple ? [...s, url] : [url]))
  }
  const confirm = () => {
    onConfirm(selected, selected.flatMap((u) => byUrl.get(u) ?? []))
    onOpenChange(false)
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      fixedHeight
      title={t('uploader.pickFromLibrary')}
      footer={
        <>
          <span className="me-auto text-sm text-muted-foreground">{t('common.selected', { count: selected.length })}</span>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={disabled || !selected.length} onClick={confirm}>
            {t('common.confirm')}
          </Button>
        </>
      }
    >
      <div className="flex h-full min-h-0 flex-col gap-3">
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('common.search')}
          className="shrink-0"
          aria-label={t('common.search')}
        />
        <div className="min-h-0 flex-1 overflow-y-auto p-1">
          {q.isLoading ? (
            <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-5">
              {Array.from({ length: PAGE_SIZE }, (_, i) => (
                <Skeleton key={i} className="aspect-4/3 w-full rounded-lg" />
              ))}
            </ul>
          ) : items.length === 0 ? (
            <EmptyState title={t('fileMgr.empty')} />
          ) : (
            <>
              <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-5">
                {items.map((f) => {
                  const checked = selected.includes(f.url)
                  return (
                    <li key={f.id}>
                      <button
                        type="button"
                        aria-pressed={checked}
                        aria-label={t('fileMgr.selectFile', { name: f.name })}
                        className={cn(
                          'group block w-full overflow-hidden rounded-lg border border-border bg-card text-start transition-shadow hover:shadow-md motion-reduce:transition-none',
                          checked && 'ring-2 ring-primary',
                        )}
                        onClick={() => toggle(f.url)}
                        onDoubleClick={multiple ? undefined : confirm}
                      >
                        <div className="relative aspect-4/3 overflow-hidden border-b border-border">
                          <FileThumb file={f} />
                          {/* 勾选指示器随块点击切换，不再单独响应点击 */}
                          <span className="pointer-events-none absolute inset-s-1 top-1 flex size-7 items-center justify-center">
                            <Checkbox checked={checked} tabIndex={-1} className="border-border bg-background shadow-xs" />
                          </span>
                          {f.visibility === 'private' && (
                            <Badge variant="outline" className="absolute inset-e-1.5 top-1.5 gap-1 border-border bg-background/90 px-1.5 py-0.5 text-[11px] font-medium shadow-xs backdrop-blur-xs">
                              <Lock className="size-3" aria-hidden="true" />
                              {t('fileMgr.private')}
                            </Badge>
                          )}
                        </div>
                        <div className="min-w-0 p-2">
                          <p className="truncate text-xs font-medium" title={f.name}>
                            {f.name}
                          </p>
                          <p className="mt-0.5 truncate text-[11px] text-muted-foreground tabular-nums">{formatSize(f.size)}</p>
                        </div>
                      </button>
                    </li>
                  )
                })}
              </ul>
              {q.hasNextPage ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mt-3 w-full"
                  disabled={q.isFetchingNextPage}
                  onClick={() => q.fetchNextPage()}
                >
                  {q.isFetchingNextPage ? t('common.loading') : t('picker.loadMore')}
                </Button>
              ) : null}
            </>
          )}
        </div>
      </div>
    </ResponsiveDialog>
  )
}
