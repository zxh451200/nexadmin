import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, Save, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { arrayMove } from '@dnd-kit/sortable'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { usePublish, useSaveForm, useFormDetail, type Audience, type FieldType, type FormDefinition, type FormField } from './api'
import { DesignerPreview } from './DesignerPreview'
import { FieldCanvas } from './FieldCanvas'
import { FieldPalette } from './FieldPalette'
import { FieldProps } from './FieldProps'
import { MaxFields } from './limits'
import { fieldProblems, hasValue, newField, nextKey, pruneConds, referenceable, renameFieldKey } from './schema'
import { SettingsPanel } from './SettingsPanel'
import { useResolvedFields } from './useResolvedFields'

const EMPTY_FIELDS: FormField[] = []
const EMPTY_AUDIENCE: Audience = { roles: [], users: [] }

/** 服务端总会返回 settings，但导入的旧文件可能缺字段：补齐默认值，避免受控组件在 undefined 上出错 */
function normalize(def: FormDefinition): FormDefinition {
  return {
    ...def,
    fields: def.fields ?? [],
    settings: {
      ...def.settings,
      allowRepeat: def.settings?.allowRepeat ?? false,
      maxSubmissions: def.settings?.maxSubmissions ?? 0,
    },
  }
}

export default function DesignerPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const [params] = useSearchParams()
  const id = params.get('id') ?? undefined
  const { data, isLoading, isError } = useFormDetail(id)
  const save = useSaveForm()
  const publish = usePublish()

  const [def, setDef] = useState<FormDefinition | null>(null)
  const [audience, setAudience] = useState<Audience>(EMPTY_AUDIENCE)
  const [dirty, setDirty] = useState(false)
  const [selected, setSelected] = useState(-1)
  const [tab, setTab] = useState('design')
  const loadedId = useRef<string>()
  const propsRef = useRef<HTMLDivElement>(null)

  // 只在首次拿到某个表单时灌入本地状态；之后以本地编辑为准（保存成功也不覆盖，避免打断正在进行的编辑）
  useEffect(() => {
    if (data && loadedId.current !== data.id) {
      loadedId.current = data.id
      setDef(normalize(data.definition))
      setAudience(data.audience ?? EMPTY_AUDIENCE)
    }
  }, [data])

  useEffect(() => {
    if (!dirty) return
    const onUnload = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', onUnload)
    return () => window.removeEventListener('beforeunload', onUnload)
  }, [dirty])

  const fields = def?.fields ?? EMPTY_FIELDS
  const resolved = useResolvedFields(fields)

  const editDef = (next: FormDefinition) => {
    setDef(next)
    setDirty(true)
  }
  const setFields = (list: FormField[], opts?: { prune?: boolean }) => {
    if (!def) return
    let out = list
    if (opts?.prune) {
      const r = pruneConds(list)
      out = r.fields
      if (r.cleaned > 0) toast.info(t('plugin_formbuilder.designer.condsCleaned', { n: r.cleaned }))
    }
    editDef({ ...def, fields: out })
  }

  const select = (i: number) => {
    setSelected(i)
    // 手机上属性面板在画布下方：选中后滚动到它
    if (window.matchMedia('(max-width: 1023px)').matches) setTimeout(() => propsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 0)
  }
  const add = (type: FieldType) => {
    if (fields.length >= MaxFields) return void toast.error(t('plugin_formbuilder.designer.tooMany', { n: MaxFields }))
    const at = selected >= 0 ? selected + 1 : fields.length
    const f = newField(type, fields, t(`plugin_formbuilder.types.${type}`))
    setFields([...fields.slice(0, at), f, ...fields.slice(at)])
    select(at)
  }
  const move = (from: number, to: number) => {
    if (to < 0 || to >= fields.length) return
    setFields(arrayMove(fields, from, to), { prune: true })
    setSelected(to)
  }
  const remove = (i: number) => {
    setFields(fields.filter((_, j) => j !== i), { prune: true })
    setSelected(-1)
  }
  const duplicate = (i: number) => {
    if (fields.length >= MaxFields) return void toast.error(t('plugin_formbuilder.designer.tooMany', { n: MaxFields }))
    const src = fields[i]
    const copy: FormField = { ...structuredClone(src), key: nextKey(fields), title: `${src.title} ${t('plugin_formbuilder.copySuffix')}`.slice(0, 200) }
    setFields([...fields.slice(0, i + 1), copy, ...fields.slice(i + 1)])
    select(i + 1)
  }
  const change = (i: number, next: FormField) => setFields(fields.map((f, j) => (j === i ? next : f)))

  const doSave = async (): Promise<boolean> => {
    if (!def || !id) return false
    try {
      await save.mutateAsync({ id, definition: def, audience })
      setDirty(false)
      return true
    } catch {
      return false // 服务端的校验提示已由请求层弹出
    }
  }
  const onSave = async () => {
    if (await doSave()) toast.success(t('plugin_formbuilder.designer.saved'))
  }
  const onPublish = async () => {
    if (!def || !id) return
    if (def.fields.length === 0) return void toast.error(t('plugin_formbuilder.designer.needField'))
    const bad = def.fields.findIndex((f) => fieldProblems(f).length > 0)
    if (bad >= 0) {
      setTab('design')
      select(bad)
      return void toast.error(t('plugin_formbuilder.designer.fixProblems'))
    }
    if (!(await confirm({ title: t('plugin_formbuilder.btn.publish'), description: t('plugin_formbuilder.publishConfirm', { name: def.name }) }))) return
    if (dirty && !(await doSave())) return
    try {
      await publish.mutateAsync(id)
      toast.success(t('plugin_formbuilder.designer.published'))
    } catch {
      /* 已提示 */
    }
  }
  const goBack = async () => {
    if (dirty && !(await confirm({ destructive: true, description: t('plugin_formbuilder.designer.discardConfirm') }))) return
    navigate('/system/formbuilder')
  }

  if (!id || isError) {
    return (
      <PageContainer>
        <EmptyState
          title={t('plugin_formbuilder.designer.notFound')}
          action={
            <Button variant="outline" onClick={() => navigate('/system/formbuilder')}>
              <ArrowLeft aria-hidden="true" />
              {t('common.back')}
            </Button>
          }
        />
      </PageContainer>
    )
  }
  if (isLoading || !def) {
    return (
      <PageContainer>
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-96 w-full" />
      </PageContainer>
    )
  }

  const sel = selected >= 0 && selected < fields.length ? selected : -1
  const status = data?.status ?? 'draft'
  const busy = save.isPending || publish.isPending

  return (
    <PageContainer>
      <PageHeader
        title={def.name || t('plugin_formbuilder.designer.untitled')}
        description={
          <>
            {t(`plugin_formbuilder.status.${status}`)}
            {data && data.publishedVersion > 0 ? ` · v${data.publishedVersion}` : ''}
            {dirty ? ` · ${t('plugin_formbuilder.designer.unsaved')}` : ''}
          </>
        }
        actions={
          <>
            <Button variant="outline" onClick={() => void goBack()}>
              <ArrowLeft aria-hidden="true" />
              {t('common.back')}
            </Button>
            <Button variant="outline" disabled={busy || !dirty} onClick={() => void onSave()}>
              <Save aria-hidden="true" />
              {t('plugin_formbuilder.designer.saveDraft')}
            </Button>
            <Can perm="formbuilder:publish">
              <Button disabled={busy} onClick={() => void onPublish()}>
                <Send aria-hidden="true" />
                {t('plugin_formbuilder.btn.publish')}
              </Button>
            </Can>
          </>
        }
      />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList className="w-fit">
          <TabsTrigger value="design">{t('plugin_formbuilder.designer.tabDesign')}</TabsTrigger>
          <TabsTrigger value="preview">{t('plugin_formbuilder.designer.tabPreview')}</TabsTrigger>
          <TabsTrigger value="settings">{t('plugin_formbuilder.designer.tabSettings')}</TabsTrigger>
        </TabsList>

        <TabsContent value="design" className="m-0">
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-[12rem_minmax(0,1fr)_22rem]">
            <FieldPalette onAdd={add} />
            <section aria-label={t('plugin_formbuilder.designer.canvas')} className="min-w-0">
              <FieldCanvas fields={fields} selected={sel} onSelect={select} onMove={move} />
            </section>
            <div ref={propsRef} className="scroll-mt-4 rounded-lg border border-border bg-card p-4 lg:sticky lg:top-4 lg:max-h-[calc(100dvh-6rem)] lg:self-start lg:overflow-y-auto">
              {sel >= 0 ? (
                <FieldProps
                  key={sel}
                  field={fields[sel]}
                  index={sel}
                  total={fields.length}
                  refs={referenceable(resolved, sel).filter(hasValue)}
                  usedKeys={fields.map((f) => f.key)}
                  onChange={(next) => change(sel, next)}
                  onRename={(key) => setFields(renameFieldKey(fields, sel, key))}
                  onMove={(d) => move(sel, sel + d)}
                  onDuplicate={() => duplicate(sel)}
                  onRemove={() => remove(sel)}
                />
              ) : (
                <p className="text-sm text-muted-foreground">{t('plugin_formbuilder.designer.selectHint')}</p>
              )}
            </div>
          </div>
        </TabsContent>

        <TabsContent value="preview" className="m-0">
          <DesignerPreview def={{ ...def, fields: resolved }} />
        </TabsContent>

        <TabsContent value="settings" className="m-0">
          <SettingsPanel
            formId={id}
            def={def}
            audience={audience}
            onDef={editDef}
            onAudience={(a) => {
              setAudience(a)
              setDirty(true)
            }}
          />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
