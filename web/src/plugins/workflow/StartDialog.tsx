// StartDialog：发起流程。只列出已发布定义；业务类型/业务键选填，用于把实例挂到业务单据上；
// 流程变量选填——排他网关按变量路由（如 amount>10000），不填永远走默认分支。
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'

import { EmptyState } from '@/framework/components/EmptyState'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'

export interface StartValues {
  defId: string
  title: string
  bizType?: string
  businessKey?: string
  vars?: Record<string, string>
}

export function StartDialog({
  open,
  defs,
  onClose,
  onSubmit,
}: {
  open: boolean
  defs: { id: string; name: string; key: string; status: string }[]
  onClose: () => void
  onSubmit: (v: StartValues) => Promise<void> | void
}) {
  const { t } = useTranslation()
  const published = defs.filter((d) => d.status === 'published')
  const [defId, setDefId] = useState('')
  const [title, setTitle] = useState('')
  const [bizType, setBizType] = useState('')
  const [businessKey, setBusinessKey] = useState('')
  const [varRows, setVarRows] = useState<{ key: string; value: string }[]>([])
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    setDefId(published[0]?.id ?? '')
    setTitle('')
    setBizType('')
    setBusinessKey('')
    setVarRows([])
    setSubmitting(false)
    // published 由 defs 派生，open 时重置一次即可
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const submit = async () => {
    if (!defId || !title.trim()) return
    setSubmitting(true)
    try {
      // 同名变量后行覆盖前行；键空白或值为空都丢弃——引擎里空值与未设置是两回事，但表单层面空值只会误导
      const vars: Record<string, string> = {}
      for (const r of varRows) {
        const k = r.key.trim()
        if (k && r.value.trim() !== '') vars[k] = r.value.trim()
      }
      await onSubmit({
        defId,
        title: title.trim(),
        bizType: bizType.trim() || undefined,
        businessKey: businessKey.trim() || undefined,
        vars: Object.keys(vars).length ? vars : undefined,
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
      title={t('plugin_workflow.startTitle')}
      description={t('plugin_workflow.startDesc')}
      size="md"
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={submitting || !defId || !title.trim()}>
            {t('plugin_workflow.startSubmit')}
          </Button>
        </>
      }
    >
      {published.length === 0 ? (
        <EmptyState title={t('plugin_workflow.startNoPublished')} className="py-6" />
      ) : (
        <div className="space-y-3">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-xs text-muted-foreground">
              {t('plugin_workflow.fields.definition')}
              <span aria-hidden="true" className="ms-0.5 text-destructive">
                *
              </span>
            </span>
            <SimpleSelect
              aria-label={t('plugin_workflow.fields.definition')}
              value={defId}
              onChange={setDefId}
              options={published.map((d) => ({ value: d.id, label: `${d.name}（${d.key}）` }))}
            />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-xs text-muted-foreground">
              {t('plugin_workflow.fields.title')}
              <span aria-hidden="true" className="ms-0.5 text-destructive">
                *
              </span>
            </span>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t('plugin_workflow.fields.titlePlaceholder')}
              autoFocus
            />
          </label>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.fields.bizType')}</span>
              <Input value={bizType} onChange={(e) => setBizType(e.target.value)} placeholder="order / leave …" />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.fields.businessKey')}</span>
              <Input value={businessKey} onChange={(e) => setBusinessKey(e.target.value)} placeholder={t('plugin_workflow.fields.businessKeyPlaceholder')} />
            </label>
          </div>
          <p className="text-xs leading-5 text-muted-foreground">{t('plugin_workflow.startBizHint')}</p>
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">{t('plugin_workflow.varsTitle')}</span>
              <Button type="button" variant="outline" size="sm" onClick={() => setVarRows((rows) => [...rows, { key: '', value: '' }])}>
                <Plus className="size-4" aria-hidden="true" />
                {t('plugin_workflow.addVar')}
              </Button>
            </div>
            {varRows.length > 0 ? <p className="text-xs text-muted-foreground">{t('plugin_workflow.varsHint')}</p> : null}
            {varRows.map((r, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  className="w-36 shrink-0 font-mono"
                  value={r.key}
                  onChange={(e) => setVarRows((rows) => rows.map((x, idx) => (idx === i ? { ...x, key: e.target.value } : x)))}
                  placeholder={t('plugin_workflow.varName')}
                  aria-label={t('plugin_workflow.varName')}
                />
                <Input
                  value={r.value}
                  onChange={(e) => setVarRows((rows) => rows.map((x, idx) => (idx === i ? { ...x, value: e.target.value } : x)))}
                  placeholder={t('plugin_workflow.varValue')}
                  aria-label={t('plugin_workflow.varValue')}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('common.delete')}
                  onClick={() => setVarRows((rows) => rows.filter((_, idx) => idx !== i))}
                >
                  <Trash2 className="size-4" aria-hidden="true" />
                </Button>
              </div>
            ))}
          </div>
        </div>
      )}
    </ResponsiveDialog>
  )
}
