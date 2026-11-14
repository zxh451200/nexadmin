import { useCallback, useEffect, useRef, useState } from 'react'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { ImagePlus, Upload, VideoIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { DragCursorHost } from './table/use-sortable-sensors'
import { ImageCropDialog, type CropConfig } from './ImageCropDialog'
import { FileTile, PendingTile, type PendingItem } from '@/framework/upload/UploaderTiles'
import { isAbortError, uploadFile } from '@/framework/upload/uploadClient'
import { precheckFile, useUploadConfig } from '@/framework/upload/precheck'
import { captureVideoPoster } from '@/framework/upload/videoPoster'
import { toWebp, webpName } from '@/framework/upload/toWebp'
import type { UploadedFile, UploadKind, UploadVisibility } from '@/framework/upload/types'

export type { UploadedFile } from '@/framework/upload/types'

export interface UploaderProps {
  kind: UploadKind
  multiple?: boolean
  /** 默认：单个为 1，multiple 为 9 */
  maxCount?: number
  /** 仅 kind=image 生效：固定比例/输出尺寸，或 'free' 自由裁剪 */
  crop?: CropConfig
  visibility?: UploadVisibility
  value: UploadedFile[]
  onChange: (value: UploadedFile[]) => void
  disabled?: boolean
  className?: string
  /** 开启后监听剪贴板粘贴（Ctrl+V 直接把截图/复制的文件加入上传）；默认关，避免多实例互相劫持 */
  enablePaste?: boolean
  /** 渲染在上传按钮右侧的附加入口（如「从文件库选择」）；上限已满/译文态时随入口行一起置灰 */
  entryExtra?: React.ReactNode
}

const NO_CROP_TYPES = ['image/gif', 'image/svg+xml']

export function Uploader({ kind, multiple, maxCount, crop, visibility = 'private', value, onChange, disabled, className, enablePaste, entryExtra }: UploaderProps) {
  const { t } = useTranslation()
  const { data: cfg } = useUploadConfig()
  const limit = maxCount ?? (multiple ? 9 : 1)
  const [pending, setPending] = useState<PendingItem[]>([])
  const [cropQueue, setCropQueue] = useState<File[]>([])
  const addRef = useRef<HTMLInputElement>(null)
  const replaceRef = useRef<HTMLInputElement>(null)
  const replaceId = useRef<string | undefined>(undefined)
  const cropReplaceId = useRef<string | undefined>(undefined)
  const controllers = useRef(new Map<string, AbortController>())
  const valueRef = useRef(value)
  valueRef.current = value
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  /** 图片地址导入：粘贴外链图片 URL 回车，登记进媒体库后加入列表（外链图不经裁剪） */
  const [urlText, setUrlText] = useState('')
  const [importing, setImporting] = useState(false)

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { delay: 200, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  useEffect(() => {
    const map = controllers.current
    return () => map.forEach((c) => c.abort())
  }, [])

  const patch = useCallback((uid: string, p: Partial<PendingItem>) => setPending((l) => l.map((x) => (x.uid === uid ? { ...x, ...p } : x))), [])
  const drop = useCallback((uid: string) => {
    controllers.current.delete(uid)
    setPending((l) => {
      const it = l.find((x) => x.uid === uid)
      if (it) URL.revokeObjectURL(it.previewUrl)
      return l.filter((x) => x.uid !== uid)
    })
  }, [])

  const run = useCallback(
    async (item: PendingItem) => {
      const ctrl = new AbortController()
      controllers.current.set(item.uid, ctrl)
      patch(item.uid, { error: undefined, progress: 0 })
      try {
        const posterP = kind === 'video' ? captureVideoPoster(item.file) : Promise.resolve(null)
        const up = await uploadFile(item.file, { kind, visibility, signal: ctrl.signal, onProgress: (p) => patch(item.uid, { progress: p }) })
        // 原图已落库；再传一份 WebP 副本（服务端按 Accept 优先下发）。任何一步失败都只影响省流量，不影响可用性。
        if (kind === 'image' && cfg?.imageWebp) {
          const webp = await toWebp(item.file).catch(() => null)
          if (webp && webp.size < up.size) {
            await uploadFile(new File([webp], webpName(item.file.name), { type: 'image/webp' }), {
              kind: 'image', visibility, variantOf: up.id, signal: ctrl.signal,
            }).catch(() => null)
          }
        }
        const blob = await posterP
        if (blob) {
          const poster = await uploadFile(new File([blob], `${item.file.name}.jpg`, { type: 'image/jpeg' }), { kind: 'image', visibility: 'public', signal: ctrl.signal }).catch(() => null)
          if (poster) up.posterUrl = poster.url
        }
        const cur = valueRef.current
        const next = item.replaceId ? cur.map((f) => (f.id === item.replaceId ? up : f)) : [...cur, up]
        valueRef.current = next
        onChangeRef.current(next)
        drop(item.uid)
      } catch (e) {
        if (isAbortError(e)) return drop(item.uid)
        patch(item.uid, { error: e instanceof Error ? e.message : 'error' })
        toast.error(t('uploader.failed'))
      }
    },
    [kind, visibility, patch, drop, t, cfg?.imageWebp],
  )

  const start = useCallback(
    (file: File, rid?: string) => {
      const item: PendingItem = { uid: `${Date.now()}-${Math.random().toString(36).slice(2)}`, file, previewUrl: URL.createObjectURL(file), progress: 0, replaceId: rid }
      setPending((l) => [...l, item])
      void run(item)
    },
    [run],
  )

  const accept = useCallback((files: File[], rid?: string) => {
    let room = rid ? files.length : limit - valueRef.current.length - pending.length
    const ok: File[] = []
    for (const f of files) {
      if (room <= 0) {
        toast.error(t('uploader.maxCount', { count: limit }))
        break
      }
      const err = precheckFile(f, kind, cfg)
      if (err) {
        toast.error(err.code === 'size' ? t('uploader.errSize', { name: f.name, limit: err.limitMb }) : err.code === 'type' ? t('uploader.errType', { name: f.name }) : t('uploader.errKind', { name: f.name, kind: t(`uploader.kind_${kind}`) }))
        continue
      }
      ok.push(f)
      room--
      if (rid) break
    }
    if (kind === 'image' && crop) {
      const toCrop = ok.filter((f) => !NO_CROP_TYPES.includes(f.type))
      ok.filter((f) => NO_CROP_TYPES.includes(f.type)).forEach((f) => start(f, rid))
      if (toCrop.length) {
        cropReplaceId.current = rid
        setCropQueue((q) => [...q, ...toCrop])
      }
    } else {
      ok.forEach((f) => start(f, rid))
    }
  }, [limit, kind, cfg, crop, start, t, pending.length])

  const onPick = (e: React.ChangeEvent<HTMLInputElement>, rid?: string) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length) accept(files, rid)
  }

  // 拖拽上传：OS 文件拖到上传区内直接走 accept（precheck 会拦类型不符的）。
  const [dragOver, setDragOver] = useState(false)
  const onDropFiles = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOver(false)
    if (disabled) return
    const files = Array.from(e.dataTransfer.files ?? [])
    if (files.length) accept(files)
  }

  // 剪贴板粘贴：仅 enablePaste 的实例监听（上传弹窗），避免页面上多个 Uploader 互相劫持 Ctrl+V
  useEffect(() => {
    if (!enablePaste) return
    const onPaste = (e: ClipboardEvent) => {
      if (disabled) return
      const files = Array.from(e.clipboardData?.files ?? [])
      if (files.length) {
        e.preventDefault()
        accept(files)
      }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  }, [enablePaste, disabled, accept])

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    const from = value.findIndex((f) => f.id === active.id)
    const to = value.findIndex((f) => f.id === over.id)
    if (from < 0 || to < 0) return
    onChange(arrayMove(value, from, to))
  }

  const accepts = kind === 'image' ? 'image/*' : kind === 'video' ? 'video/*' : cfg?.allowedTypes?.length ? cfg.allowedTypes.join(',') : undefined
  const canAdd = !disabled && value.length + pending.filter((p) => !p.replaceId).length < limit
  const sortable = !!multiple && value.length > 1 && !disabled
  const AddIcon = kind === 'image' ? ImagePlus : kind === 'video' ? VideoIcon : Upload
  const curCrop = cropQueue[0] ?? null

  /** 地址导入：把外链图片登记进媒体库（POST /files 支持外链 URL，后端校验协议）。
   *  外链图不经过裁剪管线（拿不到本地 blob），用原始地址直接进列表。 */
  const addByUrl = async () => {
    const url = urlText.trim()
    if (!url) return
    if (!/^https?:\/\//i.test(url)) {
      toast.error(t('uploader.urlInvalid'))
      return
    }
    setImporting(true)
    try {
      const name = decodeURIComponent(new URL(url).pathname.split('/').pop() ?? '') || 'image'
      const ext = name.includes('.') ? name.split('.').pop()?.toLowerCase() : ''
      const type = ext === 'png' ? 'image/png' : ext === 'webp' ? 'image/webp' : ext === 'gif' ? 'image/gif' : 'image/jpeg'
      const items = await api.post<Array<{ id: string; name: string; url: string; type: string; size: number }>>('/files', {
        files: [{ url, name, type, size: 0 }],
        visibility,
      })
      const room = limit - valueRef.current.length - pending.length
      if (room <= 0) {
        toast.error(t('uploader.maxCount', { count: limit }))
        return
      }
      const next = [...valueRef.current, ...items.slice(0, Math.max(0, room)).map((f) => ({ id: f.id, url: f.url, name: f.name, size: f.size ?? 0, type: f.type ?? type }))]
      valueRef.current = next
      onChangeRef.current(next)
      setUrlText('')
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出
    } finally {
      setImporting(false)
    }
  }

  return (
    <div
      className={cn('w-full', className, dragOver && 'rounded-md outline-2 outline-dashed outline-primary outline-offset-4')}
      onDragOver={(e) => {
        if (disabled) return
        e.preventDefault()
        setDragOver(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragOver(false)
      }}
      onDrop={onDropFiles}
    >
      {/* 上传入口行：地址导入 + 上传按钮 + 附加入口（如从文件库）。
          常驻渲染（满额/译文态只置灰不消失，与其他字段行为一致）；拖拽上传在整个容器上仍可用。
          入口行在缩略图上方：先选导入方式，下面按顺序铺结果（对齐登录表单「先输入后结果」的阅读顺序） */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {kind === 'image' && (
          <Input
            value={urlText}
            disabled={disabled || importing || !canAdd}
            onChange={(e) => setUrlText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                void addByUrl()
              }
            }}
            placeholder={t('uploader.urlPlaceholder')}
            aria-label={t('uploader.urlPlaceholder')}
            className="h-9 min-w-56 flex-1 text-sm sm:max-w-sm"
          />
        )}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-9"
          disabled={disabled || !canAdd}
          onClick={() => addRef.current?.click()}
          aria-label={t('common.upload')}
        >
          <AddIcon className="size-4" aria-hidden="true" />
          {t(`uploader.add_${kind}`)}
        </Button>
        {entryExtra}
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <DragCursorHost />
        <SortableContext items={value.map((f) => f.id)} strategy={kind === 'image' ? rectSortingStrategy : verticalListSortingStrategy}>
          <div className={cn(kind === 'image' ? 'flex flex-wrap gap-3' : 'flex flex-col gap-2')}>
            {value.map((f) => (
              <FileTile
                key={f.id}
                file={f}
                sortable={sortable}
                onRemove={() => onChange(value.filter((x) => x.id !== f.id))}
                onReplace={() => {
                  if (disabled) return
                  replaceId.current = f.id
                  replaceRef.current?.click()
                }}
              />
            ))}
            {pending.map((p) => (
              <PendingTile
                key={p.uid}
                item={p}
                kind={kind}
                onCancel={() => (controllers.current.get(p.uid)?.abort(), drop(p.uid))}
                onRetry={() => void run(p)}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>

      <input ref={addRef} type="file" hidden accept={accepts} multiple={!!multiple && limit > 1} onChange={(e) => onPick(e)} />
      <input
        ref={replaceRef}
        type="file"
        hidden
        accept={accepts}
        onChange={(e) => {
          const rid = replaceId.current
          replaceId.current = undefined
          onPick(e, rid)
        }}
      />

      <ImageCropDialog
        open={!!curCrop}
        file={curCrop}
        crop={crop ?? 'free'}
        onCancel={() => setCropQueue((q) => q.slice(1))}
        onConfirm={(f) => {
          setCropQueue((q) => q.slice(1))
          start(f, cropReplaceId.current)
        }}
      />
    </div>
  )
}
