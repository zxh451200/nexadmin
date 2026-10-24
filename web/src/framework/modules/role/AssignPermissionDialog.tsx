import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAssignRoleMenus, useMenuTree, useRoleMenus, type Role } from './api'
import { PermissionTree } from './PermissionTree'

export interface AssignPermissionDialogProps {
  role: Role | null
  onOpenChange: (open: boolean) => void
}

export function AssignPermissionDialog({ role, onOpenChange }: AssignPermissionDialogProps) {
  const { t } = useTranslation()
  const open = !!role
  const tree = useMenuTree(open)
  const current = useRoleMenus(role?.id)
  const assign = useAssignRoleMenus()
  const [ids, setIds] = useState<string[]>([])

  useEffect(() => {
    if (current.data) setIds(current.data)
  }, [current.data])
  useEffect(() => {
    if (!open) setIds([])
  }, [open])

  const loading = tree.isLoading || current.isLoading

  const save = async () => {
    if (!role) return
    try {
      await assign.mutateAsync({ id: role.id, menuIds: ids })
      toast.success(t('role.assign.success'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={t('role.assign.title')}
      description={t('role.assign.description', { name: role?.name ?? '' })}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" onClick={save} disabled={assign.isPending || loading}>
            {assign.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : (
        <PermissionTree data={tree.data ?? []} value={ids} onChange={setIds} />
      )}
    </ResponsiveDialog>
  )
}
