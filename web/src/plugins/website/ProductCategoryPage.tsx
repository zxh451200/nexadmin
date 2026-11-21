import { useCallback, useMemo, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
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
import { slugify } from './format'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { TreeTable } from '@/framework/components/TreeTable'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  translateContent,
  useDeleteProductCategory,
  useI18nSchema,
  useLanguages,
  useNavMaxDepth,
  useProductCategories,
  useReorderProductCategories,
  useSaveProductCategory,
  type ProductCategoryNode,
} from './api'

/** slug 的口径与后端 slugReg 一致：小写字母、数字与连字符，长度 1-64。 */
const slugReg = /^[a-z0-9][a-z0-9-]{0,63}$/

/** 后台类目树节点；parentId 为 "0" 表示顶级。 */
interface CatNode extends ProductCategoryNode {
  children?: CatNode[]
}

/** 平铺列表按 parentId 组树；父级缺失时按顶级处理，避免整条数据消失。 */
function buildCatTree(rows: ProductCategoryNode[]): CatNode[] {
  const byId = new Map<string, CatNode>()
  for (const row of rows) byId.set(row.id, { ...row })
  const roots: CatNode[] = []
  for (const node of byId.values()) {
    const parent = node.parentId && node.parentId !== '0' ? byId.get(node.parentId) : undefined
    if (parent) (parent.children ??= []).push(node)
    else roots.push(node)
  }
  return roots
}

function findCatNode(nodes: CatNode[], id: string): CatNode | undefined {
  for (const n of nodes) {
    if (n.id === id) return n
    const hit = findCatNode(n.children ?? [], id)
    if (hit) return hit
  }
  return undefined
}

/** 把节点从原位置摘下；返回值里 moved 是带子树的整个节点。 */
function detachCatNode(nodes: CatNode[], id: string): { rest: CatNode[]; moved?: CatNode } {
  let moved: CatNode | undefined
  const rest = nodes.flatMap((node) => {
    if (node.id === id) {
      moved = node
      return []
    }
    if (!node.children?.length) return [node]
    const inner = detachCatNode(node.children, id)
    if (inner.moved && !moved) moved = inner.moved
    return [{ ...node, children: inner.rest }]
  })
  return { rest, moved }
}

/** afterId 未给=追加到末尾；空串=插到最前；否则插到该同级之后 */
function insertAt(list: CatNode[], node: CatNode, parentId: string, afterId?: string): CatNode[] {
  const at = afterId === undefined ? list.length : afterId === '' ? 0 : list.findIndex((n) => n.id === afterId) + 1
  const copy = [...list]
  copy.splice(at < 0 ? list.length : at, 0, { ...node, parentId })
  return copy
}

function insertCatNode(nodes: CatNode[], parentId: string, node: CatNode, afterId?: string): CatNode[] {
  if (parentId === '0') return insertAt(nodes, node, '0', afterId)
  return nodes.map((n) => {
    if (n.id === parentId) return { ...n, children: insertAt(n.children ?? [], node, parentId, afterId) }
    return n.children?.length ? { ...n, children: insertCatNode(n.children, parentId, node, afterId) } : n
  })
}

function moveCatNode(nodes: CatNode[], id: string, parentId: string, afterId?: string): CatNode[] {
  const { rest, moved } = detachCatNode(nodes, id)
  return moved ? insertCatNode(rest, parentId, moved, afterId) : nodes
}

/** 按树的当前顺序重排 parentId / sort，用于整批提交 */
function flattenCatTree(nodes: CatNode[], parentId = '0', out: { id: string; parentId: string; sort: number }[] = []) {
  nodes.forEach((n, i) => {
    out.push({ id: n.id, parentId, sort: (i + 1) * 10 })
    if (n.children?.length) flattenCatTree(n.children, n.id, out)
  })
  return out
}

/** 子树层数（叶子 = 1），用于限制「上级类目」下拉里会超深的选择。 */
function catHeight(node: CatNode): number {
  return 1 + Math.max(0, ...(node.children ?? []).map(catHeight))
}

/**
 * 类目表单：标题按语言编辑；slug/上级/排序是组织维度，全语言共用一份。
 * 类目译文只改展示名，链接与层级不受影响。
 */
function CategoryFormBody({
  onOpenChange, editing, tree, defaultParentId,
}: {
  onOpenChange: (v: boolean) => void
  editing: ProductCategoryNode | null
  tree: CatNode[]
  defaultParentId: string
}) {
  const { t } = useTranslation()
  const save = useSaveProductCategory()
  const maxDepth = useNavMaxDepth()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [parentId, setParentId] = useState(defaultParentId)
  const [sort, setSort] = useState(editing ? String(editing.sort) : '0')
  // 行内校验提示：提交未过时点亮，行内小字按当前值定位（toast 仍是快捷反馈）
  const [showErrors, setShowErrors] = useState(false)
  const baseTitle = (lf.baseValues().title ?? '').trim()
  const titleMissing = !baseTitle
  const slugBad = slug.trim() !== '' && !slugReg.test(slug.trim())

  /** 上级候选：自己与自己的后代不能当上级（会成环），层级到顶的也不能再挂子级。 */
  const parentOptions = useMemo(() => {
    const blocked = new Set<string>()
    if (editing) {
      const collect = (node: CatNode) => {
        blocked.add(node.id)
        node.children?.forEach(collect)
      }
      const self = findCatNode(tree, editing.id)
      if (self) collect(self)
    }
    const walk = (nodes: CatNode[], depth: number): { id: string; label: string; depth: number }[] => {
      const out: { id: string; label: string; depth: number }[] = []
      for (const n of nodes) {
        if (!blocked.has(n.id) && depth + catHeight(n) <= maxDepth) {
          out.push({ id: n.id, label: n.title, depth })
        }
        out.push(...walk(n.children ?? [], depth + 1))
      }
      return out
    }
    return walk(tree, 1)
  }, [tree, editing, maxDepth])

  const submit = async () => {
    if (!baseTitle) {
      setShowErrors(true)
      toast.error(t('plugin_website.productCategory.required'))
      return
    }
    // 手填了 slug 才做本地格式校验；留空 = 服务端按标题走 pkg/slug 行业标准生成
    const finalSlug = slug.trim()
    if (finalSlug && !slugReg.test(finalSlug)) {
      setShowErrors(true)
      toast.error(t('plugin_website.productCategory.slugInvalid'))
      return
    }
    setShowErrors(false)
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          parentId,
          title: baseTitle,
          slug: finalSlug || slugify(baseTitle),
          sort: Number(sort) || 0,
          i18nJson: lf.serialize().i18nJson,
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.productCategory.edit') : t('plugin_website.productCategory.create')}
      side="right"
      className="space-y-3"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button disabled={save.isPending} onClick={() => void submit()}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <div className="flex items-center justify-between gap-2 pb-1">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      <div className="flex flex-col gap-3">
        <LocalizedField fieldKey="title" required />
        {showErrors && titleMissing ? <span className="text-xs text-destructive">{t('plugin_website.productCategory.required')}</span> : null}
        <SharedFieldShell labelKey="plugin_website.fields.slug">
          <Input
            value={slug}
            maxLength={64}
            disabled={sharedDisabled}
            aria-invalid={showErrors && slugBad}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={t('plugin_website.fields.slugAuto')}
            className="font-mono"
          />
          {showErrors && slugBad ? <span className="text-xs text-destructive">{t('plugin_website.productCategory.slugInvalid')}</span> : null}
          <span className="text-xs text-muted-foreground">/products?category={slug || '…'}</span>
        </SharedFieldShell>
        <SharedFieldShell labelKey="plugin_website.productCategory.parent">
          <Select value={parentId} onValueChange={setParentId} disabled={sharedDisabled}>
            <SelectTrigger className="w-full" aria-label={t('plugin_website.productCategory.parent')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="max-h-72">
              <SelectItem value="0">{t('plugin_website.productCategory.parentRoot')}</SelectItem>
              {parentOptions.map((opt) => (
                <SelectItem key={opt.id} value={opt.id}>
                  <span style={{ paddingInlineStart: `${(opt.depth - 1) * 16}px` }}>{opt.label}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SharedFieldShell>
        <SharedFieldShell labelKey="plugin_website.fields.sort">
          <Input type="number" value={sort} disabled={sharedDisabled} onChange={(e) => setSort(e.target.value)} placeholder="0" />
        </SharedFieldShell>
        <LocaleFormActions />
      </div>
    </ResponsiveDialog>
  )
}

function CategoryFormDialog({
  onOpenChange, editing, tree, defaultParentId,
}: {
  onOpenChange: (v: boolean) => void
  editing: ProductCategoryNode | null
  tree: CatNode[]
  defaultParentId: string
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('product-category')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('product-category', texts, defaultLocale, to, format),
    [defaultLocale],
  )

  if (!schema || !defaultLocale) {
    return <EditorSchemaGate onOpenChange={onOpenChange} error={isError} onRetry={() => void refetch()} />
  }

  return (
    <LocaleFormProvider
      schema={schema}
      locales={languages}
      defaultLocale={defaultLocale}
      base={{ title: editing?.title ?? '' }}
      i18nJson={editing?.i18nJson}
      translate={translate}
    >
      <CategoryFormBody onOpenChange={onOpenChange} editing={editing} tree={tree} defaultParentId={defaultParentId} />
    </LocaleFormProvider>
  )
}

export default function ProductCategoryPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { has } = usePermission()
  // 预取翻译表单的字段元数据，点新增/编辑时不再闪空表单。
  useI18nSchema('product-category')
  const { data: categories = [], isLoading } = useProductCategories()
  const remove = useDeleteProductCategory()
  const reorder = useReorderProductCategories()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<ProductCategoryNode | null>(null)
  const [parentId, setParentId] = useState('0')
  const [selected, setSelected] = useState<string[]>([])

  const tree = useMemo(() => buildCatTree(categories), [categories])
  // 行深度：已到层级上限的行不能再挂子级（后端会拒），「新增子类目」按钮按深度隐藏。
  const maxDepth = useNavMaxDepth()
  const depths = useMemo(() => {
    const out = new Map<string, number>()
    const walk = (nodes: CatNode[], level: number) => {
      for (const n of nodes) {
        out.set(n.id, level)
        walk(n.children ?? [], level + 1)
      }
    }
    walk(tree, 1)
    return out
  }, [tree])

  const openCreate = (pid = '0') => {
    setEditing(null)
    setParentId(pid)
    setFormOpen(true)
  }
  const openEdit = (row: ProductCategoryNode) => {
    setEditing(row)
    setParentId(row.parentId || '0')
    setFormOpen(true)
  }

  const onDelete = async (row: CatNode) => {
    if (
      await confirm({
        destructive: true,
        description: t('plugin_website.productCategory.deleteConfirm', { name: row.title, count: row.productCount }),
      })
    ) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 服务端原因由 http 拦截器统一弹出
      }
    }
  }

  // 批量删除：TreeTable 勾父带子，selected 就是「会被删掉的那些行」（与后端级联删除同口径）。
  const onDeleteBatch = async (ids: string[]) => {
    if (await confirm({ destructive: true, description: t('plugin_website.productCategory.deleteBatchConfirm', { count: ids.length }) })) {
      try {
        for (const id of ids) {
          await remove.mutateAsync(id)
        }
        toast.success(t('common.operateSuccess'))
        setSelected([])
      } catch {
        // 服务端原因由 http 拦截器统一弹出
      }
    }
  }

  const columns: Column<CatNode>[] = [
    {
      key: 'title',
      title: t('plugin_website.productCategory.nameColumn'),
      mobile: 'title',
      render: (r) => (
        <span className="block max-w-[18rem] truncate font-medium text-foreground" title={r.title}>
          {r.title}
        </span>
      ),
    },
    {
      key: 'i18n',
      title: t('plugin_website.i18n.status'),
      render: (r) => <TranslationStatus value={r.i18nJson} fields={['title']} />,
    },
    {
      key: 'slug',
      title: t('plugin_website.fields.slug'),
      mobile: 'sub',
      render: (r) => (
        <span className="block max-w-48 truncate font-mono text-xs" title={r.slug}>
          {r.slug}
        </span>
      ),
    },
    {
      key: 'count',
      title: t('plugin_website.productCategory.countColumn'),
      align: 'end',
      render: (r) => <span className="num text-muted-foreground">{r.productCount}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.productCategory.title')} description={t('plugin_website.productCategory.desc', { max: maxDepth })} />
      <TreeTable<CatNode>
        columns={columns}
        data={tree}
        loading={isLoading}
        defaultExpandAll
        maxDepth={maxDepth}
        columnSettingsKey="plugin-website-product-categories"
        emptyText={t('plugin_website.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="website:product-category">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        // 同层拖动后整组重排；失败时 TreeTable 回滚乐观数据并 toast
        onReorder={
          has('website:product-category')
            ? (pid, ids) => reorder.mutateAsync(ids.map((id, i) => ({ id, parentId: pid ?? '0', sort: (i + 1) * 10 })))
            : undefined
        }
        // 跨级拖动：整棵树重排后一次提交
        onMove={
          has('website:product-category')
            ? async (id, parentId, afterId) => {
                const next = moveCatNode(tree, id, parentId ?? '0', afterId)
                await reorder.mutateAsync(flattenCatTree(next))
              }
            : undefined
        }
        toolbar={
          <Can perm="website:product-category">
            <Button onClick={() => openCreate()}>
              <Plus aria-hidden="true" />
              {t('plugin_website.productCategory.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:product-category">
            {(depths.get(row.id) ?? 1) < maxDepth ? (
              <Button variant="ghost" size="icon" aria-label={t('plugin_website.productCategory.addChild')} onClick={() => openCreate(row.id)}>
                <Plus aria-hidden="true" />
              </Button>
            ) : null}
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openEdit(row)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && (
        <CategoryFormDialog
          key={`${editing?.id ?? 'new'}:${parentId}`}
          onOpenChange={setFormOpen}
          editing={editing}
          tree={tree}
          defaultParentId={parentId}
        />
      )}
    </PageContainer>
  )
}
