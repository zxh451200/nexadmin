import { useMemo, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { PostSection } from '@/framework/modules/post/PostPage'
import { countDescendants, isTrimmed, useDeleteDept, useDeptTree, useSortDepts, type SysDept } from './api'
import { DeptFormDialog } from './DeptFormDialog'

export default function DeptPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { has } = usePermission()
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') === 'post' ? 'post' : 'dept'
  const setTab = (v: string) => setParams(v === 'post' ? { tab: 'post' } : {}, { replace: true })
  const { data = [], isLoading } = useDeptTree()
  // 被数据范围裁剪的树只含部分同级，整体排序会得到 409，裁剪时不提供拖动排序
  const trimmed = useMemo(() => isTrimmed(data), [data])
  const del = useDeleteDept()
  const sort = useSortDepts()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SysDept | null>(null)
  const [parentId, setParentId] = useState<string | null>(null)

  const filter = useClientFilter()
  const shown = useMemo(() => {
    if (!filter.active) return data
    const f = filter.applied
    return filterTree(data, (n) => matchText(f.keyword, n.name, n.leaderName, n.phone) && (!f.status || n.status === f.status))
  }, [data, filter.active, filter.applied])

  const openCreate = (pid: string | null) => {
    setEditing(null)
    setParentId(pid)
    setDialogOpen(true)
  }
  const openEdit = (row: SysDept) => {
    setEditing(row)
    setParentId(null)
    setDialogOpen(true)
  }

  const onDelete = async (row: SysDept) => {
    const count = countDescendants(row)
    const ok = await confirm({
      destructive: true,
      description:
        count > 0
          ? t('dept.confirm.deleteCascade', { name: row.name, count })
          : t('dept.confirm.delete', { name: row.name }),
    })
    if (!ok) return
    try {
      await del.mutateAsync(row.id)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const canDrag = !(filter.active || trimmed)
  const sortColumn: Column<SysDept> = { key: 'sort', title: t('dept.col.sort'), align: 'end', render: (r) => <span className="num">{r.sort}</span> }

  const columns: Column<SysDept>[] = [
    {
      key: 'name',
      title: t('dept.col.name'),
      mobile: 'title',
      render: (r) => (r.readonly ? <span className="text-muted-foreground" title={t('dept.readonlyHint')}>{r.name}</span> : r.name),
    },
    { key: 'leaderName', title: t('dept.col.leader'), mobile: 'sub' },
    { key: 'phone', title: t('dept.col.phone') },
    { key: 'email', title: t('dept.col.email') },
    // 拖拽可用时排序由拖动承担，再摆一列数字等于两套机制；只在拖拽被筛选禁用时露出来
    ...(canDrag ? [] : [sortColumn]),
    { key: 'status', title: t('dept.col.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('dept.menu')} description={t('dept.description')} />
      <Tabs value={tab} onValueChange={setTab}>
        {/* 岗位是三五条的查找表，和部门同属组织架构：并成一个页面，少一个一级菜单 */}
        <TabsList className="w-fit">
          <TabsTrigger value="dept">{t('dept.tabs.tree')}</TabsTrigger>
          {has('post:list') ? <TabsTrigger value="post">{t('dept.tabs.post')}</TabsTrigger> : null}
        </TabsList>
        <TabsContent value="dept">
      <SimpleFilterBar filter={filter} placeholder={t('dept.col.name')} />
      <TreeTable<SysDept>
        toolbar={
          <Can perm="dept:create">
            <Button type="button" onClick={() => openCreate(null)}>
              <Plus className="me-2 size-4" aria-hidden="true" />
              {t('dept.action.add')}
            </Button>
          </Can>
        }
        columns={columns}
        data={shown}
        loading={isLoading}
        defaultExpandAll
        forceExpandAll={filter.active}
        columnSettingsKey="dept"
        onReorder={filter.active || trimmed ? undefined : (pid, ids) => sort.mutateAsync({ parentId: pid, ids })}
        rowActions={(row) =>
          row.readonly ? null : (
            <>
              <Can perm="dept:create">
                <Button type="button" variant="ghost" size="icon" aria-label={t('dept.action.addChild')} onClick={() => openCreate(row.id)}>
                  <Plus className="size-4" aria-hidden="true" />
                </Button>
              </Can>
              <Can perm="dept:update">
                <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openEdit(row)}>
                  <Pencil className="size-4" aria-hidden="true" />
                </Button>
              </Can>
              <Can perm="dept:delete">
                <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => onDelete(row)}>
                  <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                </Button>
              </Can>
            </>
          )
        }
      />
      <DeptFormDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
        defaultParentId={parentId}
        tree={data}
      />
        </TabsContent>
        <TabsContent value="post">
          <PostSection />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
