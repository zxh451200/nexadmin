import { useMemo, useState } from 'react'
import { icons } from 'lucide-react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { DictTag } from '@/framework/components/DictTag'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { SimpleFilterBar } from '@/framework/components/SimpleFilterBar'
import { filterTree, matchText, useClientFilter } from '@/framework/components/table/use-client-filter'
import { TreeTable } from '@/framework/components/TreeTable'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { countDescendants, useDeleteMenu, useMenuTree, useMoveMenu, useSortMenus, type SysMenu } from './api'
import { MenuFormDialog } from './MenuFormDialog'

export default function MenuPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const confirm = useConfirm()
  const { data = [], isLoading } = useMenuTree()
  const del = useDeleteMenu()
  const sort = useSortMenus()
  const move = useMoveMenu()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SysMenu | null>(null)
  const [parentId, setParentId] = useState<string | null>(null)

  const tr = (s: string) => t(s, { defaultValue: s })

  const filter = useClientFilter()
  const shown = useMemo(() => {
    if (!filter.active) return data
    const f = filter.applied
    return filterTree(
      data,
      (n) =>
        matchText(f.keyword, t(n.title, { defaultValue: n.title }), n.perm, n.path) && (!f.type || n.type === f.type) && (!f.status || n.status === f.status),
    )
  }, [data, filter.active, filter.applied, t])

  const openCreate = (pid: string | null) => {
    setEditing(null)
    setParentId(pid)
    setDialogOpen(true)
  }
  const openEdit = (row: SysMenu) => {
    setEditing(row)
    setParentId(null)
    setDialogOpen(true)
  }

  const onDelete = async (row: SysMenu) => {
    const count = countDescendants(row)
    const name = tr(row.title)
    const ok = await confirm({
      destructive: true,
      description: count > 0 ? t('sysMenu.confirm.deleteCascade', { name, count }) : t('sysMenu.confirm.delete', { name }),
    })
    if (!ok) return
    try {
      await del.mutateAsync(row.id)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const canDrag = has('menu:update') && !filter.active
  const sortColumn: Column<SysMenu> = { key: 'sort', title: t('sysMenu.col.sort'), align: 'end', render: (r) => <span className="num">{r.sort}</span> }

  const columns: Column<SysMenu>[] = [
    {
      key: 'title',
      title: t('sysMenu.col.title'),
      mobile: 'title',
      render: (r) => {
        const Icon = r.icon ? (icons as Record<string, React.ComponentType<{ className?: string }>>)[r.icon] : undefined
        return (
          <span className="inline-flex items-center gap-2">
            {Icon ? <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" /> : null}
            <span className="truncate">{tr(r.title)}</span>
          </span>
        )
      },
    },
    { key: 'type', title: t('sysMenu.col.type'), mobile: 'sub', render: (r) => <DictTag dict="menu_type" value={r.type} /> },
    { key: 'perm', title: t('sysMenu.col.perm') },
    { key: 'path', title: t('sysMenu.col.path') },
    // 拖拽可用时排序由拖动承担，再摆一列数字等于两套机制；只在拖拽被筛选禁用时露出来
    ...(canDrag ? [] : [sortColumn]),
    { key: 'status', title: t('sysMenu.col.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('sysMenu.menu')} description={t('sysMenu.description')} />
      <SimpleFilterBar filter={filter} placeholder={t('sysMenu.col.title')} typeDict="menu_type" typePlaceholder={t('sysMenu.col.type')} />
      <TreeTable<SysMenu>
        toolbar={
          <Can perm="menu:create">
            <Button type="button" onClick={() => openCreate(null)}>
              <Plus className="me-2 size-4" aria-hidden="true" />
              {t('sysMenu.action.add')}
            </Button>
          </Can>
        }
        columns={columns}
        data={shown}
        loading={isLoading}
        columnSettingsKey="sysMenu"
        forceExpandAll={filter.active}
        onReorder={canDrag ? (pid, ids) => sort.mutateAsync({ parentId: pid, ids }) : undefined}
        onMove={canDrag ? (id, parentId, afterId) => move.mutateAsync({ id, parentId, afterId }) : undefined}
        // 按钮是最末级：拖到按钮行右侧时当场拦下并说明原因，不等服务端报错
        blockNest={(_active, target) => (target.type === 'button' ? 'sysMenu.drag.underButton' : null)}
        rowActions={(row) => (
          <>
            {row.type !== 'button' && (
              <Can perm="menu:create">
                <Button type="button" variant="ghost" size="icon" aria-label={t('sysMenu.action.addChild')} onClick={() => openCreate(row.id)}>
                  <Plus className="size-4" aria-hidden="true" />
                </Button>
              </Can>
            )}
            <Can perm="menu:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openEdit(row)}>
                <Pencil className="size-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="menu:delete">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => onDelete(row)}>
                <Trash2 className="size-4 text-destructive" aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <MenuFormDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
        defaultParentId={parentId}
        tree={data}
      />
    </PageContainer>
  )
}
