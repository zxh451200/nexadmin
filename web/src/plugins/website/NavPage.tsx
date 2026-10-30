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
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { SimpleSelect, type SimpleSelectOption } from '@/framework/components/SimpleSelect'
import { TreeTable } from '@/framework/components/TreeTable'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  translateContent,
  useDeleteNav,
  useI18nSchema,
  useLanguages,
  useNavs,
  usePages,
  useReorderNavs,
  useSaveNav,
  type NavItem,
  type SitePage,
} from './api'

/** 后台导航树节点；parentId 为 "0" 表示顶级。 */
interface NavNode extends NavItem {
  children?: NavNode[]
}

const NAV_MAX_DEPTH = 3

/** 「自定义链接」占位值：选中时展示输入框，提交时用输入值代替。 */
const CUSTOM_HREF = '__custom__'

/** 平铺列表按 parentId 组树；父级缺失时按顶级处理，避免整条数据消失。 */
function buildNavTree(rows: NavItem[]): NavNode[] {
  const byId = new Map<string, NavNode>()
  for (const row of rows) byId.set(row.id, { ...row })
  const roots: NavNode[] = []
  for (const node of byId.values()) {
    const parent = node.parentId && node.parentId !== '0' ? byId.get(node.parentId) : undefined
    if (parent) (parent.children ??= []).push(node)
    else roots.push(node)
  }
  return roots
}

function findNavNode(nodes: NavNode[], id: string): NavNode | undefined {
  for (const n of nodes) {
    if (n.id === id) return n
    const hit = findNavNode(n.children ?? [], id)
    if (hit) return hit
  }
  return undefined
}

/** 把节点从原位置摘下；返回值里 moved 是带子树的整个节点。 */
function detachNavNode(nodes: NavNode[], id: string): { rest: NavNode[]; moved?: NavNode } {
  let moved: NavNode | undefined
  const rest = nodes.flatMap((node) => {
    if (node.id === id) {
      moved = node
      return []
    }
    if (!node.children?.length) return [node]
    const inner = detachNavNode(node.children, id)
    if (inner.moved && !moved) moved = inner.moved
    return [{ ...node, children: inner.rest }]
  })
  return { rest, moved }
}

/** afterId 未给=追加到末尾；空串=插到最前；否则插到该同级之后 */
function insertAt(list: NavNode[], node: NavNode, parentId: string, afterId?: string): NavNode[] {
  const at = afterId === undefined ? list.length : afterId === '' ? 0 : list.findIndex((n) => n.id === afterId) + 1
  const copy = [...list]
  copy.splice(at < 0 ? list.length : at, 0, { ...node, parentId })
  return copy
}

function insertNavNode(nodes: NavNode[], parentId: string, node: NavNode, afterId?: string): NavNode[] {
  if (parentId === '0') return insertAt(nodes, node, '0', afterId)
  return nodes.map((n) => {
    if (n.id === parentId) return { ...n, children: insertAt(n.children ?? [], node, parentId, afterId) }
    return n.children?.length ? { ...n, children: insertNavNode(n.children, parentId, node, afterId) } : n
  })
}

function moveNavNode(nodes: NavNode[], id: string, parentId: string, afterId?: string): NavNode[] {
  const { rest, moved } = detachNavNode(nodes, id)
  return moved ? insertNavNode(rest, parentId, moved, afterId) : nodes
}

/** 按树的当前顺序重排 parentId / sort，用于整批提交 */
function flattenNavTree(nodes: NavNode[], parentId = '0', out: { id: string; parentId: string; sort: number }[] = []) {
  nodes.forEach((n, i) => {
    out.push({ id: n.id, parentId, sort: (i + 1) * 10 })
    if (n.children?.length) flattenNavTree(n.children, n.id, out)
  })
  return out
}

/** 子树层数（叶子 = 1），用于限制「上级导航」下拉里会超深的选择。 */
function navHeight(node: NavNode): number {
  return 1 + Math.max(0, ...(node.children ?? []).map(navHeight))
}

function collectNavIds(node: NavNode, out: Set<string>): void {
  out.add(node.id)
  for (const child of node.children ?? []) collectNavIds(child, out)
}

function countNavs(nodes: NavNode[]): number {
  return nodes.reduce((n, child) => n + 1 + countNavs(child.children ?? []), 0)
}

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * label 归 LocaleFormProvider；href/上级/排序/启用是共用字段，留在本地 state，译文态下置灰。
 * 弹窗外壳也由本组件渲染：底部按钮要用这里的 submit 与 state，必须留在 Provider 内。
 */
function NavFormBody({
  onOpenChange, editing, tree, defaultParentId, pages,
}: {
  onOpenChange: (v: boolean) => void
  editing: NavItem | null
  tree: NavNode[]
  defaultParentId: string
  pages: SitePage[]
}) {
  const { t } = useTranslation()
  const save = useSaveNav()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [href, setHref] = useState(editing?.href ?? '')

  // 可由下拉直选的站内目标；其余（外部链接、锚点、详情页）走「自定义链接」。
  const presets = useMemo<SimpleSelectOption[]>(() => {
    const out: SimpleSelectOption[] = [
      { value: '/', label: t('plugin_website.nav.hrefHome') },
      { value: '/news', label: t('plugin_website.nav.hrefNews') },
      { value: '/plugins', label: t('plugin_website.nav.hrefPlugins') },
      { value: '/docs', label: t('plugin_website.nav.hrefDocs') },
    ]
    for (const page of pages) {
      out.push({ value: `/p/${page.slug}`, label: `${t('plugin_website.nav.hrefPage')} · ${page.title}` })
    }
    return out
  }, [pages, t])
  // 新建时默认指向首页；编辑时命中预设就选中，否则落到「自定义链接」。
  const [target, setTarget] = useState(() => (href === '' ? '/' : presets.some((o) => o.value === href) ? href : CUSTOM_HREF))
  const [hrefError, setHrefError] = useState<{ title: string; hints: string[] } | null>(null)

  /** 与服务端 allowlist 对齐；额外检查 /p/{slug} 是否已在页面管理里存在。 */
  const checkHref = (value: string): { title: string; hints: string[] } | null => {
    const v = value.trim()
    if (!v || v.startsWith('#') || /^(https?:\/\/|mailto:|tel:)/i.test(v)) return null
    if (!v.startsWith('/')) {
      return { title: t('plugin_website.nav.errNoSlash'), hints: [t('plugin_website.nav.errNoSlashHint')] }
    }
    const path = v.split(/[?#]/)[0]
    const known = (x: string) =>
      x === '/' || ['/news', '/plugins', '/docs'].some((r) => x === r || x.startsWith(`${r}/`)) || (x.startsWith('/p/') && x.length > 3)
    if (known(path)) {
      if (path.startsWith('/p/')) {
        const slug = path.slice(3)
        if (!pages.some((page) => page.slug === slug)) {
          return { title: t('plugin_website.nav.errPageMissing', { slug }), hints: [t('plugin_website.nav.errPageMissingHint')] }
        }
      }
      return null
    }
    const langRest = /^\/[a-z]{2}(?:-[A-Za-z]{2,})?(\/.*)$/.exec(path)
    if (langRest && known(langRest[1])) return null
    if (/^\/[a-z]{2}(?:-[A-Za-z]{2,})?$/.test(path)) return null
    return {
      title: t('plugin_website.nav.errUnknownPath', { path }),
      hints: [t('plugin_website.nav.errUnknownHint1'), t('plugin_website.nav.errUnknownHint2')],
    }
  }
  const [parentId, setParentId] = useState(editing?.parentId ?? defaultParentId)
  const [sort, setSort] = useState(editing?.sort ?? 0)
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)

  // 上级选项：顶级 + 现有节点；排除自己与后代，并过滤掉放下后会超过 3 级的候选。
  const parentOptions = useMemo<SimpleSelectOption[]>(() => {
    const self = editing ? findNavNode(tree, editing.id) ?? { ...editing, children: [] } : null
    const blocked = new Set<string>()
    if (self) collectNavIds(self, blocked)
    const height = self ? navHeight(self) : 1
    const out: SimpleSelectOption[] = [{ value: '0', label: t('plugin_website.nav.topLevel') }]
    const walk = (nodes: NavNode[], depth: number) => {
      for (const n of nodes) {
        if (!blocked.has(n.id) && depth + height <= NAV_MAX_DEPTH) {
          out.push({ value: n.id, label: `${'— '.repeat(depth - 1)}${n.label}` })
        }
        walk(n.children ?? [], depth + 1)
      }
    }
    walk(tree, 1)
    return out
  }, [tree, editing, t])

  const submit = async () => {
    const base = lf.baseValues()
    const finalHref = target === CUSTOM_HREF ? href.trim() : target
    if (!(base.label ?? '').trim() || !finalHref) {
      toast.error(t('plugin_website.nav.required'))
      return
    }
    if (target === CUSTOM_HREF) {
      const err = checkHref(finalHref)
      if (err) {
        setHrefError(err)
        return
      }
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          parentId,
          href: finalHref,
          sort,
          enabled,
          // 行上的裸列永远存原文；nav 不可按语言发布，只回传 i18nJson
          label: base.label ?? '',
          i18nJson: lf.serialize().i18nJson,
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 具体原因（上级不存在 / 超层级等）已由请求层 toast
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.nav.edit') : t('plugin_website.nav.create')}
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
        <LocalizedField fieldKey="label" required />
        <SharedFieldShell labelKey="plugin_website.nav.hrefTarget" required>
          <SimpleSelect
            value={target}
            onChange={(v) => { setHrefError(null); setTarget(v) }}
            options={[...presets, { value: CUSTOM_HREF, label: t('plugin_website.nav.hrefCustom') }]}
            aria-label={t('plugin_website.nav.hrefTarget')}
            disabled={sharedDisabled}
          />
        </SharedFieldShell>
        {target === CUSTOM_HREF && (
          <SharedFieldShell labelKey="plugin_website.fields.navHref" required>
            <Input
              required
              value={href}
              disabled={sharedDisabled}
              onChange={(e) => { setHrefError(null); setHref(e.target.value) }}
              placeholder="https://example.com/page"
            />
          </SharedFieldShell>
        )}
        <p className="text-xs text-muted-foreground">{t('plugin_website.hint.navHref')}</p>
        {hrefError && (
          <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2">
            <p className="text-xs font-medium text-destructive">{hrefError.title}</p>
            <ul className="mt-1 list-disc space-y-0.5 ps-4 text-xs text-muted-foreground">
              {hrefError.hints.map((hint) => <li key={hint}>{hint}</li>)}
            </ul>
          </div>
        )}
        <SharedFieldShell labelKey="plugin_website.nav.parent">
          <SimpleSelect
            value={parentId}
            onChange={setParentId}
            options={parentOptions}
            aria-label={t('plugin_website.nav.parent')}
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
        <SwitchRow label={t('plugin_website.fields.enabled')} checked={enabled} disabled={sharedDisabled} onCheckedChange={setEnabled} />
        <LocaleFormActions />
      </div>
    </ResponsiveDialog>
  )
}

function NavFormDialog({
  onOpenChange, editing, tree, defaultParentId, pages,
}: {
  onOpenChange: (v: boolean) => void
  editing: NavItem | null
  tree: NavNode[]
  defaultParentId: string
  pages: SitePage[]
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('nav')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('nav', texts, defaultLocale, to, format),
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
      base={{ label: editing?.label ?? '' }}
      i18nJson={editing?.i18nJson}
      translate={translate}
    >
      <NavFormBody
        onOpenChange={onOpenChange}
        editing={editing}
        tree={tree}
        defaultParentId={defaultParentId}
        pages={pages}
      />
    </LocaleFormProvider>
  )
}

export default function NavPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('nav')
  const { data: navs = [], isLoading } = useNavs()
  const { data: pagesData } = usePages({ page: 1, pageSize: 100 })
  const remove = useDeleteNav()
  const reorder = useReorderNavs()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<NavItem | null>(null)
  const [parentId, setParentId] = useState('0')

  const tree = useMemo(() => buildNavTree(navs), [navs])

  const openCreate = (pid = '0') => {
    setEditing(null)
    setParentId(pid)
    setFormOpen(true)
  }
  const openEdit = (row: NavItem) => {
    setEditing(row)
    setParentId(row.parentId || '0')
    setFormOpen(true)
  }

  const onDelete = async (row: NavNode) => {
    const children = countNavs(row.children ?? [])
    const description = children > 0
      ? t('plugin_website.nav.deleteCascadeConfirm', { name: row.label, count: children })
      : t('plugin_website.nav.deleteConfirm', { name: row.label })
    if (await confirm({ destructive: true, description })) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<NavNode>[] = [
    { key: 'label', title: t('plugin_website.fields.navLabel'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.label}</span> },
    { key: 'href', title: t('plugin_website.fields.navHref'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">{r.href}</span> },
    { key: 'i18n', title: t('plugin_website.i18n.status'), render: (r) => <TranslationStatus value={r.i18nJson} fields={['label']} /> },
    { key: 'enabled', title: t('plugin_website.fields.enabled'), render: (r) => (r.enabled ? <Badge variant="success">{t('plugin_website.enabledOn')}</Badge> : <Badge variant="outline">{t('plugin_website.enabledOff')}</Badge>) },
    // 排序由拖拽承担，不再单独摆一列数字（数字仍在编辑弹窗里可改）
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.nav.title')} description={t('plugin_website.nav.desc')} />
      <TreeTable<NavNode>
        columns={columns}
        data={tree}
        loading={isLoading}
        defaultExpandAll
        maxDepth={NAV_MAX_DEPTH}
        columnSettingsKey="plugin-website-navs"
        emptyText={t('plugin_website.empty')}
        // 同层拖动后整组重排；失败时 TreeTable 回滚乐观数据并 toast
        onReorder={
          has('website:nav')
            ? (pid, ids) => reorder.mutateAsync(ids.map((id, i) => ({ id, parentId: pid ?? '0', sort: (i + 1) * 10 })))
            : undefined
        }
        // 跨级拖动（右移降为子级 / 左移升一级 / 拖到顶部区域回顶级）：整棵树重排后一次提交
        onMove={
          has('website:nav')
            ? async (id, parentId, afterId) => {
                const next = moveNavNode(tree, id, parentId ?? '0', afterId)
                await reorder.mutateAsync(flattenNavTree(next))
              }
            : undefined
        }
        toolbar={
          <Can perm="website:nav">
            <Button onClick={() => openCreate()}>
              <Plus aria-hidden="true" />
              {t('plugin_website.nav.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:nav">
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.nav.addChild')} onClick={() => openCreate(row.id)}>
              <Plus aria-hidden="true" />
            </Button>
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
        <NavFormDialog
          key={`${editing?.id ?? 'new'}:${parentId}`}
          onOpenChange={setFormOpen}
          editing={editing}
          tree={tree}
          defaultParentId={parentId}
          pages={pagesData?.list ?? []}
        />
      )}
    </PageContainer>
  )
}
