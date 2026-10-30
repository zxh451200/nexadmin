import { useMemo, useState } from 'react'
import { Pencil, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { SimpleFilterBar } from '@/framework/components/SimpleFilterBar'
import { matchText, useClientFilter } from '@/framework/components/table/use-client-filter'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { useDeleteRole, useRoleList, useSortRoles, type Role } from './api'
import { AssignPermissionDialog } from './AssignPermissionDialog'
import { RoleFormDialog } from './RoleFormDialog'

const iconBtn = 'h-11 w-11 md:h-9 md:w-9'

export default function RolePage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const confirm = useConfirm()
  const list = useRoleList()
  const del = useDeleteRole()
  const sort = useSortRoles()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Role | null>(null)
  const [assigning, setAssigning] = useState<Role | null>(null)

  const rows = useMemo(() => list.data?.list ?? [], [list.data?.list])
  const nextSort = rows.reduce((m, r) => Math.max(m, r.sort), 0) + 10

  const filter = useClientFilter()
  const shown = useMemo(
    () => rows.filter((r) => matchText(filter.applied.keyword, r.name, r.code) && (!filter.applied.status || r.status === filter.applied.status)),
    [rows, filter.applied],
  )

  const openForm = (r: Role | null) => {
    setEditing(r)
    setFormOpen(true)
  }

  const remove = async (r: Role) => {
    if (!(await confirm({ destructive: true, description: t('role.deleteConfirm', { name: r.name }) }))) return
    try {
      await del.mutateAsync(r.id)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<Role>[] = [
    {
      key: 'name',
      title: t('role.col.name'),
      mobile: 'title',
      render: (r) => (
        <span className="inline-flex items-center gap-2">
          <span className="font-medium text-foreground">{r.name}</span>
          {r.builtin ? <Badge variant="info">{t('role.builtin')}</Badge> : null}
        </span>
      ),
    },
    { key: 'code', title: t('role.col.code'), mobile: 'sub', render: (r) => <span className="num">{r.code}</span> },
    { key: 'sort', title: t('role.col.sort'), width: 80, render: (r) => <span className="num">{r.sort}</span> },
    { key: 'dataScope', title: t('role.col.dataScope'), render: (r) => <DictTag dict="data_scope" value={r.dataScope} /> },
    { key: 'status', title: t('role.col.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
    {
      key: 'assignable',
      title: t('role.col.assignable'),
      render: (r) =>
        r.assignable && !r.builtin ? (
          <Badge variant="success">{t('role.assignable.yes')}</Badge>
        ) : (
          <Badge variant="outline">{t('role.assignable.no')}</Badge>
        ),
    },
    { key: 'remark', title: t('role.col.remark') },
    {
      key: 'createdAt',
      title: t('role.col.createdAt'),
      render: (r) => <span className="num">{new Date(r.createdAt).toLocaleString()}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('role.title')} description={t('role.description')} />

      <SimpleFilterBar filter={filter} placeholder={t('role.col.name')} />

      <DataTable<Role>
        toolbar={
          <Can perm="role:create">
            <Button type="button" onClick={() => openForm(null)}>
              <Plus className="me-2 h-4 w-4" aria-hidden="true" />
              {t('common.create')}
            </Button>
          </Can>
        }
        columns={columns}
        columnSettingsKey="role"
        data={shown}
        rowKey="id"
        loading={list.isLoading}
        pagination={false}
        sortable={filter.active || !has('role:update') ? undefined : { onReorder: (ids) => sort.mutateAsync(ids) }}
        rowActions={(r) => (
          <>
            <Can perm="role:assign">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className={iconBtn}
                aria-label={t('role.assign.action')}
                onClick={() => setAssigning(r)}
              >
                <ShieldCheck className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="role:update">
              <Button type="button" variant="ghost" size="icon" className={iconBtn} aria-label={t('common.edit')} onClick={() => openForm(r)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            {!r.builtin ? (
              <Can perm="role:delete">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className={`${iconBtn} text-destructive`}
                  aria-label={t('common.delete')}
                  onClick={() => remove(r)}
                >
                  <Trash2 className="h-4 w-4" aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </>
        )}
      />

      <RoleFormDialog open={formOpen} onOpenChange={setFormOpen} role={editing} nextSort={nextSort} />
      <AssignPermissionDialog role={assigning} onOpenChange={(o) => !o && setAssigning(null)} />
    </PageContainer>
  )
}
