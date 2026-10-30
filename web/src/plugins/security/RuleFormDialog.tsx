import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { useSaveRule, type IpRule, type IpRuleForm, type RuleAction, type RuleScope } from './api'

const empty: IpRuleForm = { action: 'deny', cidr: '', scope: 'all', remark: '', enabled: true }

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: IpRule | null
}

export function RuleFormDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const save = useSaveRule()
  const [v, setV] = useState<IpRuleForm>(empty)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      setV(editing ? { action: editing.action, cidr: editing.cidr, scope: editing.scope, remark: editing.remark, enabled: editing.enabled } : empty)
      setError('')
    }
  }, [open, editing])

  const onSubmit = async () => {
    const cidr = v.cidr.trim()
    if (!cidr) {
      setError(t('plugin_security.ip.errors.cidrRequired'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: { ...v, cidr, remark: v.remark.trim() } })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={editing ? t('plugin_security.ip.editTitle') : t('plugin_security.ip.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={save.isPending} onClick={() => void onSubmit()}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <FormGrid>
        <div className="flex flex-col gap-2">
          <Label htmlFor="sec-rule-action">{t('plugin_security.ip.action')}</Label>
          <Select value={v.action} onValueChange={(a) => setV((s) => ({ ...s, action: a as RuleAction }))}>
            <SelectTrigger id="sec-rule-action">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="deny">{t('plugin_security.ip.actions.deny')}</SelectItem>
              <SelectItem value="allow">{t('plugin_security.ip.actions.allow')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="sec-rule-scope">{t('plugin_security.ip.scope')}</Label>
          <Select value={v.scope} onValueChange={(a) => setV((s) => ({ ...s, scope: a as RuleScope }))}>
            <SelectTrigger id="sec-rule-scope">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('plugin_security.ip.scopes.all')}</SelectItem>
              <SelectItem value="super">{t('plugin_security.ip.scopes.super')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <FormGridFull>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sec-rule-cidr">{t('plugin_security.ip.cidr')}</Label>
            <Input
              id="sec-rule-cidr"
              className="font-mono"
              value={v.cidr}
              onChange={(e) => setV((s) => ({ ...s, cidr: e.target.value }))}
              placeholder="203.0.113.0/24, 2001:db8::/32"
              spellCheck={false}
              autoComplete="off"
              maxLength={64}
            />
            <p className="text-sm text-muted-foreground">{t('plugin_security.ip.cidrHint')}</p>
          </div>
        </FormGridFull>
        <FormGridFull>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sec-rule-remark">{t('plugin_security.ip.remark')}</Label>
            <Textarea id="sec-rule-remark" rows={3} value={v.remark} onChange={(e) => setV((s) => ({ ...s, remark: e.target.value }))} maxLength={200} />
          </div>
        </FormGridFull>
        <FormGridFull>
          <SwitchRow label={t('plugin_security.ip.enabled')} checked={v.enabled} onCheckedChange={(c) => setV((s) => ({ ...s, enabled: c }))} />
        </FormGridFull>
        {error ? (
          <FormGridFull>
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          </FormGridFull>
        ) : null}
      </FormGrid>
    </ResponsiveDialog>
  )
}
