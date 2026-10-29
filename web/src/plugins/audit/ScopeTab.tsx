import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { useConfig, useSaveConfig, type AuditConfig } from './api'
import { TableMultiSelect } from './TableMultiSelect'

function ScopeForm({ config }: { config: AuditConfig }) {
  const { t } = useTranslation()
  const save = useSaveConfig()
  const [tables, setTables] = useState<string[]>(config.tables)
  const dirty = tables.join(',') !== config.tables.join(',')

  const submit = async () => {
    try {
      await save.mutateAsync(tables)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <Card className="flex flex-col gap-4 p-4 md:p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-foreground" id="audit-scope-label">
          {t('plugin_audit.scope.title')}
        </h2>
        <p className="text-sm text-muted-foreground">{t('plugin_audit.scope.description')}</p>
      </div>
      <TableMultiSelect options={config.available} value={tables} onChange={setTables} aria-labelledby="audit-scope-label" />
      {tables.length === 0 ? <p className="text-sm text-warning">{t('plugin_audit.scope.noneSelected')}</p> : null}
      <p className="text-xs text-muted-foreground">{t('plugin_audit.scope.note')}</p>
      <div>
        <Button type="button" disabled={!dirty || save.isPending} onClick={() => void submit()}>
          {save.isPending ? t('common.loading') : t('common.save')}
        </Button>
      </div>
    </Card>
  )
}

export function ScopeTab() {
  const { t } = useTranslation()
  const { data, isLoading } = useConfig()
  if (isLoading || !data) return <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
  // key：服务端保存后范围变化时重置表单草稿
  return <ScopeForm key={data.tables.join(',')} config={data} />
}
