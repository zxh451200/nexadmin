import { useMemo, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { DataTable } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { ExportCsvButton, type CsvColumn } from '@/framework/components/export/ExportCsvButton'
import { useDictItems } from '@/framework/components/pickers/useDictItems'
import { SimpleFilterBar } from '@/framework/components/SimpleFilterBar'
import { matchText, useClientFilter } from '@/framework/components/table/use-client-filter'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import type { DictItem } from '@/framework/request/types'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { useDeletePost, useDeletePosts, useDeptNodes, usePostList, useSortPosts, type DeptNode, type SysPost } from './api'
import { PostFormDialog } from './PostFormDialog'

function flatten(nodes: DeptNode[], map = new Map<string, string>()) {
  nodes.forEach((n) => {
    map.set(n.id, n.name)
    if (n.children) flatten(n.children, map)
  })
  return map
}

export function PostSection() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data = [], isLoading } = usePostList()
  const { has } = usePermission()
  // 没有 dept:list 时不请求部门树（否则岗位页一进来就吃 403），所属部门列退化为「-」
  const canDept = has('dept:list')
  const { data: depts = [] } = useDeptNodes(canDept)
  const del = useDeletePost()
  const delBatch = useDeletePosts()
  const sort = useSortPosts()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SysPost | null>(null)
  const [selected, setSelected] = useState<string[]>([])

  // 导出用的状态文本与表格里 DictTag 同一份字典（同 query key，不额外发请求）。
  const { data: statusItems } = useDictItems('sys_status')
  const dictText = (items: DictItem[] | undefined, v: string) =>
    items?.find((d) => String(d.value) === String(v))?.label ?? v

  const deptNames = useMemo(() => flatten(depts), [depts])

  const filter = useClientFilter()
  const shown = useMemo(
    () => data.filter((p) => matchText(filter.applied.keyword, p.name, p.code) && (!filter.applied.status || p.status === filter.applied.status)),
    [data, filter.applied],
  )

  const openEdit = (row: SysPost | null) => {
    setEditing(row)
    setDialogOpen(true)
  }

  const onDelete = async (row: SysPost) => {
    if (!(await confirm({ destructive: true, description: t('post.confirm.delete', { name: row.name }) }))) return
    try {
      await del.mutateAsync(row.id)
      setSelected((s) => s.filter((id) => id !== row.id))
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  // 批量删除：后端逐条走与单条相同的护栏（有人在用的岗位不让删），任一条不合格整批回滚。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('post.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      await delBatch.mutateAsync(ids)
      setSelected((s) => s.filter((id) => !ids.includes(id)))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<SysPost>[] = [
    { key: 'name', title: t('post.col.name'), mobile: 'title' },
    { key: 'code', title: t('post.col.code'), mobile: 'sub' },
    { key: 'deptId', title: t('post.col.dept'), render: (r) => (r.deptId ? deptNames.get(r.deptId) ?? '-' : '-') },
    { key: 'status', title: t('post.col.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
    { key: 'remark', title: t('post.col.remark'), mobile: 'hidden' },
  ]

  // 导出列与可见列一致：部门取名称（没有 dept:list 时与页面同样落回「-」），状态取字典文本而不是原始码。
  const exportColumns: CsvColumn<SysPost>[] = [
    { header: t('post.col.name'), value: (r) => r.name },
    { header: t('post.col.code'), value: (r) => r.code },
    { header: t('post.col.dept'), value: (r) => (r.deptId ? deptNames.get(r.deptId) ?? '-' : '-') },
    { header: t('post.col.status'), value: (r) => dictText(statusItems, r.status) },
    { header: t('post.col.remark'), value: (r) => r.remark ?? '' },
  ]

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">{t('post.description')}</p>
      <SimpleFilterBar filter={filter} placeholder={t('post.col.name')} />
      <DataTable<SysPost>
        toolbar={
          <>
            <Can perm="post:create">
              <Button type="button" onClick={() => openEdit(null)}>
                <Plus className="me-2 size-4" aria-hidden="true" />
                {t('post.action.add')}
              </Button>
            </Can>
            <ExportCsvButton<SysPost>
              filename="posts"
              perm="post:list"
              columns={exportColumns}
              getRows={async () => shown}
            />
          </>
        }
        columns={columns}
        data={shown}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="post:delete">
            <Button type="button" variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        columnSettingsKey="post"
        sortable={filter.active || !has('post:update') ? undefined : { onReorder: (ids) => sort.mutateAsync(ids) }}
        rowActions={(row) => (
          <>
            <Can perm="post:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openEdit(row)}>
                <Pencil className="size-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="post:delete">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => onDelete(row)}>
                <Trash2 className="size-4 text-destructive" aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <PostFormDialog open={dialogOpen} onOpenChange={setDialogOpen} editing={editing} count={data.length} />
    </div>
  )
}
