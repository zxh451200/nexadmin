import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { useResetPassword, type User } from './api'

const FORM_ID = 'user-reset-password-form'

export interface ResetPasswordDialogProps {
  user: User | null
  onOpenChange: (open: boolean) => void
}

export function ResetPasswordDialog({ user, onOpenChange }: ResetPasswordDialogProps) {
  const { t } = useTranslation()
  const reset = useResetPassword()
  const open = !!user

  const schema = useMemo(
    () =>
      z
        .object({
          password: z.string().min(1, t('common.required')),
          confirm: z.string(),
        })
        .refine((v) => v.password === v.confirm, { path: ['confirm'], message: t('user.v.passwordMismatch') }),
    [t],
  )
  type FormValues = z.infer<typeof schema>

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: { password: '', confirm: '' } })
  useEffect(() => {
    if (open) form.reset({ password: '', confirm: '' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, user?.id])

  const onSubmit = form.handleSubmit(async (v) => {
    if (!user) return
    try {
      await reset.mutateAsync({ id: user.id, password: v.password })
      toast.success(t('user.resetPassword.success'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={t('user.resetPassword.title')}
      description={t('user.resetPassword.description', { name: user?.nickname ?? '' })}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={reset.isPending}>
            {reset.isPending ? t('common.loading') : t('common.confirm')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id={FORM_ID} onSubmit={onSubmit}>
          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('user.resetPassword.password')}</FormLabel>
                <FormControl>
                  <Input {...field} type="password" autoComplete="new-password" />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="confirm"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('user.resetPassword.confirm')}</FormLabel>
                <FormControl>
                  <Input {...field} type="password" autoComplete="new-password" />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
