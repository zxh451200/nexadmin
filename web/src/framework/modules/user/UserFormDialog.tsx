import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { DictSelect } from '@/framework/components/DictSelect'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RegionSelect } from '@/framework/components/RegionSelect'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Uploader } from '@/framework/components/Uploader'
import { api } from '@/framework/request/http'
import type { UploadedFile } from '@/framework/upload/types'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { useCreateUser, useUpdateUser, type User, type UserInput } from './api'

interface DeptNode {
  id: string
  name: string
  children?: DeptNode[]
}

function flatten(nodes: DeptNode[] | undefined, map = new Map<string, string>()) {
  nodes?.forEach((n) => {
    map.set(n.id, n.name)
    flatten(n.children, map)
  })
  return map
}

const FORM_ID = 'user-form'

export interface UserFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 为空表示新建 */
  user: User | null
}

export function UserFormDialog({ open, onOpenChange, user }: UserFormDialogProps) {
  const { t } = useTranslation()
  const create = useCreateUser()
  const update = useUpdateUser()
  const isEdit = !!user

  const depts = useQuery({ queryKey: ['user-form', 'depts'], queryFn: () => api.get<DeptNode[]>('/depts/tree'), enabled: open })
  const roles = useQuery({
    queryKey: ['user-form', 'roles'],
    queryFn: () => api.get<{ id: string; name: string }[]>('/roles/options', { scene: 'assign' }),
    enabled: open,
  })

  const schema = useMemo(
    () =>
      z.object({
        username: z
          .string()
          .trim()
          .min(3, t('user.v.usernameMin'))
          .max(32)
          .regex(/^[A-Za-z0-9_.-]+$/, t('user.v.usernameFormat')),
        nickname: z.string().trim().min(1, t('user.v.nicknameRequired')).max(32),
        phone: z.string().trim().regex(/^$|^1[3-9]\d{9}$/, t('user.v.phoneFormat')),
        email: z.string().trim().email(t('user.v.emailFormat')).or(z.literal('')),
        gender: z.string(),
        deptId: z.string().min(1, t('user.v.deptRequired')),
        postIds: z.array(z.string()),
        roleIds: z.array(z.string()).min(1, t('user.v.rolesRequired')),
        enabled: z.boolean(),
        avatar: z.array(z.custom<UploadedFile>()),
        region: z.object({ province: z.string().optional(), city: z.string().optional(), district: z.string().optional() }),
        remark: z.string().max(200),
      }),
    [t],
  )
  type FormValues = z.infer<typeof schema>

  const toValues = (u: User | null): FormValues => ({
    username: u?.username ?? '',
    nickname: u?.nickname ?? '',
    phone: u?.phone ?? '',
    email: u?.email ?? '',
    gender: u?.gender ?? '0',
    deptId: u?.deptId ?? '',
    postIds: u?.postIds ?? [],
    roleIds: u?.roleIds ?? [],
    enabled: (u?.status ?? '1') === '1',
    avatar: u?.avatar ? [{ id: 'avatar', url: u.avatar, name: 'avatar', size: 0, type: 'image/*' }] : [],
    region: u?.region ?? {},
    remark: u?.remark ?? '',
  })

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: toValues(user) })
  useEffect(() => {
    if (open) form.reset(toValues(user))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, user])

  const pending = create.isPending || update.isPending

  const onSubmit = form.handleSubmit(async (v) => {
    const deptMap = flatten(depts.data)
    const roleMap = new Map((roles.data ?? []).map((r) => [r.id, r.name]))
    const input: UserInput = {
      username: v.username,
      nickname: v.nickname,
      avatar: v.avatar[0]?.url ?? '',
      phone: v.phone,
      email: v.email,
      gender: v.gender,
      deptId: v.deptId,
      deptName: deptMap.get(v.deptId) ?? user?.deptName ?? '',
      postIds: v.postIds,
      roleIds: v.roleIds,
      roleNames: v.roleIds.map((id) => roleMap.get(id) ?? id),
      status: v.enabled ? '1' : '0',
      region: v.region,
      remark: v.remark,
    }
    try {
      if (user) await update.mutateAsync({ id: user.id, input })
      else await create.mutateAsync(input)
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={isEdit ? t('user.form.editTitle') : t('user.form.createTitle')}
      description={isEdit ? undefined : t('user.form.defaultPasswordHint')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={pending}>
            {pending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id={FORM_ID} onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.username')}</FormLabel>
                  <FormControl>
                    <Input {...field} disabled={isEdit} autoComplete="off" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="nickname"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.nickname')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="phone"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.phone')}</FormLabel>
                  <FormControl>
                    <Input {...field} type="tel" inputMode="tel" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.email')}</FormLabel>
                  <FormControl>
                    <Input {...field} type="email" inputMode="email" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="gender"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.gender')}</FormLabel>
                  <FormControl>
                    <DictSelect dict="sys_gender" value={field.value} onChange={field.onChange} allowClear={false} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="deptId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.dept')}</FormLabel>
                  <FormControl>
                    <TreeSelect url="/depts/tree" value={field.value} onChange={field.onChange} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="postIds"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.posts')}</FormLabel>
                  <FormControl>
                    <RemoteSelect url="/posts/options" multiple value={field.value} onChange={field.onChange} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="roleIds"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.roles')}</FormLabel>
                  <FormControl>
                    <RemoteSelect url="/roles/options" params={{ scene: 'assign' }} multiple value={field.value} onChange={field.onChange} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="region"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.region')}</FormLabel>
                  <RegionSelect level={3} value={field.value} onChange={field.onChange} />
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="enabled"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('user.form.status')}</FormLabel>
                  <div className="flex min-h-11 items-center md:min-h-9">
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
                    </FormControl>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <FormField
                control={form.control}
                name="avatar"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('user.form.avatar')}</FormLabel>
                    <Uploader kind="image" crop={{ aspect: 1 }} visibility="public" value={field.value} onChange={field.onChange} />
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="remark"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('user.form.remark')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={3} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
