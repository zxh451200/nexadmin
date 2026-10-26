import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import TranslationsEditor from './TranslationsEditor'
import { FEATURE_ICONS, useDeleteFeature, useFeatures, useSaveFeature, type FeatureItem } from './api'

function FeatureFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: FeatureItem | null
}) {
  const { t } = useTranslation()
  const save = useSaveFeature()
  const [icon, setIcon] = useState(editing?.icon ?? 'building')
  const [title, setTitle] = useState(editing?.title ?? '')
  const [description, setDescription] = useState(editing?.description ?? '')
  const [sort, setSort] = useState(editing?.sort ?? 0)
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [i18nJson, setI18nJson] = useState(editing?.i18nJson ?? '')

  const submit = async () => {
    if (!title.trim()) {
      toast.error(t('plugin_website.feature.required'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: { icon, title, description, sort, enabled, i18nJson } })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{editing ? t('plugin_website.feature.edit') : t('plugin_website.feature.create')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.featureTitle')}
            <Input value={title} onChange={(e) => setTitle(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.featureDesc')}
            <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.featureIcon')}
            <select
              className="h-9 rounded-md border border-input bg-transparent px-3 text-sm"
              value={icon}
              onChange={(e) => setIcon(e.target.value)}
              aria-label={t('plugin_website.fields.featureIcon')}
            >
              {FEATURE_ICONS.map((name) => (
                <option key={name} value={name}>{name}</option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.sort')}
            <Input type="number" value={sort} onChange={(e) => setSort(Number(e.target.value) || 0)} />
          </label>
          <label className="flex items-center justify-between text-sm">
            {t('plugin_website.fields.enabled')}
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </label>
          <TranslationsEditor
            value={i18nJson}
            onChange={setI18nJson}
            fields={[
              { key: 'title', label: t('plugin_website.fields.featureTitle') },
              { key: 'description', label: t('plugin_website.fields.featureDesc'), multiline: true },
            ]}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function FeaturePage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data: features = [], isLoading } = useFeatures()
  const remove = useDeleteFeature()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<FeatureItem | null>(null)

  const onDelete = async (row: FeatureItem) => {
    if (await confirm({ destructive: true, description: t('plugin_website.feature.deleteConfirm', { name: row.title }) })) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<FeatureItem>[] = [
    { key: 'title', title: t('plugin_website.fields.featureTitle'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.title}</span> },
    { key: 'description', title: t('plugin_website.fields.featureDesc'), mobile: 'sub', render: (r) => <span className="line-clamp-1 text-muted-foreground">{r.description || '-'}</span> },
    { key: 'icon', title: t('plugin_website.fields.featureIcon'), render: (r) => <span className="font-mono text-xs">{r.icon || '-'}</span> },
    { key: 'enabled', title: t('plugin_website.fields.enabled'), render: (r) => (r.enabled ? <Badge variant="success">{t('plugin_website.enabledOn')}</Badge> : <Badge variant="outline">{t('plugin_website.enabledOff')}</Badge>) },
    { key: 'sort', title: t('plugin_website.fields.sort'), align: 'end', render: (r) => <span className="num">{r.sort}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.feature.title')} description={t('plugin_website.feature.desc')} />
      <DataTable
        columns={columns}
        data={features}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-features"
        emptyText={t('plugin_website.empty')}
        toolbar={
          <Can perm="website:feature">
            <Button onClick={() => { setEditing(null); setFormOpen(true) }}>
              <Plus aria-hidden="true" />
              {t('plugin_website.feature.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:feature">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => { setEditing(row); setFormOpen(true) }}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && <FeatureFormDialog key={editing?.id ?? 'new'} onOpenChange={setFormOpen} editing={editing} />}
    </PageContainer>
  )
}
