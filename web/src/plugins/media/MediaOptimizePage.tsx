import { useCallback, useEffect, useRef, useState } from 'react'
import { ImageDown, Images, PackageCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Progress } from '@/framework/ui/progress'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { formatSize } from '@/framework/upload/precheck'
import { toWebp, webpName } from '@/framework/upload/toWebp'
import { uploadFile } from '@/framework/upload/uploadClient'
import { useMediaStats, usePendingImages, type PendingImage } from './api'
import { VideoTab } from './VideoTab'

type RowState = 'idle' | 'working' | 'done' | 'error'

const PAGE_SIZE = 20

export default function MediaOptimizePage() {
  const { t } = useTranslation()
  const stats = useMediaStats()
  const [page, setPage] = useState(1)
  const list = usePendingImages(page, PAGE_SIZE)
  const [state, setState] = useState<Record<string, RowState>>({})
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(0)
  const [saved, setSaved] = useState(0)
  const stop = useRef(false)

  useEffect(() => () => {
    stop.current = true
  }, [])

  const rows = list.data?.list ?? []

  /** 原图仍然保留：这里只是把浏览器转出来的 WebP 作为派生文件挂上去。 */
  const convert = useCallback(async (img: PendingImage): Promise<'done' | 'skipped'> => {
    const res = await fetch(img.url, { credentials: 'same-origin' })
    if (!res.ok) throw new Error(String(res.status))
    const original = await res.blob()
    const file = new File([original], img.name, { type: img.type })
    const webp = await toWebp(file)
    // 已经是高压缩图或浏览器不支持编码：跳过，不算失败。
    if (!webp || webp.size >= original.size) return 'skipped'
    await uploadFile(new File([webp], webpName(img.name), { type: 'image/webp' }), {
      kind: 'image',
      visibility: 'public',
      variantOf: img.id,
    })
    setSaved((n) => n + (original.size - webp.size))
    return 'done'
  }, [])

  const runOne = useCallback(async (img: PendingImage) => {
    setState((s) => ({ ...s, [img.id]: 'working' }))
    try {
      const r = await convert(img)
      setState((s) => ({ ...s, [img.id]: 'done' }))
      setDone((n) => n + 1)
      if (r === 'skipped') toast.info(t('plugin_media.skippedOne', { name: img.name }))
      return true
    } catch {
      setState((s) => ({ ...s, [img.id]: 'error' }))
      return false
    }
  }, [convert, t])

  const runAll = async () => {
    if (running) return
    stop.current = false
    setRunning(true)
    setDone(0)
    setSaved(0)
    let failed = 0
    for (const img of rows) {
      if (stop.current) break
      if (!(await runOne(img))) failed++
    }
    setRunning(false)
    await Promise.all([stats.refetch(), list.refetch()])
    if (failed) toast.error(t('plugin_media.someFailed', { count: failed }))
    else toast.success(t('plugin_media.allDone'))
  }

  const columns: Column<PendingImage>[] = [
    {
      key: 'name',
      title: t('plugin_media.col.name'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <div className="truncate font-medium text-foreground">{r.name}</div>
          <div className="num text-xs text-muted-foreground">{r.type}</div>
        </div>
      ),
    },
    { key: 'size', title: t('plugin_media.col.size'), align: 'end', render: (r) => <span className="num">{formatSize(r.size)}</span> },
    {
      key: 'createdAt',
      title: t('plugin_media.col.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{new Date(r.createdAt).toLocaleString()}</span>,
    },
    {
      key: 'state',
      title: t('plugin_media.col.state'),
      align: 'end',
      render: (r) => {
        const st = state[r.id]
        if (st === 'working') return <Badge variant="secondary">{t('plugin_media.state.working')}</Badge>
        if (st === 'done') return <Badge variant="success">{t('plugin_media.state.done')}</Badge>
        if (st === 'error') return <Badge variant="destructive">{t('plugin_media.state.error')}</Badge>
        return (
          <Button variant="outline" size="sm" disabled={running} onClick={() => void runOne(r)}>
            {t('plugin_media.action.one')}
          </Button>
        )
      },
    },
  ]

  const percent = rows.length > 0 ? Math.round((done / rows.length) * 100) : 0

  return (
    <PageContainer>
      <PageHeader title={t('plugin_media.title')} description={t('plugin_media.desc')} />

      <Tabs defaultValue="images" className="flex flex-col gap-4">
        <TabsList className="w-full sm:w-fit">
          <TabsTrigger value="images" className="flex-1 sm:flex-none">
            {t('plugin_media.tab.images')}
          </TabsTrigger>
          <TabsTrigger value="videos" className="flex-1 sm:flex-none">
            {t('plugin_media.tab.videos')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="images" className="mt-0 flex flex-col gap-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 md:gap-4">
            <StatCard tone="chart-1" title={t('plugin_media.stats.total')} value={String(stats.data?.total ?? 0)} icon={<Images aria-hidden="true" />} />
            <StatCard tone="success" title={t('plugin_media.stats.done')} value={String(stats.data?.done ?? 0)} icon={<PackageCheck aria-hidden="true" />} />
            <StatCard tone="warning" title={t('plugin_media.stats.pending')} value={String(stats.data?.pending ?? 0)} icon={<ImageDown aria-hidden="true" />} />
          </div>

          {running ? (
            <div className="flex items-center gap-3">
              <Progress className="flex-1" value={percent} aria-label={t('plugin_media.action.all')} />
              <span className="num text-xs text-muted-foreground">
                {done}/{rows.length}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  stop.current = true
                }}
              >
                {t('common.cancel')}
              </Button>
            </div>
          ) : null}

          <DataTable<PendingImage>
            toolbar={
              <>
                <Button onClick={() => void runAll()} disabled={running || rows.length === 0}>
                  <ImageDown aria-hidden="true" />
                  {t('plugin_media.action.all')}
                </Button>
                {saved > 0 ? <span className="text-xs text-muted-foreground">{t('plugin_media.saved', { size: formatSize(saved) })}</span> : null}
              </>
            }
            columns={columns}
            data={rows}
            rowKey="id"
            loading={list.isLoading}
            columnSettingsKey="media-pending"
            pagination={{
              page,
              pageSize: PAGE_SIZE,
              total: list.data?.total ?? 0,
              onChange: setPage,
            }}
            emptyText={t('plugin_media.empty')}
          />

          <p className="text-xs text-muted-foreground">{t('plugin_media.note')}</p>
        </TabsContent>

        <TabsContent value="videos" className="mt-0">
          <VideoTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
