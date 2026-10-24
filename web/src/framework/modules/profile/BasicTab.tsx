import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DictSelect } from '@/framework/components/DictSelect'
import { FormLayout } from '@/framework/components/FormLayout'
import { RegionSelect } from '@/framework/components/RegionSelect'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import { useProfile, useUpdateProfile } from './api'

interface Values {
  avatar: string
  nickname: string
  gender: string
  email: string
  region: { province?: string; city?: string; district?: string }
  bio: string
}

const toFiles = (url: string): UploadedFile[] =>
  url ? [{ id: url, url, name: 'avatar', size: 0, type: 'image/*' }] : []

export function BasicTab() {
  const { t } = useTranslation()
  const { data: profile } = useProfile()
  const update = useUpdateProfile()

  const schema = useMemo(
    () =>
      z.object({
        avatar: z.string(),
        nickname: z.string().trim().min(1, t('profile.basic.nicknameRequired')).max(32),
        gender: z.string(),
        email: z.string().trim().email(t('profile.basic.emailInvalid')).or(z.literal('')),
        region: z.object({ province: z.string().optional(), city: z.string().optional(), district: z.string().optional() }),
        bio: z.string().max(200),
      }),
    [t],
  )

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { avatar: '', nickname: '', gender: '0', email: '', region: {}, bio: '' },
  })

  useEffect(() => {
    if (!profile) return
    form.reset({
      avatar: profile.avatar ?? '',
      nickname: profile.nickname,
      gender: profile.gender,
      email: profile.email ?? '',
      region: { province: profile.province, city: profile.city, district: profile.district },
      bio: profile.bio ?? '',
    })
  }, [profile, form])

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      await update.mutateAsync({
        avatar: v.avatar,
        nickname: v.nickname.trim(),
        gender: v.gender as '0' | '1' | '2',
        email: v.email.trim(),
        province: v.region.province,
        city: v.region.city,
        district: v.region.district,
        bio: v.bio,
      })
      toast.success(t('profile.basic.saved'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <Form {...form}>
      <FormLayout onSubmit={onSubmit} className="max-w-2xl gap-5">
        <FormField
          control={form.control}
          name="avatar"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('profile.basic.avatar')}</FormLabel>
              <Uploader
                kind="image"
                visibility="public"
                crop={{ aspect: 1, width: 256, height: 256 }}
                value={toFiles(field.value)}
                onChange={(files) => field.onChange(files[0]?.url ?? '')}
              />
              <FormDescription>{t('profile.basic.avatarHint')}</FormDescription>
            </FormItem>
          )}
        />
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
          <div className="flex flex-col gap-2">
            <Label htmlFor="pf-username">{t('profile.basic.username')}</Label>
            <Input id="pf-username" value={profile?.username ?? ''} readOnly disabled />
          </div>
          <FormField
            control={form.control}
            name="nickname"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('profile.basic.nickname')}</FormLabel>
                <FormControl>
                  <Input {...field} />
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
                <FormLabel>{t('profile.basic.gender')}</FormLabel>
                <DictSelect dict="sys_gender" value={field.value} onChange={field.onChange} allowClear={false} />
              </FormItem>
            )}
          />
          <div className="flex flex-col gap-2">
            <Label htmlFor="pf-phone">{t('profile.basic.phone')}</Label>
            <Input id="pf-phone" value={profile?.phone ?? ''} readOnly disabled inputMode="tel" />
            <p className="text-sm text-muted-foreground">{t('profile.basic.phoneHint')}</p>
          </div>
          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('profile.basic.email')}</FormLabel>
                <FormControl>
                  <Input {...field} type="email" inputMode="email" />
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
                <FormLabel>{t('profile.basic.region')}</FormLabel>
                <RegionSelect level={3} value={field.value} onChange={field.onChange} />
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name="bio"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('profile.basic.bio')}</FormLabel>
              <FormControl>
                <Textarea {...field} rows={4} placeholder={t('profile.basic.bioPlaceholder')} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div>
          <Button type="submit" className="w-full sm:w-auto" disabled={update.isPending}>
            {t('common.save')}
          </Button>
        </div>
      </FormLayout>
    </Form>
  )
}
