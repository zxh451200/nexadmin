import { useState } from 'react'
import { Mail, MailOpen, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { formatTime } from './format'
import { useDeleteMessage, useMessages, useSetMessageRead, type SiteMessage } from './api'

/** 官网「在线留言」收件箱：列表 + 详情；打开详情即标记已读。 */
export default function MessagePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [keyword, setKeyword] = useState('')
  const [draft, setDraft] = useState('')
  const [unread, setUnread] = useState(false)
  const [detail, setDetail] = useState<SiteMessage | null>(null)
  const { data, isLoading } = useMessages({ page, pageSize, keyword: keyword || undefined, unread: unread || undefined })
  const setRead = useSetMessageRead()
  const remove = useDeleteMessage()

  const open = (row: SiteMessage) => {
    setDetail(row)
    if (!row.read) void setRead.mutateAsync({ id: row.id, read: true }).catch(() => undefined)
  }

  const toggleRead = async (row: SiteMessage) => {
    try {
      await setRead.mutateAsync({ id: row.id, read: !row.read })
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDelete = async (row: SiteMessage) => {
    if (await confirm({ destructive: true, description: t('plugin_website.message.deleteConfirm', { name: row.name }) })) {
      try {
        await remove.mutateAsync(row.id)
        if (detail?.id === row.id) setDetail(null)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<SiteMessage>[] = [
    {
      key: 'read',
      title: t('plugin_website.message.status'),
      render: (r) =>
        r.read ? (
          <Badge variant="outline">{t('plugin_website.message.read')}</Badge>
        ) : (
          <Badge variant="info">{t('plugin_website.message.unread')}</Badge>
        ),
    },
    {
      key: 'name',
      title: t('plugin_website.message.name'),
      mobile: 'title',
      render: (r) => (
        <button type="button" className="font-medium text-foreground hover:underline" onClick={() => open(r)}>
          {r.name}
        </button>
      ),
    },
    {
      key: 'contact',
      title: t('plugin_website.message.contact'),
      mobile: 'sub',
      render: (r) => (
        <span className="text-muted-foreground">
          {r.email}
          {r.phone ? ` · ${r.phone}` : ''}
        </span>
      ),
    },
    {
      key: 'content',
      title: t('plugin_website.message.content'),
      render: (r) => <span className="line-clamp-1 text-muted-foreground">{r.content}</span>,
    },
    {
      key: 'createdAt',
      title: t('plugin_website.message.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.message.title')} description={t('plugin_website.message.desc')} />
      <FilterBar
        onSearch={() => {
          setKeyword(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft('')
          setKeyword('')
          setUnread(false)
          setPage(1)
        }}
      >
        <Input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t('plugin_website.message.searchPlaceholder')}
          aria-label={t('plugin_website.message.searchPlaceholder')}
          className="md:w-64"
        />
        <label className="flex h-10 items-center gap-2 whitespace-nowrap rounded-md border border-input bg-background px-3 text-sm md:h-9">
          <Switch
            checked={unread}
            onCheckedChange={(v) => {
              setUnread(v)
              setPage(1)
            }}
            aria-label={t('plugin_website.message.onlyUnread')}
          />
          {t('plugin_website.message.onlyUnread')}
        </label>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-messages"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_website.message.empty')}
        rowActions={(row) => (
          <Can perm="website:message">
            <Button
              variant="ghost"
              size="icon"
              aria-label={row.read ? t('plugin_website.message.markUnread') : t('plugin_website.message.markRead')}
              onClick={() => void toggleRead(row)}
            >
              {row.read ? <Mail className="size-4" aria-hidden="true" /> : <MailOpen className="size-4" aria-hidden="true" />}
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 className="size-4" aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {detail ? (
        <ResponsiveDialog
          open
          onOpenChange={(v) => {
            if (!v) setDetail(null)
          }}
          title={t('plugin_website.message.detailTitle')}
          footer={
            <Button variant="outline" onClick={() => setDetail(null)}>
              {t('common.close')}
            </Button>
          }
        >
          <dl className="flex flex-col gap-3 text-sm">
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.name')}</dt>
              <dd className="min-w-0 break-words">{detail.name}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.contact')}</dt>
              <dd className="min-w-0 break-words">
                <a className="underline" href={`mailto:${detail.email}`}>{detail.email}</a>
                {detail.phone ? <span className="text-muted-foreground"> · {detail.phone}</span> : null}
              </dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.createdAt')}</dt>
              <dd className="num">{formatTime(detail.createdAt, i18n.language)}</dd>
            </div>
            <div className="whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-3">{detail.content}</div>
            <div className="text-xs text-muted-foreground">{[detail.ip, detail.ua].filter(Boolean).join(' · ')}</div>
          </dl>
        </ResponsiveDialog>
      ) : null}
    </PageContainer>
  )
}
