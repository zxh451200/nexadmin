import { useCallback, useEffect, useRef, useState } from 'react'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { ImagePlus, Plus, Upload, VideoIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { DragCursorHost } from './table/use-sortable-sensors'
import { ImageCropDialog, type CropConfig } from './ImageCropDialog'
import { FileTile, PendingTile, tileSize, type PendingItem } from '@/framework/upload/UploaderTiles'
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
}

const NO_CROP_TYPES = ['image/gif', 'image/svg+xml']

export function Uploader({ kind, multiple, maxCount, crop, visibility = 'private', value, onChange, disabled, className }: UploaderProps) {
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

  const accept = (files: File[], rid?: string) => {
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
  }

  const onPick = (e: React.ChangeEvent<HTMLInputElement>, rid?: string) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length) accept(files, rid)
  }

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

  return (
    <div className={cn('w-full', className)}>
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
            {canAdd && (
              <button
                type="button"
                onClick={() => addRef.current?.click()}
                aria-label={t('common.upload')}
                className={cn(
                  'flex flex-col items-center justify-center gap-1 rounded-md border border-dashed border-input bg-background text-sm text-muted-foreground transition-colors hover:border-primary hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none',
                  kind === 'file' ? 'min-h-14 w-full flex-row' : kind === 'video' ? 'aspect-video w-full max-w-xs sm:w-56' : tileSize('image'),
                )}
              >
                {kind === 'image' && !value.length ? <Plus className="size-6" aria-hidden="true" /> : <AddIcon className="size-5" aria-hidden="true" />}
                <span>{t(`uploader.add_${kind}`)}</span>
              </button>
            )}
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
