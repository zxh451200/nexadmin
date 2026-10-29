import { useEffect, useRef, useState } from 'react'
import { ExternalLink, Link2, Play, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Progress } from '@/framework/ui/progress'
import { Switch } from '@/framework/ui/switch'
import { formatSize } from '@/framework/upload/precheck'
import { uploadToTus } from '@/framework/upload/uploadClient'
import { createVideoUpload, deleteVideo, saveVideo, useSaveVideoConfig, useVideoConfig, useVideos, type VideoItem } from './api'

const PAGE_SIZE = 10

export function VideoTab() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const cfg = useVideoConfig()
  const save = useSaveVideoConfig()
  const [page, setPage] = useState(1)
  const list = useVideos(page, PAGE_SIZE)
  const [form, setForm] = useState({ enabled: false, accountId: '', token: '', subdomain: '' })
  const [progress, setProgress] = useState<number | null>(null)
  const [playing, setPlaying] = useState<VideoItem | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (cfg.data) setForm({ enabled: cfg.data.enabled, accountId: cfg.data.accountId, token: '', subdomain: cfg.data.subdomain })
  }, [cfg.data])

  const onSaveConfig = async () => {
    try {
      await save.mutateAsync(form)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onPick = async (file: File | undefined) => {
    if (!file) return
    try {
      setProgress(0)
      const target = await createVideoUpload(file.name)
      await uploadToTus(target.uploadUrl, file, {
        kind: 'video',
        onProgress: setProgress,
      })
      await saveVideo({ videoId: target.videoId, name: file.name, size: file.size })
      await qc.invalidateQueries({ queryKey: ['media', 'video', 'assets'] })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    } finally {
      setProgress(null)
    }
  }

  const onDelete = async (row: VideoItem) => {
    if (!(await confirm({ destructive: true, description: t('plugin_media.video.deleteConfirm', { name: row.name }) }))) return
    try {
      await deleteVideo(row.id)
      await qc.invalidateQueries({ queryKey: ['media', 'video', 'assets'] })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const copyLink = async (row: VideoItem) => {
    if (!row.downloadUrl) {
      toast.error(t('plugin_media.video.noDownload'))
      return
    }
    try {
      await navigator.clipboard.writeText(row.downloadUrl)
      toast.success(t('plugin_media.video.copied'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<VideoItem>[] = [
    { key: 'name', title: t('plugin_media.col.name'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.name}</span> },
    { key: 'size', title: t('plugin_media.col.size'), align: 'end', render: (r) => <span className="num">{formatSize(r.size)}</span> },
    { key: 'status', title: t('plugin_media.col.state'), render: (r) => <Badge variant={r.status === 'ready' ? 'success' : 'secondary'}>{r.status}</Badge> },
    { key: 'uploader', title: t('plugin_media.video.uploader'), render: (r) => <span className="text-xs text-muted-foreground">{r.uploader}</span> },
    {
      key: 'createdAt',
      title: t('plugin_media.col.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{new Date(r.createdAt).toLocaleString()}</span>,
    },
    {
      key: 'actions',
      title: t('plugin_media.video.actions'),
      align: 'end',
      render: (r) => (
        <div className="flex items-center justify-end gap-1">
          <Button variant="ghost" size="icon" aria-label={t('plugin_media.video.play')} onClick={() => setPlaying(r)}>
            <Play aria-hidden="true" />
          </Button>
          <Button variant="ghost" size="icon" aria-label={t('plugin_media.video.copyLink')} title={t('plugin_media.video.copyLink')} onClick={() => void copyLink(r)}>
            <Link2 aria-hidden="true" />
          </Button>
          <a
            href={r.playbackUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            aria-label={t('plugin_media.video.open')}
          >
            <ExternalLink className="size-4" aria-hidden="true" />
          </a>
          <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(r)}>
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('plugin_media.video.configTitle')}</CardTitle>
          <CardDescription>{t('plugin_media.video.configDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div className="flex items-center justify-between gap-3 md:col-span-2">
            <Label htmlFor="video-enabled">{t('plugin_media.video.enabled')}</Label>
            <Switch id="video-enabled" checked={form.enabled} onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="video-account">{t('plugin_media.video.accountId')}</Label>
            <Input id="video-account" value={form.accountId} onChange={(e) => setForm((f) => ({ ...f, accountId: e.target.value }))} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="video-subdomain">{t('plugin_media.video.subdomain')}</Label>
            <Input id="video-subdomain" value={form.subdomain} onChange={(e) => setForm((f) => ({ ...f, subdomain: e.target.value }))} />
          </div>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="video-token">{t('plugin_media.video.token')}</Label>
            <Input
              id="video-token"
              type="password"
              autoComplete="new-password"
              value={form.token}
              placeholder={cfg.data?.hasToken ? t('plugin_media.video.tokenSaved') : ''}
              onChange={(e) => setForm((f) => ({ ...f, token: e.target.value }))}
            />
          </div>
          <div className="md:col-span-2">
            <Button onClick={() => void onSaveConfig()} disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <input
        ref={fileRef}
        type="file"
        accept="video/*"
        className="sr-only"
        aria-label={t('plugin_media.video.upload')}
        onChange={(e) => {
          const f = e.target.files?.[0]
          e.target.value = ''
          void onPick(f)
        }}
      />

      {progress !== null ? (
        <div className="flex items-center gap-3">
          <Progress className="flex-1" value={progress} aria-label={t('plugin_media.video.upload')} />
          <span className="num text-xs text-muted-foreground">{progress}%</span>
        </div>
      ) : null}

      <DataTable<VideoItem>
        toolbar={
          <Button onClick={() => fileRef.current?.click()} disabled={!cfg.data?.enabled || progress !== null}>
            <Upload aria-hidden="true" />
            {t('plugin_media.video.upload')}
          </Button>
        }
        columns={columns}
        data={list.data?.list ?? []}
        rowKey="id"
        loading={list.isLoading}
        columnSettingsKey="media-videos"
        pagination={{ page, pageSize: PAGE_SIZE, total: list.data?.total ?? 0, onChange: setPage }}
        emptyText={t('plugin_media.video.empty')}
      />

      <ResponsiveDialog
        open={!!playing}
        onOpenChange={(o) => !o && setPlaying(null)}
        title={playing?.name ?? ''}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setPlaying(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {playing ? (
          <div className="aspect-video w-full overflow-hidden rounded-md bg-black">
            <iframe
              key={playing.id}
              src={playing.playbackUrl}
              title={playing.name}
              className="h-full w-full"
              allow="accelerometer; gyroscope; autoplay; encrypted-media; picture-in-picture"
              allowFullScreen
            />
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
