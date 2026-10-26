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
import TranslationsEditor from './TranslationsEditor'
import { useDeleteNav, useNavs, useSaveNav, type NavItem } from './api'

function NavFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: NavItem | null
}) {
  const { t } = useTranslation()
  const save = useSaveNav()
  const [label, setLabel] = useState(editing?.label ?? '')
  const [href, setHref] = useState(editing?.href ?? '')
  const [sort, setSort] = useState(editing?.sort ?? 0)
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [i18nJson, setI18nJson] = useState(editing?.i18nJson ?? '')

  const submit = async () => {
    if (!label.trim() || !href.trim()) {
      toast.error(t('plugin_website.nav.required'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: { label, href, sort, enabled, i18nJson } })
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
          <DialogTitle>{editing ? t('plugin_website.nav.edit') : t('plugin_website.nav.create')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.navLabel')}
            <Input value={label} onChange={(e) => setLabel(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.navHref')}
            <Input value={href} onChange={(e) => setHref(e.target.value)} placeholder="#features / /news / https://…" />
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
            fields={[{ key: 'label', label: t('plugin_website.fields.navLabel') }]}
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

export default function NavPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data: navs = [], isLoading } = useNavs()
  const remove = useDeleteNav()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<NavItem | null>(null)

  const onDelete = async (row: NavItem) => {
    if (await confirm({ destructive: true, description: t('plugin_website.nav.deleteConfirm', { name: row.label }) })) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<NavItem>[] = [
    { key: 'label', title: t('plugin_website.fields.navLabel'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.label}</span> },
    { key: 'href', title: t('plugin_website.fields.navHref'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">{r.href}</span> },
    { key: 'enabled', title: t('plugin_website.fields.enabled'), render: (r) => (r.enabled ? <Badge variant="success">{t('plugin_website.enabledOn')}</Badge> : <Badge variant="outline">{t('plugin_website.enabledOff')}</Badge>) },
    { key: 'sort', title: t('plugin_website.fields.sort'), align: 'end', render: (r) => <span className="num">{r.sort}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.nav.title')} description={t('plugin_website.nav.desc')} />
      <DataTable
        columns={columns}
        data={navs}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-navs"
        emptyText={t('plugin_website.empty')}
        toolbar={
          <Can perm="website:nav">
            <Button onClick={() => { setEditing(null); setFormOpen(true) }}>
              <Plus aria-hidden="true" />
              {t('plugin_website.nav.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:nav">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => { setEditing(row); setFormOpen(true) }}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && <NavFormDialog key={editing?.id ?? 'new'} onOpenChange={setFormOpen} editing={editing} />}
    </PageContainer>
  )
}
