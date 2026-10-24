import { useState } from 'react'
import { Download, Paperclip } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { RichTextView } from '@/framework/components/RichTextView'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { useMyNotices, useReadNotice, type MyNotice } from './api'
import { formatTime } from './format'

export function NoticeTab() {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<MyNotice | null>(null)
  const { data, isLoading } = useMyNotices({ page, pageSize })
  const readNotice = useReadNotice()

  const open = (n: MyNotice) => {
    setDetail(n)
    if (!n.read) {
      readNotice.mutate(n.id)
      setDetail({ ...n, read: true })
    }
  }

  const columns: Column<MyNotice>[] = [
    {
      key: 'title',
      title: t('message.fields.title'),
      mobile: 'title',
      render: (n) => (
        <div className="flex min-w-0 items-center gap-2">
          {!n.read ? (
            <span className="size-2 shrink-0 rounded-full bg-primary" role="img" aria-label={t('message.unread')} />
          ) : null}
          {n.pinned ? <Badge variant="outline">{t('message.pinned')}</Badge> : null}
          <span className={n.read ? 'truncate text-muted-foreground' : 'truncate font-medium text-foreground'}>{n.title}</span>
        </div>
      ),
    },
    { key: 'type', title: t('message.fields.type'), mobile: 'sub', render: (n) => <DictTag dict="notice_type" value={n.type} /> },
    { key: 'level', title: t('message.fields.level'), render: (n) => <DictTag dict="notice_level" value={n.level} /> },
    {
      key: 'publishedAt',
      title: t('message.fields.publishedAt'),
      render: (n) => <span className="num text-muted-foreground">{formatTime(n.publishedAt, i18n.language)}</span>,
    },
  ]

  return (
    <>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        onRowClick={open}
        emptyText={t('message.emptyNotices')}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(o) => !o && setDetail(null)}
        size="lg"
        title={detail?.title}
        footer={
          <Button type="button" variant="outline" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <DictTag dict="notice_type" value={detail.type} />
              <DictTag dict="notice_level" value={detail.level} />
              <span className="num">{formatTime(detail.publishedAt, i18n.language)}</span>
            </div>
            <RichTextView html={detail.content} />
            {detail.attachments?.length ? (
              <div className="flex flex-col gap-2">
                <h3 className="flex items-center gap-1 text-sm font-semibold text-foreground">
                  <Paperclip className="size-4" aria-hidden="true" />
                  {t('message.attachments')}
                </h3>
                <ul className="flex flex-col gap-1">
                  {detail.attachments.map((f) => (
                    <li key={f.id}>
                      <a
                        href={f.url}
                        download={f.name}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex min-h-11 items-center gap-2 text-sm text-primary hover:underline md:min-h-8"
                      >
                        <Download className="size-4" aria-hidden="true" />
                        <span className="truncate">{f.name}</span>
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        ) : null}
      </ResponsiveDialog>
    </>
  )
}
