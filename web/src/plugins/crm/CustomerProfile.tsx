import { useState } from 'react'
import { Check, Pencil, Phone, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DatePicker } from '@/framework/components/DatePicker'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { Can } from '@/framework/auth/Can'
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
  type CrmCustomer,
} from './api'
import { formatTime } from './format'

// 客户详情弹窗里的两块 P0 能力：联系人维护 + 跟进时间线。
// 都继承客户的数据范围（后端按 customer.owner 裁剪），前端只管展示与提交。

const KINDS: ActivityKind[] = ['call', 'visit', 'note', 'todo']

const kindVariant = (k: ActivityKind) =>
  k === 'todo' ? 'warning' : k === 'call' ? 'info' : k === 'visit' ? 'success' : 'outline'

function ContactForm({
  initial,
  prefill,
  onSubmit,
  onCancel,
  busy,
}: {
  initial?: CrmContact
  /** 新建时的预填（个人客户「一键把本人加成联系人」）。 */
  prefill?: Partial<CrmContactForm>
  onSubmit: (data: CrmContactForm) => Promise<void>
  onCancel: () => void
  busy?: boolean
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<Partial<CrmContactForm>>(
    initial
      ? { name: initial.name, title: initial.title, phone: initial.phone, email: initial.email, isPrimary: initial.isPrimary, remark: initial.remark }
      : (prefill ?? {}),
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
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">
            {t('plugin_crm.fields.contactName')}
            <RequiredMark />
          </span>
          <Input
            value={draft.name ?? ''}
            onChange={(e) => set({ name: e.target.value })}
            placeholder={t('plugin_crm.fields.contactName')}
            aria-label={t('plugin_crm.fields.contactName')}
          />
        </label>
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

export function ContactsSection({ customer }: { customer: CrmCustomer }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const customerId = customer.id
  const isCompany = customer.type === 'company'
  const [adding, setAdding] = useState(false)
  // 个人客户本人就是一条联系人记录：预填姓名与手机，省得照抄一遍。
  const [selfFill, setSelfFill] = useState(false)
  const [editing, setEditing] = useState<CrmContact | null>(null)
  const { data: contacts = [], isLoading } = useContacts(customerId)
  const save = useSaveContact()
  const remove = useDeleteContact()

  const openAdd = (asSelf: boolean) => {
    setSelfFill(asSelf)
    setAdding(true)
  }

  const closeForm = () => {
    setAdding(false)
    setSelfFill(false)
    setEditing(null)
  }

  const submit = async (data: CrmContactForm) => {
    try {
      await save.mutateAsync({ customerId, id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      closeForm()
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
      <div className="flex flex-col gap-1">
        <div className="flex items-baseline justify-between gap-2">
          <div className="text-sm font-medium text-foreground">{t('plugin_crm.contact.title')}</div>
          {!adding && !editing ? (
            <Can perm="crm:edit">
              <div className="flex items-center gap-2">
                {isCompany ? null : (
                  <Button variant="outline" size="sm" onClick={() => openAdd(true)}>
                    {t('plugin_crm.contact.addSelf')}
                  </Button>
                )}
                <Button variant="outline" size="sm" onClick={() => openAdd(false)}>
                  {t('plugin_crm.contact.add')}
                </Button>
              </div>
            </Can>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">
          {t(isCompany ? 'plugin_crm.contact.hintCompany' : 'plugin_crm.contact.hintPerson')}
        </p>
      </div>
      {isLoading ? (
        <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
      ) : adding || editing ? (
        <ContactForm
          initial={editing ?? undefined}
          prefill={
            selfFill
              ? { name: customer.name, phone: customer.phone, email: customer.email, isPrimary: true }
              : undefined
          }
          busy={save.isPending}
          onSubmit={submit}
          onCancel={closeForm}
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
            <Can perm="crm:edit">
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
            </Can>
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
        <DatePicker
          value={nextAt}
          onChange={setNextAt}
          placeholder={t('plugin_crm.activity.nextAt')}
          ariaLabel={t('plugin_crm.activity.nextAt')}
        />
      </div>
      <label className="flex flex-col gap-1">
        <span className="text-xs text-muted-foreground">
          {t('plugin_crm.activity.content')}
          <RequiredMark />
        </span>
        <Textarea
          rows={2}
          maxLength={2000}
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder={t('plugin_crm.activity.contentPlaceholder')}
          aria-label={t('plugin_crm.activity.content')}
        />
      </label>
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
            <Can perm="crm:edit">
              <Button variant="outline" size="sm" onClick={() => setWriting(true)}>
                {t('plugin_crm.activity.write')}
              </Button>
            </Can>
          ) : null}
        </div>
      </div>
      {writing ? <ActivityForm customerId={customerId} onDone={() => setWriting(false)} /> : null}
      {isLoading ? (
        <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
      ) : (data?.list ?? []).length ? (
        <ol className="flex flex-col gap-2">
          {(data?.list ?? []).map((a) =>
            // 系统留痕（归属/合并/标签等操作）走居中胶囊：它不是销售写的跟进，别占同一视觉权重。
            a.kind === 'system' ? (
              <li key={a.id} className="flex justify-center">
                <span className="max-w-[85%] rounded-full bg-muted px-2.5 py-1 text-center text-[11px] text-muted-foreground">
                  {a.content} · {formatTime(a.createdAt, i18n.language)}
                </span>
              </li>
            ) : (
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
            ),
          )}
        </ol>
      ) : (
        <p className="text-xs text-muted-foreground">{t('plugin_crm.activity.empty')}</p>
      )}
    </div>
  )
}
