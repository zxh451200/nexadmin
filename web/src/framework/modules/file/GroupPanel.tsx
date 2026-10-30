import { useState } from 'react'
import { Folder, FolderOpen, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { cn } from '@/lib/utils'
import { useCreateGroup, useDeleteGroup, useFileGroups, useRenameGroup, type FileGroup } from './api'

/** 特殊值：全部 */
export const GROUP_ALL = 'all'
/** 特殊值：未分组（对应 groupId=''，筛选时传 'none'） */
export const GROUP_NONE = 'none'

interface Props {
  value: string
  onChange: (value: string) => void
}

export function GroupPanel({ value, onChange }: Props) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const confirm = useConfirm()
  const { data: groups = [] } = useFileGroups()
  const create = useCreateGroup()
  const rename = useRenameGroup()
  const remove = useDeleteGroup()
  const [editing, setEditing] = useState<{ id?: string; name: string } | null>(null)

  const real = groups.find((g) => g.id === value)

  const save = async () => {
    const name = editing?.name.trim()
    if (!editing || !name) return
    try {
      if (editing.id) await rename.mutateAsync({ id: editing.id, name })
      else await create.mutateAsync(name)
      toast.success(t('common.operateSuccess'))
      setEditing(null)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const del = async (g: FileGroup) => {
    if (!(await confirm({ title: t('fileMgr.deleteGroup'), description: t('fileMgr.groupDeleteConfirm'), destructive: true }))) return
    try {
      await remove.mutateAsync(g.id)
      if (value === g.id) onChange(GROUP_ALL)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const item = (key: string, label: string, count: number | undefined, editable?: FileGroup) => {
    const active = value === key
    return (
      <li key={key} className="group/item flex items-center">
        <button
          type="button"
          aria-current={active ? 'true' : undefined}
          onClick={() => onChange(key)}
          className={cn(
            'flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-md px-3 text-start text-sm transition-colors motion-reduce:transition-none md:min-h-9',
            active ? 'bg-primary/10 font-medium text-primary' : 'text-foreground hover:bg-muted',
          )}
        >
          {active ? <FolderOpen className="size-4 shrink-0" aria-hidden="true" /> : <Folder className="size-4 shrink-0" aria-hidden="true" />}
          <span className="min-w-0 flex-1 truncate">{label}</span>
          {count !== undefined && <span className="num text-xs text-muted-foreground">{count}</span>}
        </button>
        {editable && (
          <Can perm="file:upload">
            <div className="flex shrink-0 items-center opacity-0 transition-opacity focus-within:opacity-100 group-hover/item:opacity-100 motion-reduce:transition-none [@media(hover:none)]:opacity-100">
              <Button variant="ghost" size="icon" aria-label={t('fileMgr.renameGroup')} className="size-11 shrink-0 md:size-8" onClick={() => setEditing({ id: editable.id, name: editable.name })}>
                <Pencil className="size-3.5" aria-hidden="true" />
              </Button>
              <Can perm="file:delete">
                <Button variant="ghost" size="icon" aria-label={t('fileMgr.deleteGroup')} className="size-11 shrink-0 text-destructive md:size-8" onClick={() => void del(editable)}>
                  <Trash2 className="size-3.5" aria-hidden="true" />
                </Button>
              </Can>
            </div>
          </Can>
        )}
      </li>
    )
  }

  const addBtn = (
    <Can perm="file:upload">
      <Button variant="outline" size="sm" className="h-11 md:h-8" onClick={() => setEditing({ name: '' })}>
        <Plus className="me-1 size-4" aria-hidden="true" />
        {t('fileMgr.addGroup')}
      </Button>
    </Can>
  )

  return (
    <>
      {isMobile ? (
        <div className="flex flex-wrap items-center gap-2">
          <Select value={value} onValueChange={onChange}>
            <SelectTrigger aria-label={t('fileMgr.currentGroup')} className="min-w-0 flex-1 basis-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GROUP_ALL}>{t('fileMgr.allFiles')}</SelectItem>
              <SelectItem value={GROUP_NONE}>{t('fileMgr.ungrouped')}</SelectItem>
              {groups.map((g) => (
                <SelectItem key={g.id} value={g.id}>
                  {g.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {real && (
            <Can perm="file:upload">
              <Button variant="outline" size="icon" aria-label={t('fileMgr.renameGroup')} className="size-11" onClick={() => setEditing({ id: real.id, name: real.name })}>
                <Pencil className="size-4" aria-hidden="true" />
              </Button>
              <Can perm="file:delete">
                <Button variant="outline" size="icon" aria-label={t('fileMgr.deleteGroup')} className="size-11 text-destructive" onClick={() => void del(real)}>
                  <Trash2 className="size-4" aria-hidden="true" />
                </Button>
              </Can>
            </Can>
          )}
          {addBtn}
        </div>
      ) : (
        <nav aria-label={t('fileMgr.groups')} className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between gap-2 ps-1">
            <h2 className="text-xs font-medium text-muted-foreground">{t('fileMgr.groups')}</h2>
            <Can perm="file:upload">
              <Button
                variant="ghost"
                size="icon"
                className="size-11 shrink-0 md:size-7"
                aria-label={t('fileMgr.addGroup')}
                title={t('fileMgr.addGroup')}
                onClick={() => setEditing({ name: '' })}
              >
                <Plus className="size-4" aria-hidden="true" />
              </Button>
            </Can>
          </div>
          <ul className="flex flex-col gap-0.5">
            {item(GROUP_ALL, t('fileMgr.allFiles'), undefined)}
            {item(GROUP_NONE, t('fileMgr.ungrouped'), undefined)}
            {groups.map((g) => item(g.id, g.name, g.count, g))}
          </ul>
        </nav>
      )}

      <ResponsiveDialog
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing?.id ? t('fileMgr.renameGroup') : t('fileMgr.addGroup')}
        size="sm"
        footer={
          <>
            <Button variant="outline" onClick={() => setEditing(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!editing?.name.trim() || create.isPending || rename.isPending} onClick={() => void save()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Label htmlFor="file-group-name">{t('fileMgr.groupName')}</Label>
          <Input
            id="file-group-name"
            autoFocus
            maxLength={30}
            value={editing?.name ?? ''}
            placeholder={t('fileMgr.groupNamePlaceholder')}
            className="text-base md:text-sm"
            onChange={(e) => setEditing((s) => (s ? { ...s, name: e.target.value } : s))}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void save()
            }}
          />
        </div>
      </ResponsiveDialog>
    </>
  )
}
