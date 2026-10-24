import { useState } from 'react'
import { Download, KeyRound, LockOpen, Pencil, Plus, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { AiFilterBox } from '@/framework/components/AiFilterBox'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Avatar, AvatarFallback, AvatarImage } from '@/framework/ui/avatar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import {
  blobExt,
  createExportJob,
  exportUsersFile,
  fetchExportPlan,
  useDeleteUsers,
  useUnlockUser,
  useUpdateUserStatus,
  useUserList,
  type User,
  type UserFilters,
} from './api'
import { saveBlob } from './csv'
import { ImportDialog } from './ImportDialog'
import { ResetPasswordDialog } from './ResetPasswordDialog'
import { UserFormDialog } from './UserFormDialog'

const iconBtn = 'h-11 w-11 md:h-9 md:w-9'
const isLocked = (u: User) => !!u.locked && (!u.lockedUntil || new Date(u.lockedUntil).getTime() > Date.now())
const errMsg = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback)

export default function UserPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<UserFilters>({})
  const [applied, setApplied] = useState<UserFilters>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [resetTarget, setResetTarget] = useState<User | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [exporting, setExporting] = useState(false)
  const { start: startExportJob } = useAsyncJob()

  const list = useUserList({ page, pageSize, ...applied })
  const del = useDeleteUsers()
  const setStatus = useUpdateUserStatus()
  const unlock = useUnlockUser()

  const search = () => {
    setPage(1)
    setSelected([])
    setApplied({ ...draft })
  }
  const reset = () => {
    setDraft({})
    setApplied({})
    setPage(1)
    setSelected([])
  }

  const openForm = (u: User | null) => {
    setEditing(u)
    setFormOpen(true)
  }

  const remove = async (ids: string[], description: string) => {
    if (!(await confirm({ destructive: true, description }))) return
    try {
      await del.mutateAsync(ids)
      setSelected((s) => s.filter((x) => !ids.includes(x)))
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    }
  }

  const toggleStatus = async (u: User, on: boolean) => {
    try {
      await setStatus.mutateAsync({ id: u.id, status: on ? '1' : '0' })
      toast.success(t('user.statusChanged'))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    }
  }

  const unlockUser = async (u: User) => {
    try {
      await unlock.mutateAsync(u.id)
      toast.success(t('user.unlock.success'))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    }
  }

  const exportUsers = async () => {
    setExporting(true)
    try {
      // 匹配行数超过 export.sync_limit 时改走异步任务：提示「已提交到导出中心」，完成后在导出中心下载
      const plan = await fetchExportPlan(applied)
      if (plan?.async) {
        await startExportJob(() => createExportJob(applied))
        return
      }
      // 服务端按当前筛选条件与数据范围生成 .xlsx；列由服务端白名单决定，不含密码等敏感字段
      const blob = await exportUsersFile(applied)
      saveBlob(`${t('user.export.filename')}.${blobExt(blob)}`, blob)
      toast.success(t('user.export.done'))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    } finally {
      setExporting(false)
    }
  }

  const columns: Column<User>[] = [
    {
      key: 'avatar',
      title: t('user.col.avatar'),
      mobile: 'hidden',
      width: 72,
      render: (u) => (
        <Avatar className="h-9 w-9">
          {u.avatar ? <AvatarImage src={u.avatar} alt={u.nickname} /> : null}
          <AvatarFallback>{u.nickname.slice(0, 1).toUpperCase()}</AvatarFallback>
        </Avatar>
      ),
    },
    { key: 'nickname', title: t('user.col.nickname'), mobile: 'title' },
    { key: 'username', title: t('user.col.username'), mobile: 'sub' },
    { key: 'deptName', title: t('user.col.dept') },
    {
      key: 'roleNames',
      title: t('user.col.roles'),
      render: (u) => (
        <div className="flex flex-wrap gap-1">
          {u.roleNames.map((r) => (
            <Badge key={r} variant="outline">
              {r}
            </Badge>
          ))}
        </div>
      ),
    },
    { key: 'phone', title: t('user.col.phone') },
    {
      key: 'status',
      title: t('user.col.status'),
      render: (u) => (
        <div className="flex flex-wrap items-center gap-2">
        <Can
          perm="user:update"
          fallback={<Badge variant={u.status === '1' ? 'success' : 'secondary'}>{u.status === '1' ? t('common.enable') : t('common.disable')}</Badge>}
        >
          <span className="inline-flex min-h-11 items-center md:min-h-0">
            <Switch
              checked={u.status === '1'}
              disabled={u.username === 'admin' || setStatus.isPending}
              aria-label={`${t('user.col.status')}: ${u.nickname}`}
              onCheckedChange={(on) => toggleStatus(u, on)}
            />
          </span>
        </Can>
        {isLocked(u) ? <Badge variant="warning">{t('user.locked')}</Badge> : null}
        </div>
      ),
    },
    {
      key: 'createdAt',
      title: t('user.col.createdAt'),
      render: (u) => <span className="num">{new Date(u.createdAt).toLocaleString()}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('user.title')}
        description={t('user.description')}
        actions={
          <>
            <Can perm="user:import">
              <Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
                <Upload className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.import')}
              </Button>
            </Can>
            <Can perm="user:export">
              <Button type="button" variant="outline" onClick={exportUsers} disabled={exporting}>
                <Download className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.export')}
              </Button>
            </Can>
            <Can perm="user:create">
              <Button type="button" onClick={() => openForm(null)}>
                <Plus className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.create')}
              </Button>
            </Can>
          </>
        }
      />

      <AiFilterBox
        scope="user"
        onApply={(f) => {
          const next: UserFilters = { keyword: f.keyword, status: f.status, gender: f.gender, createdFrom: f.createdFrom, createdTo: f.createdTo }
          setDraft({ keyword: next.keyword, status: next.status })
          setApplied(next)
          setPage(1)
          setSelected([])
        }}
      />

      <FilterBar onSearch={search} onReset={reset}>
        <div className="flex flex-col gap-1.5 md:w-64">
          <Label htmlFor="user-keyword">{t('user.keyword')}</Label>
          <Input
            id="user-keyword"
            value={draft.keyword ?? ''}
            placeholder={t('user.keywordPlaceholder')}
            onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          />
        </div>
        <div className="flex flex-col gap-1.5 md:w-40">
          <Label>{t('common.status')}</Label>
          <DictSelect dict="sys_status" value={draft.status ?? ''} onChange={(v) => setDraft((d) => ({ ...d, status: v || undefined }))} />
        </div>
        <div className="flex flex-col gap-1.5 md:w-56">
          <Label>{t('user.dept')}</Label>
          <TreeSelect url="/depts/tree" value={draft.deptId ?? ''} onChange={(v) => setDraft((d) => ({ ...d, deptId: v || undefined }))} />
        </div>
      </FilterBar>

      <DataTable<User>
        columns={columns}
        data={list.data?.list ?? []}
        rowKey="id"
        loading={list.isLoading}
        columnSettingsKey="user-list"
        pagination={{
          page,
          pageSize,
          total: list.data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="user:delete">
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={() => remove(ids, t('user.deleteBatch', { count: ids.length }))}
            >
              <Trash2 className="me-2 h-4 w-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(u) => (
          <>
            <Can perm="user:update">
              <Button type="button" variant="ghost" size="icon" className={iconBtn} aria-label={t('common.edit')} onClick={() => openForm(u)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="user:resetPwd">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className={iconBtn}
                aria-label={t('user.resetPassword.action')}
                onClick={() => setResetTarget(u)}
              >
                <KeyRound className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            {isLocked(u) ? (
              <Can perm="user:unlock">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className={iconBtn}
                  aria-label={t('user.unlock.action')}
                  title={t('user.unlock.action')}
                  disabled={unlock.isPending}
                  onClick={() => unlockUser(u)}
                >
                  <LockOpen className="h-4 w-4" aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
            {u.username !== 'admin' ? (
              <Can perm="user:delete">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className={`${iconBtn} text-destructive`}
                  aria-label={t('common.delete')}
                  onClick={() => remove([u.id], t('user.deleteOne', { name: u.nickname }))}
                >
                  <Trash2 className="h-4 w-4" aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </>
        )}
      />

      <UserFormDialog open={formOpen} onOpenChange={setFormOpen} user={editing} />
      <ResetPasswordDialog user={resetTarget} onOpenChange={(o) => !o && setResetTarget(null)} />
      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
    </PageContainer>
  )
}
