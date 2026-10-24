import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DateTimePicker } from '@/framework/components/DateTimePicker'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { downloadDefinition, useVersions, type Audience, type FormDefinition, type FormSettings } from './api'
import { AudiencePicker } from './AudiencePicker'
import { Field, NumInput } from './controls'
import { formatTime } from './format'
import { MaxSubmissionCap } from './limits'

interface Props {
  formId: string
  def: FormDefinition
  audience: Audience
  onDef: (def: FormDefinition) => void
  onAudience: (a: Audience) => void
}

/** 表单设置：名称/说明/重复提交/截止时间/次数上限/提交后提示，加可填写范围与发布版本 */
export function SettingsPanel({ formId, def, audience, onDef, onAudience }: Props) {
  const { t, i18n } = useTranslation()
  const { data: versions = [] } = useVersions(formId)
  const s = def.settings
  const setS = (p: Partial<FormSettings>) => onDef({ ...def, settings: { ...s, ...p } })

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <section className="flex flex-col gap-4 rounded-lg border border-border bg-card p-4" aria-labelledby="fb-set-basic">
        <h2 id="fb-set-basic" className="text-sm font-semibold">
          {t('plugin_formbuilder.settings.basic')}
        </h2>
        <Field id="fb-set-name" label={t('plugin_formbuilder.fields.name')}>
          <Input id="fb-set-name" value={def.name} maxLength={100} aria-invalid={!def.name.trim() || undefined} onChange={(e) => onDef({ ...def, name: e.target.value })} />
        </Field>
        <Field id="fb-set-desc" label={t('plugin_formbuilder.fields.description')}>
          <Textarea id="fb-set-desc" rows={3} value={def.description ?? ''} maxLength={500} onChange={(e) => onDef({ ...def, description: e.target.value })} />
        </Field>
        <Field id="fb-set-msg" label={t('plugin_formbuilder.settings.successMessage')} hint={t('plugin_formbuilder.settings.successMessageHint')}>
          <Textarea id="fb-set-msg" rows={2} value={s.successMessage ?? ''} maxLength={500} onChange={(e) => setS({ successMessage: e.target.value })} />
        </Field>
      </section>

      <section className="flex flex-col gap-4 rounded-lg border border-border bg-card p-4" aria-labelledby="fb-set-rules">
        <h2 id="fb-set-rules" className="text-sm font-semibold">
          {t('plugin_formbuilder.settings.rules')}
        </h2>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <label htmlFor="fb-set-repeat" className="text-sm font-medium">
              {t('plugin_formbuilder.settings.allowRepeat')}
            </label>
            <p className="text-xs text-muted-foreground">{t('plugin_formbuilder.settings.allowRepeatHint')}</p>
          </div>
          <Switch id="fb-set-repeat" checked={s.allowRepeat} onCheckedChange={(v) => setS({ allowRepeat: v })} />
        </div>
        <Field id="fb-set-deadline" label={t('plugin_formbuilder.fields.deadline')} hint={t('plugin_formbuilder.settings.deadlineHint')}>
          <DateTimePicker id="fb-set-deadline" value={s.deadline ?? ''} onChange={(v) => setS({ deadline: v || null })} placeholder={t('plugin_formbuilder.settings.noDeadline')} />
        </Field>
        <Field id="fb-set-max" label={t('plugin_formbuilder.settings.maxSubmissions')} hint={t('plugin_formbuilder.settings.maxSubmissionsHint')}>
          <NumInput id="fb-set-max" min={0} max={MaxSubmissionCap} step={1} value={s.maxSubmissions} onChange={(v) => setS({ maxSubmissions: Math.trunc(v ?? 0) })} />
        </Field>
      </section>

      <section className="flex flex-col gap-4 rounded-lg border border-border bg-card p-4" aria-labelledby="fb-set-aud">
        <h2 id="fb-set-aud" className="text-sm font-semibold">
          {t('plugin_formbuilder.settings.audience')}
        </h2>
        <AudiencePicker value={audience} onChange={onAudience} />
      </section>

      <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4" aria-labelledby="fb-set-ver">
        <h2 id="fb-set-ver" className="text-sm font-semibold">
          {t('plugin_formbuilder.settings.versions')}
        </h2>
        {versions.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('plugin_formbuilder.settings.noVersions')}</p>
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {[...versions].reverse().map((v) => (
              <li key={v.version} className="flex items-center justify-between gap-2 py-1.5 text-sm">
                <span className="min-w-0">
                  <span className="num font-medium">v{v.version}</span>
                  <span className="ms-2 text-xs text-muted-foreground">
                    {t('plugin_formbuilder.settings.versionMeta', { n: v.fieldCount, time: formatTime(v.createdAt, i18n.language) })}
                  </span>
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_formbuilder.settings.exportVersion', { v: v.version })}
                  title={t('plugin_formbuilder.settings.exportVersion', { v: v.version })}
                  onClick={() => void downloadDefinition(formId, def.name, v.version).catch(() => undefined)}
                >
                  <Download aria-hidden="true" />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
