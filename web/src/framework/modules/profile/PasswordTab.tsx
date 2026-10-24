import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/framework/ui/button'
import { FormLayout } from '@/framework/components/FormLayout'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { useChangePassword } from './api'

interface Values {
  oldPassword: string
  newPassword: string
  confirm: string
}

export function PasswordTab() {
  const { t } = useTranslation()
  const change = useChangePassword()

  const schema = useMemo(
    () =>
      z
        .object({
          oldPassword: z.string().min(1, t('profile.password.oldRequired')),
          newPassword: z.string().min(1, t('common.required')),
          confirm: z.string(),
        })
        .refine((v) => v.newPassword === v.confirm, { path: ['confirm'], message: t('profile.password.mismatch') })
        .refine((v) => v.newPassword !== v.oldPassword, { path: ['newPassword'], message: t('profile.password.sameAsOld') }),
    [t],
  )

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { oldPassword: '', newPassword: '', confirm: '' },
  })

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      await change.mutateAsync({ oldPassword: v.oldPassword, newPassword: v.newPassword })
      form.reset()
      toast.success(t('profile.password.done'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <Form {...form}>
      <FormLayout onSubmit={onSubmit} className="max-w-md gap-5">
        <FormField
          control={form.control}
          name="oldPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('profile.password.old')}</FormLabel>
              <FormControl>
                <Input {...field} type="password" autoComplete="current-password" />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="newPassword"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('profile.password.new')}</FormLabel>
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
              <FormLabel>{t('profile.password.confirm')}</FormLabel>
              <FormControl>
                <Input {...field} type="password" autoComplete="new-password" />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div>
          <Button type="submit" className="w-full sm:w-auto" disabled={change.isPending}>
            {t('profile.password.submit')}
          </Button>
        </div>
      </FormLayout>
    </Form>
  )
}
