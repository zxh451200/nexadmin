import { useState } from 'react'
import { Check, Pencil, Phone, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  useActivities,
  useContacts,
  useCreateActivity,
  useDeleteActivity,
  useDeleteContact,
  useSaveContact,
  useToggleActivityDone,
  type ActivityKind,
  type CrmActivity,
  type CrmContact,
  type CrmContactForm,
} from './api'
import { formatTime } from './format'

// 客户详情弹窗里的两块 P0 能力：联系人维护 + 跟进时间线。
// 都继承客户的数据范围（后端按 customer.owner 裁剪），前端只管展示与提交。

const KINDS: ActivityKind[] = ['call', 'visit', 'note', 'todo']

const kindVariant = (k: ActivityKind) =>
  k === 'todo' ? 'warning' : k === 'call' ? 'info' : k === 'visit' ? 'success' : 'outline'

function ContactForm({
  initial,
  onSubmit,
  onCancel,
  busy,
}: {
  initial?: CrmContact
  onSubmit: (data: CrmContactForm) => Promise<void>
  onCancel: () => void
  busy?: boolean
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<Partial<CrmContactForm>>(
    initial ? { name: initial.name, title: initial.title, phone: initial.phone, email: initial.email, isPrimary: initial.isPrimary, remark: initial.remark } : {},
  )
  const set = (patch: Partial<CrmContactForm>) => setDraft((d) => ({ ...d, ...patch }))

  const submit = async () => {
    if (!(draft.name ?? '').trim()) {
      toast.error(t('plugin_crm.contact.nameRequired'))
      return
    }
    await onSubmit(draft as CrmContactForm)
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <Input
          value={draft.name ?? ''}
          onChange={(e) => set({ name: e.target.value })}
          placeholder={t('plugin_crm.fields.contactName')}
          aria-label={t('plugin_crm.fields.contactName')}
        />
        <Input
          value={draft.title ?? ''}
          onChange={(e) => set({ title: e.target.value })}
          placeholder={t('plugin_crm.fields.contactTitle')}
          aria-label={t('plugin_crm.fields.contactTitle')}
        />
        <Input
          value={draft.phone ?? ''}
          onChange={(e) => set({ phone: e.target.value })}
          placeholder={t('plugin_crm.fields.phone')}
          aria-label={t('plugin_crm.fields.phone')}
        />
        <Input
          value={draft.email ?? ''}
          onChange={(e) => set({ email: e.target.value })}
          placeholder={t('plugin_crm.fields.email')}
          aria-label={t('plugin_crm.fields.email')}
        />
      </div>
      <Textarea
        rows={2}
        maxLength={500}
        value={draft.remark ?? ''}
        onChange={(e) => set({ remark: e.target.value })}
        placeholder={t('plugin_crm.fields.remark')}
        aria-label={t('plugin_crm.fields.remark')}
      />
      <div className="flex items-center justify-between gap-2">
        <label className="flex items-center gap-2 text-sm text-foreground">
          <Checkbox checked={!!draft.isPrimary} onCheckedChange={(v) => set({ isPrimary: v === true })} />
          {t('plugin_crm.contact.primary')}
        </label>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button size="sm" disabled={busy} onClick={() => void submit()}>
            {t('common.save')}
          </Button>
        </div>
      </div>
    </div>
  )
}

export function ContactsSection({ customerId }: { customerId: string }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<CrmContact | null>(null)
  const { data: contacts = [], isLoading } = useContacts(customerId)
  const save = useSaveContact()
  const remove = useDeleteContact()

  const submit = async (data: CrmContactForm) => {
    try {
      await save.mutateAsync({ customerId, id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      setAdding(false)
      setEditing(null)
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  const onDelete = async (ct: CrmContact) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.contact.deleteConfirm', { name: ct.name }) })) {
      try {
        await remove.mutateAsync(ct.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 失败原因已由请求层统一提示。
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <div className="text-sm font-medium text-foreground">{t('plugin_crm.contact.title')}</div>
        {!adding && !editing ? (
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            {t('plugin_crm.contact.add')}
          </Button>
        ) : null}
      </div>
      {isLoading ? (
        <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
      ) : adding || editing ? (
        <ContactForm
          initial={editing ?? undefined}
          busy={save.isPending}
          onSubmit={submit}
          onCancel={() => {
            setAdding(false)
            setEditing(null)
          }}
        />
      ) : contacts.length ? (
        contacts.map((ct) => (
          <div key={ct.id} className="flex items-start justify-between gap-3 rounded-md border border-border px-3 py-2">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="truncate text-sm text-foreground">{ct.name}</span>
                {ct.isPrimary ? <Badge variant="success">{t('plugin_crm.contact.primary')}</Badge> : null}
                {ct.title ? <span className="truncate text-xs text-muted-foreground">{ct.title}</span> : null}
              </div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                {ct.phone ? (
                  <span className="num inline-flex items-center gap-1">
                    <Phone className="size-3" aria-hidden="true" />
                    {ct.phone}
                  </span>
                ) : null}
                {ct.email ? <span className="num truncate">{ct.email}</span> : null}
                {ct.remark ? <span className="truncate">{ct.remark}</span> : null}
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => setEditing(ct)}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="text-destructive"
                aria-label={t('common.delete')}
                onClick={() => void onDelete(ct)}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          </div>
        ))
      ) : (
        <p className="text-xs text-muted-foreground">{t('plugin_crm.contact.empty')}</p>
      )}
    </div>
  )
}

function ActivityForm({ customerId, onDone }: { customerId: string; onDone: () => void }) {
  const { t } = useTranslation()
  const [kind, setKind] = useState<ActivityKind>('note')
  const [content, setContent] = useState('')
  const [nextAt, setNextAt] = useState('')
  const create = useCreateActivity()

  const submit = async () => {
    if (!content.trim()) {
      toast.error(t('plugin_crm.activity.contentRequired'))
      return
    }
    try {
      await create.mutateAsync({ customerId, data: { kind, content: content.trim(), nextAt: nextAt || undefined } })
      toast.success(t('common.operateSuccess'))
      setContent('')
      setNextAt('')
      onDone()
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[auto_1fr]">
        <Select value={kind} onValueChange={(v) => setKind(v as ActivityKind)}>
          <SelectTrigger aria-label={t('plugin_crm.activity.kindLabel')} className="sm:w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`plugin_crm.activity.kind.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          type="date"
          value={nextAt}
          onChange={(e) => setNextAt(e.target.value)}
          aria-label={t('plugin_crm.activity.nextAt')}
        />
      </div>
      <Textarea
        rows={2}
        maxLength={2000}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder={t('plugin_crm.activity.contentPlaceholder')}
        aria-label={t('plugin_crm.activity.content')}
      />
      <div className="flex justify-end">
        <Button size="sm" disabled={create.isPending} onClick={() => void submit()}>
          {t('plugin_crm.activity.submit')}
        </Button>
      </div>
    </div>
  )
}

export function ActivitiesSection({ customerId }: { customerId: string }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [writing, setWriting] = useState(false)
  const [openOnly, setOpenOnly] = useState(false)
  const { data, isLoading } = useActivities({ customerId, openOnly, page: 1, pageSize: 50 })
  const toggle = useToggleActivityDone()
  const remove = useDeleteActivity()

  const onDelete = async (a: CrmActivity) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.activity.deleteConfirm') })) {
      try {
        await remove.mutateAsync(a.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 失败原因已由请求层统一提示。
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <div className="text-sm font-medium text-foreground">{t('plugin_crm.activity.title')}</div>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Checkbox checked={openOnly} onCheckedChange={(v) => setOpenOnly(v === true)} />
            {t('plugin_crm.activity.openOnly')}
          </label>
          {!writing ? (
            <Button variant="outline" size="sm" onClick={() => setWriting(true)}>
              {t('plugin_crm.activity.write')}
            </Button>
          ) : null}
        </div>
      </div>
      {writing ? <ActivityForm customerId={customerId} onDone={() => setWriting(false)} /> : null}
      {isLoading ? (
        <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
      ) : (data?.list ?? []).length ? (
        <ol className="flex flex-col gap-2">
          {(data?.list ?? []).map((a) => (
            <li key={a.id} className="flex items-start justify-between gap-3 rounded-md border border-border px-3 py-2">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={kindVariant(a.kind)}>{t(`plugin_crm.activity.kind.${a.kind}`)}</Badge>
                  {a.kind === 'todo' && a.nextAt ? (
                    <span className="num text-xs text-muted-foreground">{t('plugin_crm.activity.due', { date: a.nextAt })}</span>
                  ) : null}
                  <span className="num text-xs text-muted-foreground">{formatTime(a.createdAt, i18n.language)}</span>
                  {a.author ? <span className="text-xs text-muted-foreground">{a.author}</span> : null}
                </div>
                <p className="mt-1 break-words whitespace-pre-wrap text-sm text-foreground">{a.content}</p>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {a.kind === 'todo' ? (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={a.done ? t('plugin_crm.activity.reopen') : t('plugin_crm.activity.complete')}
                    onClick={() => toggle.mutate({ id: a.id, done: !a.done })}
                  >
                    <Check className={a.done ? 'text-success' : ''} aria-hidden="true" />
                  </Button>
                ) : null}
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-destructive"
                  aria-label={t('common.delete')}
                  onClick={() => void onDelete(a)}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <p className="text-xs text-muted-foreground">{t('plugin_crm.activity.empty')}</p>
      )}
    </div>
  )
}
