import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAssignRoleMenus, useMenuTree, useRoleMenus, type PermissionTreeNode, type Role } from './api'
import { PermissionTree } from './PermissionTree'

export interface AssignPermissionDialogProps {
  role: Role | null
  onOpenChange: (open: boolean) => void
}

function allMenuIds(nodes: PermissionTreeNode[]): string[] {
  return nodes.flatMap((node) => [node.id, ...allMenuIds(node.children ?? [])])
}

export function AssignPermissionDialog({ role, onOpenChange }: AssignPermissionDialogProps) {
  const { t } = useTranslation()
  const open = !!role
  const builtin = role?.builtin === true
  const tree = useMenuTree(open)
  const current = useRoleMenus(role?.id)
  const assign = useAssignRoleMenus()
  const [ids, setIds] = useState<string[]>([])
  const allIds = useMemo(() => allMenuIds(tree.data ?? []), [tree.data])
  const permissionIds = builtin ? allIds : ids

  useEffect(() => {
    if (current.data) setIds(current.data)
  }, [current.data])
  useEffect(() => {
    if (!open) setIds([])
  }, [open])

  const loading = tree.isLoading || (!builtin && current.isLoading)

  const save = async () => {
    if (!role || builtin) return
    try {
      await assign.mutateAsync({ id: role.id, menuIds: ids })
      toast.success(t('role.assign.success'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog side="right"
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={t('role.assign.title')}
      description={t('role.assign.description', { name: role?.name ?? '' })}
      footer={
        builtin ? (
          <Button type="button" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
        ) : (
          <>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={save} disabled={assign.isPending || loading}>
              {assign.isPending ? t('common.loading') : t('common.save')}
            </Button>
          </>
        )
      }
    >
      {loading ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-col gap-3">
          {builtin ? (
            <p className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
              {t('role.assign.builtinHint')}
            </p>
          ) : null}
          <PermissionTree data={tree.data ?? []} value={permissionIds} onChange={setIds} disabled={builtin} />
        </div>
      )}
    </ResponsiveDialog>
  )
}
