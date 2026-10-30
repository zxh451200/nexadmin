import { useCallback, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import {
  LocaleFormActions,
  LocaleFormProvider,
  LocalizedField,
  LocaleSwitcher,
  SharedFieldShell,
  useLocaleForm,
  useSharedField,
  type TranslateBatch,
} from '@/framework/components/i18n'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { OptionList } from '@/framework/components/pickers/OptionList'
import { PickerShell } from '@/framework/components/pickers/PickerShell'
import { PickerTrigger } from '@/framework/components/pickers/PickerTrigger'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  translateContent,
  useDeleteFeature,
  useFeatureIcons,
  useFeatures,
  useI18nSchema,
  useLanguages,
  useSaveFeature,
  type FeatureIcon,
  type FeatureItem,
} from './api'

/** 官网模板的线性图标：路径由后端下发，渲染参数与 template icon 函数保持一致。 */
function FeatureIconSvg({ paths, className }: { paths: string; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
      dangerouslySetInnerHTML={{ __html: paths }}
    />
  )
}

function FeatureIconSelect({
  value, onChange, icons, loading, ariaLabel, disabled,
}: {
  value: string
  onChange: (v: string) => void
  icons: FeatureIcon[]
  loading?: boolean
  ariaLabel: string
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const current = icons.find((i) => i.name === value)
  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      title={ariaLabel}
      contentClassName="w-64"
      trigger={
        <PickerTrigger
          aria-label={ariaLabel}
          hasValue={!!value}
          display={value}
          disabled={disabled}
          icon={current ? <FeatureIconSvg paths={current.paths} className="size-4 shrink-0" /> : undefined}
        />
      }
    >
      <OptionList
        className="w-full"
        options={icons.map((i) => ({
          value: i.name,
          label: i.name,
          node: (
            <span className="inline-flex items-center gap-2">
              <FeatureIconSvg paths={i.paths} className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate">{i.name}</span>
            </span>
          ),
        }))}
        value={[value]}
        loading={loading}
        onSelect={(v) => {
          onChange(v)
          setOpen(false)
        }}
      />
    </PickerShell>
  )
}

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * title/description 归 LocaleFormProvider，icon/sort/enabled 是共用字段，留在本地 state。
 * 弹窗外壳也由本组件渲染：底部按钮要用这里的 submit 与 state，必须留在 Provider 内。
 */
function FeatureFormBody({ onOpenChange, editing }: { onOpenChange: (v: boolean) => void; editing: FeatureItem | null }) {
  const { t } = useTranslation()
  const save = useSaveFeature()
  const { data: icons = [], isLoading: iconsLoading } = useFeatureIcons()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [icon, setIcon] = useState(editing?.icon ?? 'building')
  const [sort, setSort] = useState(editing?.sort ?? 0)
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)

  const submit = async () => {
    const base = lf.baseValues()
    if (!(base.title ?? '').trim()) {
      toast.error(t('plugin_website.feature.required'))
      return
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          icon,
          sort,
          enabled,
          // 行上的裸列永远存原文；feature 不可按语言发布，只回传 i18nJson
          title: base.title ?? '',
          description: base.description ?? '',
          i18nJson: lf.serialize().i18nJson,
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.feature.edit') : t('plugin_website.feature.create')}
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex items-center justify-between gap-2 pb-1">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      <div className="flex flex-col gap-3">
        <LocalizedField fieldKey="title" required />
        <LocalizedField fieldKey="description" />
        <SharedFieldShell labelKey="plugin_website.fields.featureIcon">
          <FeatureIconSelect
            ariaLabel={t('plugin_website.fields.featureIcon')}
            value={icon}
            onChange={setIcon}
            icons={icons}
            loading={iconsLoading}
            disabled={sharedDisabled}
          />
        </SharedFieldShell>
        <SharedFieldShell labelKey="plugin_website.fields.sort">
          <Input
            type="number"
            value={sort}
            disabled={sharedDisabled}
            onChange={(e) => setSort(Number(e.target.value) || 0)}
          />
        </SharedFieldShell>
        <SharedFieldShell labelKey="plugin_website.fields.enabled" className="flex-row items-center justify-between">
          <Switch checked={enabled} disabled={sharedDisabled} onCheckedChange={setEnabled} />
        </SharedFieldShell>
        <LocaleFormActions />
      </div>
    </ResponsiveDialog>
  )
}

function FeatureFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: FeatureItem | null
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('feature')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('feature', texts, defaultLocale, to, format),
    [defaultLocale],
  )

  // schema 与语言清单是表单的前提，没到货前不给渲染——否则会闪出一个没有字段的空表单。
  if (!schema || !defaultLocale) {
    return <EditorSchemaGate onOpenChange={onOpenChange} error={isError} onRetry={() => void refetch()} />
  }

  return (
    <LocaleFormProvider
      schema={schema}
      locales={languages}
      defaultLocale={defaultLocale}
      base={{ title: editing?.title ?? '', description: editing?.description ?? '' }}
      i18nJson={editing?.i18nJson}
      translate={translate}
    >
      <FeatureFormBody onOpenChange={onOpenChange} editing={editing} />
    </LocaleFormProvider>
  )
}

export default function FeaturePage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('feature')
  const { data: features = [], isLoading } = useFeatures()
  const { data: icons = [] } = useFeatureIcons()
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
    { key: 'icon', title: t('plugin_website.fields.featureIcon'), render: (r) => {
      const paths = icons.find((i) => i.name === r.icon)?.paths
      return (
        <span className="inline-flex items-center gap-2">
          {paths && <FeatureIconSvg paths={paths} className="size-4 shrink-0 text-muted-foreground" />}
          <span className="font-mono text-xs">{r.icon || '-'}</span>
        </span>
      )
    } },
    { key: 'i18n', title: t('plugin_website.i18n.status'), render: (r) => <TranslationStatus value={r.i18nJson} fields={['title', 'description']} /> },
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
