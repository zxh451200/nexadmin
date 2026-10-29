import { useMemo, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { SimpleFilterBar } from '@/framework/components/SimpleFilterBar'
import { matchText, useClientFilter } from '@/framework/components/table/use-client-filter'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { useDeletePost, useDeptNodes, usePostList, useSortPosts, type DeptNode, type SysPost } from './api'
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
  const { data: depts = [] } = useDeptNodes()
  const del = useDeletePost()
  const sort = useSortPosts()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SysPost | null>(null)

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
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<SysPost>[] = [
    { key: 'name', title: t('post.col.name'), mobile: 'title' },
    { key: 'code', title: t('post.col.code'), mobile: 'sub' },
    { key: 'deptId', title: t('post.col.dept'), render: (r) => (r.deptId ? deptNames.get(r.deptId) ?? '-' : '-') },
    { key: 'status', title: t('post.col.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
    { key: 'remark', title: t('post.col.remark'), mobile: 'hidden' },
  ]

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">{t('post.description')}</p>
      <SimpleFilterBar filter={filter} placeholder={t('post.col.name')} />
      <DataTable<SysPost>
        toolbar={
          <Can perm="post:create">
            <Button type="button" onClick={() => openEdit(null)}>
              <Plus className="me-2 size-4" aria-hidden="true" />
              {t('post.action.add')}
            </Button>
          </Can>
        }
        columns={columns}
        data={shown}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        columnSettingsKey="post"
        sortable={filter.active ? undefined : { onReorder: (ids) => sort.mutateAsync(ids) }}
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
