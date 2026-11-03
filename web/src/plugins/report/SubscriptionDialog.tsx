import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  useDeleteSubscription,
  useReportSubscription,
  useSaveSubscription,
  type ReportSubscriptionForm,
  type SubscriptionSchedule,
} from './api'

// 报表订阅配置弹窗：每张报表一条，到点给收件人列表发文本提醒。

const EMAIL_RE = /^[^@\s]+@[^@\s]+\.[^@\s]+$/

export function SubscriptionDialog({ report, onClose }: { report: { id: string; name: string } | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data: current } = useReportSubscription(report?.id)
  const save = useSaveSubscription()
  const del = useDeleteSubscription()
  const [form, setForm] = useState<Required<ReportSubscriptionForm>>({ schedule: 'daily', sendHour: 8, weekday: 1, recipients: '', enabled: true })
  const [emailErr, setEmailErr] = useState('')

  useEffect(() => {
    if (!report) return
    // 数据到货后回填；未配置过给默认值
    if (current) {
      setForm({
        schedule: current.schedule,
        sendHour: current.sendHour,
        weekday: current.weekday,
        recipients: current.recipients,
        enabled: current.enabled,
      })
    } else {
      setForm({ schedule: 'daily', sendHour: 8, weekday: 1, recipients: '', enabled: true })
    }
    setEmailErr('')
  }, [report, current])

  const submit = async () => {
    if (!report) return
    const emails = form.recipients.split(/[,;\s]+/).filter(Boolean)
    if (emails.length === 0) {
      setEmailErr(t('plugin_report.subscription.emailRequired'))
      return
    }
    const bad = emails.find((e) => !EMAIL_RE.test(e))
    if (bad) {
      setEmailErr(t('plugin_report.subscription.emailInvalid', { email: bad }))
      return
    }
    setEmailErr('')
    try {
      await save.mutateAsync({ id: report.id, data: { ...form, recipients: emails.join(',') } })
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const remove = async () => {
    if (!report || !(await confirm({ destructive: true, description: t('plugin_report.subscription.deleteConfirm') }))) return
    try {
      await del.mutateAsync(report.id)
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const weekdayItem = (i: number) => (
    <SelectItem key={i} value={String(i)}>
      {t(`plugin_report.subscription.wd.${i}`)}
    </SelectItem>
  )

  return (
    <ResponsiveDialog
      open={!!report}
      onOpenChange={(o) => !o && onClose()}
      title={`${t('plugin_report.subscription.title')} · ${report?.name ?? ''}`}
      description={t('plugin_report.subscription.desc')}
      size="sm"
      footer={
        <>
          {current ? (
            <Button variant="destructive" className="me-auto" onClick={() => void remove()} disabled={del.isPending}>
              {t('common.delete')}
            </Button>
          ) : null}
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4 text-sm">
        <div className="flex flex-col gap-1.5">
          <Label>{t('plugin_report.subscription.schedule')}</Label>
          <Select value={form.schedule} onValueChange={(v) => setForm((f) => ({ ...f, schedule: v as SubscriptionSchedule }))}>
            <SelectTrigger aria-label={t('plugin_report.subscription.schedule')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="daily">{t('plugin_report.subscription.daily')}</SelectItem>
              <SelectItem value="weekly">{t('plugin_report.subscription.weekly')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1.5">
            <Label>{t('plugin_report.subscription.hour')}</Label>
            <Input
              type="number"
              min={0}
              max={23}
              value={form.sendHour}
              onChange={(e) => setForm((f) => ({ ...f, sendHour: Math.max(0, Math.min(23, Number(e.target.value) || 0)) }))}
              aria-label={t('plugin_report.subscription.hour')}
            />
          </div>
          {form.schedule === 'weekly' ? (
            <div className="flex flex-col gap-1.5">
              <Label>{t('plugin_report.subscription.weekday')}</Label>
              <Select value={String(form.weekday)} onValueChange={(v) => setForm((f) => ({ ...f, weekday: Number(v) }))}>
                <SelectTrigger aria-label={t('plugin_report.subscription.weekday')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[0, 1, 2, 3, 4, 5, 6].map(weekdayItem)}
                </SelectContent>
              </Select>
            </div>
          ) : null}
        </div>
        <div className="space-y-1.5">
          <Label>{t('plugin_report.subscription.recipients')}</Label>
          <Textarea
            rows={3}
            value={form.recipients}
            onChange={(e) => setForm((f) => ({ ...f, recipients: e.target.value }))}
            placeholder="ops@example.com, boss@example.com"
            aria-invalid={!!emailErr}
          />
          {emailErr ? <p className="text-xs text-destructive">{emailErr}</p> : null}
          <p className="text-xs text-muted-foreground">{t('plugin_report.subscription.recipientsHint')}</p>
        </div>
        <SwitchRow
          label={t('plugin_report.subscription.enabled')}
          checked={form.enabled}
          onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
        />
        {current?.lastSentAt ? (
          <p className="text-xs text-muted-foreground">
            {t('plugin_report.subscription.lastSent', { time: new Date(current.lastSentAt).toLocaleDateString(i18n.language) })}
          </p>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}
