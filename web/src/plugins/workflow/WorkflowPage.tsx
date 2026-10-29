import { useState } from 'react'
import { CircleCheck, Pencil, Plus, Trash2, Undo2, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { cn } from '@/lib/utils'
import {
  useDefs,
  useDeleteDef,
  useInstance,
  useInstances,
  useSaveDef,
  useTemplates,
  useWfAction,
  type WfDef,
  type WfDefinition,
} from './api'
import { actionLabel, formatTime, statusVariant } from './format'
import { FlowDesigner } from './FlowDesigner'

// WorkflowPage 是工作流中心:待办 / 已办 / 全部实例 / 流程定义 四个页签。
// 流程定义用 FlowDesigner 可视化编辑(SVG 拖拽画布),支持从模板新建。

export default function WorkflowPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('todo')
  const [selected, setSelected] = useState<string | undefined>()

  return (
    <PageContainer>
      <PageHeader title={t('plugin_workflow.title')} description={t('plugin_workflow.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="todo">{t('plugin_workflow.tabs.todo')}</TabsTrigger>
          <TabsTrigger value="done">{t('plugin_workflow.tabs.done')}</TabsTrigger>
          <TabsTrigger value="all">{t('plugin_workflow.tabs.all')}</TabsTrigger>
          <Can perm="workflow:edit">
            <TabsTrigger value="defs">{t('plugin_workflow.tabs.defs')}</TabsTrigger>
          </Can>
        </TabsList>
        <TabsContent value="todo" className="m-0">
          <InstanceList todo="1" onSelect={setSelected} />
        </TabsContent>
        <TabsContent value="done" className="m-0">
          <InstanceList done="1" onSelect={setSelected} />
        </TabsContent>
        <TabsContent value="all" className="m-0">
          <InstanceList onSelect={setSelected} />
        </TabsContent>
        <Can perm="workflow:edit">
          <TabsContent value="defs" className="m-0">
            <DefList />
          </TabsContent>
        </Can>
      </Tabs>
      <InstanceDrawer id={selected} onClose={() => setSelected(undefined)} />
    </PageContainer>
  )
}

// ---- 实例列表 ----

function InstanceList({
  todo,
  done,
  onSelect,
}: {
  todo?: string
  done?: string
  onSelect: (id: string) => void
}) {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const instances = useInstances({ todo, done, page, pageSize })

  const rows = instances.data?.list ?? []
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {instances.isLoading && rows.length === 0 ? (
        <div className="p-4 text-center text-sm text-muted-foreground">{t('plugin_workflow.loading')}</div>
      ) : null}
      {rows.map((w) => (
        <button
          key={w.id}
          type="button"
          onClick={() => onSelect(w.id)}
          className={cn('flex w-full flex-col gap-1 border-b px-4 py-2.5 text-start text-sm last:border-b-0 hover:bg-accent')}
        >
          <div className="flex w-full items-center gap-2">
            <span className="min-w-0 flex-1 truncate font-medium text-foreground">{w.title || w.defKey}</span>
            <Badge variant={statusVariant(w.status)}>{t(`plugin_workflow.status.${w.status}`)}</Badge>
          </div>
          <div className="flex w-full items-center gap-2 text-xs text-muted-foreground">
            <span className="min-w-0 flex-1 truncate">
              {w.businessKey ? `${w.bizType}/${w.businessKey} · ` : ''}
              {w.current.join(', ')}
            </span>
            <span>{formatTime(w.updatedAt, i18n.language)}</span>
          </div>
        </button>
      ))}
      {!instances.isLoading && rows.length === 0 ? (
        <EmptyState title={t('plugin_workflow.empty')} className="py-8" />
      ) : null}
      {(instances.data?.total ?? 0) > (page * pageSize) || page > 1 ? (
        <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
          <span>{t('plugin_workflow.total', { count: instances.data?.total ?? 0 })}</span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              {t('plugin_workflow.prev')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={page * pageSize >= (instances.data?.total ?? 0)}
              onClick={() => setPage((p) => p + 1)}
            >
              {t('plugin_workflow.next')}
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

// ---- 流程定义(可视化编辑) ----

function DefList() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [editing, setEditing] = useState<WfDef | 'new' | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(50)

  const defs = useDefs({ page, pageSize })
  const remove = useDeleteDef()
  const templates = useTemplates()

  const onDelete = async (d: WfDef) => {
    if (await confirm({ destructive: true, description: t('plugin_workflow.defDeleteConfirm', { name: d.name }) })) {
      try {
        await remove.mutateAsync(d.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<WfDef>[] = [
    {
      key: 'name',
      title: t('plugin_workflow.fields.defName'),
      mobile: 'title',
      render: (d) => (
        <div>
          <span className="font-medium text-foreground">{d.name}</span>
          <div className="text-xs text-muted-foreground">{d.key} · v{d.version}</div>
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_workflow.fields.status'),
      render: (d) => (
        <Badge variant={d.status === 'published' ? 'success' : d.status === 'disabled' ? 'destructive' : 'secondary'}>
          {t(`plugin_workflow.defStatus.${d.status}`)}
        </Badge>
      ),
    },
    { key: 'updatedAt', title: t('plugin_workflow.fields.updatedAt'), render: (d) => <span className="num text-xs text-muted-foreground">{formatTime(d.updatedAt, i18n.language)}</span> },
  ]

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Can perm="workflow:edit">
          <Button onClick={() => setEditing('new')}>
            <Plus aria-hidden="true" />
            {t('plugin_workflow.defCreate')}
          </Button>
        </Can>
      </div>
      <DataTable
        columns={columns}
        data={defs.data?.list ?? []}
        rowKey="id"
        loading={defs.isLoading}
        columnSettingsKey="plugin-workflow-defs"
        pagination={{ page, pageSize, total: defs.data?.total ?? 0, onChange: setPage }}
        emptyText={t('plugin_workflow.empty')}
        rowActions={(d) => (
          <Can perm="workflow:edit">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => setEditing(d)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(d)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />

      {editing ? (
        <DefEditor
          def={editing === 'new' ? null : editing}
          templates={templates.data ?? []}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  )
}

// ---- 定义编辑器(全屏 Dialog + FlowDesigner) ----

function DefEditor({
  def,
  templates,
  onClose,
}: {
  def: WfDef | null
  templates: { key: string; name: string; definition: WfDefinition }[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const save = useSaveDef()
  const [name, setName] = useState(def?.name ?? '')
  const [key, setKey] = useState(def?.key ?? '')
  const [status, setStatus] = useState(def?.status ?? 'draft')
  const [templateKey, setTemplateKey] = useState('__blank')
  const [definition, setDefinition] = useState<WfDefinition>(
    def?.definition ?? templates[0]?.definition ?? { key: '', name: '', start: 'start', nodes: [], edges: [] },
  )

  const submit = async () => {
    if (!name.trim() || !key.trim()) {
      toast.error(t('plugin_workflow.defNameKeyRequired'))
      return
    }
    try {
      await save.mutateAsync({
        id: def?.id,
        data: { key: key.trim(), name: name.trim(), status, definition: { ...definition, key: key.trim(), name: name.trim() } },
      })
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={def ? t('plugin_workflow.defEdit') : t('plugin_workflow.defCreate')}
      size="xl"
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">
            {t('plugin_workflow.fields.defName')}
            <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
          </span>
          <Input required value={name} onChange={(e) => setName(e.target.value)} className="w-56" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">
            {t('plugin_workflow.fields.defKey')}
            <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
          </span>
          <Input required value={key} onChange={(e) => setKey(e.target.value)} disabled={!!def} className="w-44 font-mono" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">{t('plugin_workflow.fields.status')}</span>
          <SimpleSelect
            className="w-32"
            aria-label={t('plugin_workflow.fields.status')}
            value={status}
            onChange={(v) => setStatus(v as 'draft' | 'published' | 'disabled')}
            options={[
              { value: 'draft', label: t('plugin_workflow.defStatus.draft') },
              { value: 'published', label: t('plugin_workflow.defStatus.published') },
              { value: 'disabled', label: t('plugin_workflow.defStatus.disabled') },
            ]}
          />
        </label>
        {!def && templates.length > 0 ? (
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-xs text-muted-foreground">{t('plugin_workflow.fromTemplate')}</span>
            <SimpleSelect
              className="w-40"
              aria-label={t('plugin_workflow.fromTemplate')}
              value={templateKey}
              onChange={(v) => {
                setTemplateKey(v)
                const tpl = templates.find((tp) => tp.key === v)
                if (tpl) {
                  setKey(tpl.definition.key)
                  setName(tpl.name)
                  setDefinition(tpl.definition)
                }
              }}
              options={[
                { value: '__blank', label: t('plugin_workflow.fromTemplateBlank') },
                ...templates.map((tp) => ({ value: tp.key, label: tp.name })),
              ]}
            />
          </label>
        ) : null}
      </div>
      <FlowDesigner definition={definition} onChange={setDefinition} />
    </ResponsiveDialog>
  )
}

// ---- 实例详情抽屉(审批 + 流转历史) ----

function InstanceDrawer({ id, onClose }: { id?: string; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [comment, setComment] = useState('')
  const detail = useInstance(id)
  const action = useWfAction()
  const inst = detail.data

  const doAction = async (act: 'approve' | 'reject' | 'escalate' | 'withdraw', confirmMsg?: string) => {
    if (!inst) return
    if (confirmMsg && !(await confirm({ destructive: act === 'withdraw', description: confirmMsg }))) return
    try {
      await action.mutateAsync({ id: inst.id, action: act, body: { comment: comment.trim() } })
      toast.success(t('common.operateSuccess'))
      setComment('')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  if (!id) return null

  const isRunning = inst?.status === 'running'

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/40" onClick={onClose}>
      <div
        className="flex h-full w-full max-w-lg flex-col border-l bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium text-foreground">
              {inst?.title || inst?.defKey || '…'}
            </div>
            <div className="truncate text-xs text-muted-foreground">
              {inst ? `${t('plugin_workflow.submitter')}: ${inst.submitter}` : ''}
            </div>
          </div>
          {inst ? <Badge variant={statusVariant(inst.status)}>{t(`plugin_workflow.status.${inst.status}`)}</Badge> : null}
          <Button variant="ghost" size="icon" aria-label={t('common.close')} onClick={onClose}>
            <XCircle aria-hidden="true" />
          </Button>
        </div>

        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
          {inst?.history.map((e, i) => (
            <div key={i} className="flex gap-2 text-sm">
              <div className="mt-1 size-2 shrink-0 rounded-full bg-muted-foreground/40" />
              <div className="min-w-0 flex-1">
                <div className="text-foreground">
                  <span className="font-medium">{e.actor}</span>{' '}
                  <span className="text-muted-foreground">{actionLabel(e.action)}</span>
                  <span className="text-muted-foreground"> · {e.node}</span>
                </div>
                {e.comment ? <div className="text-xs text-muted-foreground">{e.comment}</div> : null}
                <div className="text-[11px] text-muted-foreground">{formatTime(e.at, i18n.language)}</div>
              </div>
            </div>
          ))}
          {!detail.isLoading && (inst?.history.length ?? 0) === 0 ? (
            <EmptyState title={t('plugin_workflow.empty')} className="py-8" />
          ) : null}
        </div>

        {inst && isRunning ? (
          <Can perm="workflow:approve">
            <div className="border-t p-3">
              <Input
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t('plugin_workflow.commentPlaceholder')}
                aria-label={t('plugin_workflow.commentPlaceholder')}
                className="mb-2"
              />
              <div className="flex flex-wrap gap-2">
                <Button size="sm" onClick={() => void doAction('approve')} disabled={action.isPending}>
                  <CircleCheck aria-hidden="true" />
                  {t('plugin_workflow.approve')}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive"
                  onClick={() => void doAction('reject')}
                  disabled={action.isPending}
                >
                  <XCircle aria-hidden="true" />
                  {t('plugin_workflow.reject')}
                </Button>
                <Button size="sm" variant="outline" onClick={() => void doAction('escalate')} disabled={action.isPending}>
                  <Undo2 aria-hidden="true" />
                  {t('plugin_workflow.escalate')}
                </Button>
              </div>
            </div>
          </Can>
        ) : null}
      </div>
    </div>
  )
}
