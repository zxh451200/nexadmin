import { useCallback, useEffect, useRef, useState } from 'react'
import { DndContext, KeyboardSensor, MouseSensor, TouchSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { Camera, ImagePlus, Images, Upload, UserRound, VideoIcon } from 'lucide-react'
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
import { FilePickerDialog, PickFromLibraryButton, toUploadedFile } from './FilePickerDialog'
import type { FileItem } from '@/framework/modules/file/api'


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
  /** 渲染在上传按钮右侧的附加入口；上限已满/译文态时随入口行一起置灰 */
  entryExtra?: React.ReactNode
  /** 入口行输入框不设宽度上限：默认 sm:max-w-sm 让宽屏下的地址框不至于长到离谱，
   *  但和上方同为 URL 字段的行并排时会短一截、右端还空一块，这种表单里传 true 让整行吃满 */
  entryFluid?: boolean
  /** avatar：头像形态——圆形预览 + 上传/更换/移除按钮，无地址导入行与缩略图列表（裁剪管线同 default） */
  variant?: 'default' | 'avatar'
  /** avatar 形态的标题/说明；不传时用 uploader 通用文案 */
  avatarLabel?: React.ReactNode
  avatarHint?: React.ReactNode
  /** 内建「从文件库选择」入口：image 形态默认开启（全站图片入口统一，头像/用户表单/商品图集不再各自接线），
   *  file/video 形态默认关（附件库选择由调用方按需接 entryExtra）；自带选择流的入口（富文本 MediaDialog、
   *  文件库自身的 UploadDialog）传 false 关掉，避免双按钮。 */
  pickFromLibrary?: boolean
}

const NO_CROP_TYPES = ['image/gif', 'image/svg+xml']

/** 地址导入按扩展名猜 mime；猜不中按 kind 兜底（image→jpeg、video→mp4、file→octet-stream） */
const MIME_BY_EXT: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', webp: 'image/webp', gif: 'image/gif', svg: 'image/svg+xml', avif: 'image/avif',
  mp4: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', ogg: 'video/ogg',
  pdf: 'application/pdf', zip: 'application/zip',
}

export function Uploader({ kind, multiple, maxCount, crop, visibility = 'private', value, onChange, disabled, className, enablePaste, entryExtra, entryFluid, variant = 'default', avatarLabel, avatarHint, pickFromLibrary: pickFromLibraryOpt }: UploaderProps) {
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
  // 内建「从文件库选择」：image 形态默认开启，自带选择流的调用方传 false 关掉
  const withLibrary = pickFromLibraryOpt ?? kind === 'image'
  const [pickOpen, setPickOpen] = useState(false)
  // 库里的文件是终态（不经裁剪/截帧管线）：按 url 映射成通用值回填——单图替换、多图追加去重
  const onLibraryPick = (urls: string[], items: FileItem[]) => {
    const picked: UploadedFile[] = urls.map((u) => {
      const it = items.find((f) => f.url === u)
      return it
        ? toUploadedFile(it)
        : { id: u, url: u, name: decodeURIComponent(u.split('/').pop() ?? u), size: 0, type: 'image/*' }
    })
    if (multiple) {
      const known = new Set(valueRef.current.map((f) => f.url))
      onChangeRef.current([...valueRef.current, ...picked.filter((f) => !known.has(f.url))].slice(0, limit))
    } else {
      onChangeRef.current(picked.slice(-1))
    }
  }
  const libraryDialog = withLibrary ? (
    <FilePickerDialog
      open={pickOpen}
      onOpenChange={setPickOpen}
      kind={kind}
      multiple={multiple}
      value={value.map((f) => f.url)}
      onConfirm={onLibraryPick}
      disabled={disabled}
    />
  ) : null
  /** 地址导入：粘贴外链 URL 回车，登记进媒体库后加入列表（外链文件不经裁剪/截帧管线） */
  const [urlText, setUrlText] = useState('')
  const [importing, setImporting] = useState(false)

  // 桌面按下就走（位移 5px 起算，点一下仍是「看大图」）；触屏长按 200ms，免得滑动页面误触。
  // 之前两种设备共用 PointerSensor 的 delay:200——鼠标按住后一移动就超 tolerance，看起来就是「拖不动」。
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 8 } }),
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

  /** 地址导入：把外链文件登记进媒体库（POST /files 支持外链 URL，后端校验协议）。
   *  外链文件拿不到本地 blob，不经裁剪/截帧管线，按扩展名猜个 mime 直接进列表。 */
  const addByUrl = async () => {
    const url = urlText.trim()
    if (!url) return
    if (!/^https?:\/\//i.test(url)) {
      toast.error(t('uploader.urlInvalid'))
      return
    }
    setImporting(true)
    try {
      const name = decodeURIComponent(new URL(url).pathname.split('/').pop() ?? '') || kind
      const ext = name.includes('.') ? name.split('.').pop()?.toLowerCase() : ''
      const type = MIME_BY_EXT[ext ?? ''] ?? (kind === 'image' ? 'image/jpeg' : kind === 'video' ? 'video/mp4' : 'application/octet-stream')
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

  // 头像形态：圆形预览（带相机角标、点击即选图）+ 右侧标题/说明/操作；
  // 单图、无地址导入行与缩略图列表；裁剪与上传管线与 default 完全同一套（裁剪对话框、WebP 副本、precheck）。
  if (variant === 'avatar') {
    const cur = value[0] ?? null
    const uploading = pending[0] ?? null
    const previewUrl = uploading?.previewUrl ?? cur?.url ?? ''
    const actionLabel = cur ? t('common.replace') : t('common.upload')
    const pick = () => {
      if (disabled || uploading) return
      if (cur) {
        replaceId.current = cur.id
        replaceRef.current?.click()
      } else {
        addRef.current?.click()
      }
    }
    return (
      <div
        className={cn(
          'w-full rounded-xl border border-border/70 bg-muted/30 p-4',
          className,
          dragOver && 'outline-2 outline-dashed outline-primary outline-offset-2',
        )}
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
        <div className="flex flex-col gap-4 text-center sm:flex-row sm:items-center sm:text-left">
          <button
            type="button"
            className="group relative mx-auto size-[72px] shrink-0 rounded-full ring-1 ring-border ring-offset-2 ring-offset-background sm:mx-0"
            onClick={pick}
            aria-label={actionLabel}
          >
            <span className="relative block size-full overflow-hidden rounded-full bg-background">
              {previewUrl ? (
                <img src={previewUrl} alt="" className="size-full object-cover" />
              ) : (
                <span className="grid size-full place-items-center">
                  <UserRound className="size-7 text-muted-foreground/70" aria-hidden="true" />
                </span>
              )}
              {uploading ? (
                <span className="absolute inset-0 grid place-items-center bg-foreground/60 text-xs font-medium text-background num">
                  {Math.round((uploading.progress ?? 0) * 100)}%
                </span>
              ) : null}
            </span>
            {uploading ? null : (
              <span className="absolute -bottom-0.5 -right-0.5 grid size-6 place-items-center rounded-full border-2 border-background bg-primary text-primary-foreground shadow-sm transition-transform group-hover:scale-105">
                <Camera className="size-3.5" aria-hidden="true" />
              </span>
            )}
          </button>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-foreground">{avatarLabel ?? t('uploader.avatarTitle')}</p>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">{avatarHint ?? t('uploader.avatarHint')}</p>
            <div className="mt-3 flex flex-wrap justify-center gap-2 sm:justify-start">
              <Button type="button" variant="outline" size="sm" className="h-8" disabled={disabled || !!uploading} onClick={pick}>
                <AddIcon className="size-4" aria-hidden="true" />
                {actionLabel}
              </Button>
              {withLibrary ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2.5"
                  disabled={disabled || !!uploading}
                  onClick={() => setPickOpen(true)}
                >
                  <Images className="size-4" aria-hidden="true" />
                  {t('uploader.pickFromLibrary')}
                </Button>
              ) : null}
              {cur && !uploading ? (
                <Button type="button" variant="ghost" size="sm" className="h-8 px-2.5 text-destructive hover:text-destructive" onClick={() => onChange([])}>
                  {t('common.remove')}
                </Button>
              ) : null}
            </div>
          </div>
        </div>
        <input ref={addRef} type="file" hidden accept={accepts} onChange={(e) => onPick(e)} />
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
        {libraryDialog}
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
          placeholder={t('uploader.urlPlaceholder', { kind: t(`uploader.kind_${kind}`) })}
          aria-label={t('uploader.urlPlaceholder', { kind: t(`uploader.kind_${kind}`) })}
          className={cn('h-9 min-w-56 flex-1 text-sm', !entryFluid && 'sm:max-w-sm')}
        />
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
          {t('common.upload')}
        </Button>
        {withLibrary ? (
          <PickFromLibraryButton onClick={() => setPickOpen(true)} disabled={disabled || !canAdd} />
        ) : null}
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
        {/* 拖拽排序没有任何可见标识（抓手是整块图、光标只在悬停时出现），可发现性为零；
            只在真能拖的时候给一行提示，单图/禁用态不出现以免噪音 */}
        {sortable ? <p className="mt-2 text-xs text-muted-foreground">{t('uploader.sortHint')}</p> : null}
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

      {libraryDialog}
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
