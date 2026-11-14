import { useState } from 'react'
import { Download, KeyRound, LockOpen, Pencil, Plus, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { pageAfterDelete } from '@/framework/hooks/page-fallback'
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
import { Switch } from '@/framework/ui/switch'
import {
  createExportJob,
  useBatchUserStatus,
  useDeleteUsers,
  useUnlockUser,
  useUpdateUserStatus,
  useUserList,
  toUserQuery,
  type User,
  type UserFilters,
} from './api'
import { ImportDialog } from './ImportDialog'
import { ResetPasswordDialog } from './ResetPasswordDialog'
import { UserFormDialog } from './UserFormDialog'

const iconBtn = 'h-11 w-11 md:h-9 md:w-9'
const isLocked = (u: User) => !!u.locked && (!u.lockedUntil || new Date(u.lockedUntil).getTime() > Date.now())
const errMsg = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback)

export default function UserPage() {
  const { t, i18n } = useTranslation()
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
  const { start: startExportJob } = useAsyncJob()

  const list = useUserList({ page, pageSize, ...toUserQuery(applied) })
  const del = useDeleteUsers()
  const setStatus = useUpdateUserStatus()
  const batch = useBatchUserStatus()
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
      const rows = list.data?.list ?? []
      const removed = rows.filter((r) => ids.includes(r.id)).length
      await del.mutateAsync(ids)
      setPage(pageAfterDelete(page, rows.length, removed))
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

  // 批量启停：后端逐个走单条护栏，自己/超管会被跳过并如实回报
  const batchStatus = async (ids: string[], on: boolean) => {
    try {
      const res = await batch.mutateAsync({ ids, status: on ? '1' : '0' })
      setSelected([])
      toast.success(
        res.skipped > 0 ? t('user.batchStatusPartial', { updated: res.updated, skipped: res.skipped }) : t('user.statusChanged'),
      )
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

  // 统一异步导出：提交 core/job 任务进导出中心，提交/完成提示由 useAsyncJob 统一给出
  const exportUsers = async () => {
    try {
      const q = toUserQuery(applied)
      await startExportJob(() => createExportJob(q))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
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
      // 角色多时只露前两枚、其余收进「+N」：行高不随角色数乱涨，全量名单悬停可看
      render: (u) =>
        u.roleNames.length ? (
          <div className="flex max-w-56 flex-wrap gap-1" title={u.roleNames.join('、')}>
            {u.roleNames.slice(0, 2).map((r) => (
              <Badge key={r} variant="outline">
                {r}
              </Badge>
            ))}
            {u.roleNames.length > 2 ? <Badge variant="secondary">+{u.roleNames.length - 2}</Badge> : null}
          </div>
        ) : (
          <span className="text-muted-foreground">-</span>
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
      render: (u) => <span className="num">{new Date(u.createdAt).toLocaleString(i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('user.title')} description={t('user.description')} />

      <AiFilterBox
        scope="user"
        onApply={(f) => {
          // AI 筛选产出的是单值，多选字段统一收进数组
          const next: UserFilters = {
            keyword: f.keyword,
            status: f.status ? [f.status] : undefined,
            gender: f.gender ? [f.gender] : undefined,
            createdFrom: f.createdFrom,
            createdTo: f.createdTo,
          }
          setDraft({ keyword: next.keyword, status: next.status, gender: next.gender })
          setApplied(next)
          setPage(1)
          setSelected([])
        }}
      />

      <FilterBar onSearch={search} onReset={reset}>
        <Input aria-label={t('user.keyword')} className="md:w-64"
          id="user-keyword"
          value={draft.keyword ?? ''}
          placeholder={t('user.keywordPlaceholder')}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
        />
        <DictSelect dict="sys_status" multiple value={draft.status ?? []} onChange={(v) => setDraft((d) => ({ ...d, status: v.length ? v : undefined }))} aria-label={t('common.status')} className="md:w-44" placeholder={t('common.status')} />
        <DictSelect dict="sys_gender" multiple value={draft.gender ?? []} onChange={(v) => setDraft((d) => ({ ...d, gender: v.length ? v : undefined }))} aria-label={t('user.gender')} className="md:w-36" placeholder={t('user.gender')} />
        <TreeSelect url="/depts/tree" multiple value={draft.deptIds ?? []} onChange={(v) => setDraft((d) => ({ ...d, deptIds: v.length ? v : undefined }))} aria-label={t('user.dept')} className="md:w-56" placeholder={t('user.dept')} />
      </FilterBar>

      <DataTable<User>
        toolbar={
          <>
            <Can perm="user:create">
              <Button type="button" onClick={() => openForm(null)}>
                <Plus className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.create')}
              </Button>
            </Can>
            <Can perm="user:import">
              <Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
                <Upload className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.import')}
              </Button>
            </Can>
            <Can perm="user:export">
              <Button type="button" variant="outline" onClick={() => void exportUsers()}>
                <Download className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.export')}
              </Button>
            </Can>
          </>
        }
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
          <>
            <Can perm="user:update">
              <Button type="button" variant="outline" size="sm" disabled={batch.isPending} onClick={() => void batchStatus(ids, true)}>
                {t('user.batchEnable')}
              </Button>
              <Button type="button" variant="outline" size="sm" disabled={batch.isPending} onClick={() => void batchStatus(ids, false)}>
                {t('user.batchDisable')}
              </Button>
            </Can>
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
          </>
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
