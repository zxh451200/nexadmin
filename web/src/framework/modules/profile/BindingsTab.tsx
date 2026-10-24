import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { fmtDateTime } from '../oper-log/format'
import { useBindPhone, useBindProvider, useBindings, useSendSmsCode, useUnbind } from './api'

function PhoneDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [left, setLeft] = useState(0)
  const send = useSendSmsCode()
  const bind = useBindPhone()

  useEffect(() => {
    if (open) {
      setPhone('')
      setCode('')
      setLeft(0)
    }
  }, [open])

  useEffect(() => {
    if (left <= 0) return
    const timer = setTimeout(() => setLeft((n) => n - 1), 1000)
    return () => clearTimeout(timer)
  }, [left])

  const phoneOk = /^1\d{10}$/.test(phone)
  const codeOk = /^\d{6}$/.test(code)

  const onSend = async () => {
    if (!phoneOk) return toast.error(t('profile.bindings.phoneInvalid'))
    try {
      await send.mutateAsync(phone)
      setLeft(60)
      toast.success(t('profile.bindings.codeSent'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onSubmit = async () => {
    if (!phoneOk) return toast.error(t('profile.bindings.phoneInvalid'))
    if (!codeOk) return toast.error(t('profile.bindings.codeInvalid'))
    try {
      await bind.mutateAsync({ phone, code })
      toast.success(t('profile.bindings.bindDone'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('profile.bindings.phoneDialog')}
      size="sm"
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={bind.isPending} onClick={onSubmit}>
            {t('common.confirm')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="bind-phone">{t('profile.bindings.phoneInput')}</Label>
          <Input
            id="bind-phone"
            type="tel"
            inputMode="numeric"
            maxLength={11}
            autoComplete="tel"
            value={phone}
            onChange={(e) => setPhone(e.target.value.replace(/\D/g, ''))}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="bind-code">{t('profile.bindings.codeInput')}</Label>
          <div className="flex gap-2">
            <Input
              id="bind-code"
              className="min-w-0 flex-1"
              inputMode="numeric"
              maxLength={6}
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
            />
            <Button type="button" variant="outline" className="shrink-0" disabled={left > 0 || send.isPending} onClick={onSend}>
              {left > 0 ? t('profile.bindings.resend', { count: left }) : t('profile.bindings.sendCode')}
            </Button>
          </div>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

export function BindingsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data } = useBindings()
  const bindProvider = useBindProvider()
  const unbind = useUnbind()
  const [phoneOpen, setPhoneOpen] = useState(false)

  const onUnbind = async (key: string, name: string) => {
    if (!(await confirm({ destructive: true, description: t('profile.bindings.confirmUnbind', { name }) }))) return
    try {
      await unbind.mutateAsync(key)
      toast.success(t('profile.bindings.unbindDone'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  // 跳转到第三方授权页；回来后由 /login/oauth/:provider 页完成绑定并回到本页
  const onBind = async (key: string) => {
    try {
      await bindProvider.mutateAsync(key)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const row = 'flex flex-col gap-3 rounded-lg border border-border bg-card p-4 sm:flex-row sm:items-center sm:justify-between'

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <section className="flex flex-col gap-3">
        <h3 className="text-sm font-medium text-foreground">{t('profile.bindings.phoneTitle')}</h3>
        <div className={row}>
          <div className="min-w-0">
            <p className="text-sm text-foreground">
              {data?.phone ? t('profile.bindings.phoneBound', { phone: data.phone }) : t('profile.bindings.phoneUnbound')}
            </p>
          </div>
          <div className="flex shrink-0 flex-wrap gap-2">
            <Button type="button" variant="outline" onClick={() => setPhoneOpen(true)}>
              {data?.phone ? t('profile.bindings.change') : t('profile.bindings.bind')}
            </Button>
            {data?.phone ? (
              <Button type="button" variant="ghost" className="text-destructive" onClick={() => onUnbind('phone', t('profile.bindings.phoneTitle'))}>
                {t('profile.bindings.unbind')}
              </Button>
            ) : null}
          </div>
        </div>
      </section>
      <section className="flex flex-col gap-3">
        <h3 className="text-sm font-medium text-foreground">{t('profile.bindings.thirdParty')}</h3>
        {(data?.providers ?? []).map((p) => (
          <div key={p.key} className={row}>
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-foreground">{p.name}</span>
                <Badge variant={p.bound ? 'success' : 'secondary'}>
                  {p.bound ? t('profile.bindings.bound') : t('profile.bindings.notBound')}
                </Badge>
              </div>
              {p.bound ? (
                <span className="truncate text-xs text-muted-foreground">
                  {t('profile.bindings.boundAs', { name: p.externalName ?? '-', time: fmtDateTime(p.boundAt, i18n.language) })}
                </span>
              ) : null}
            </div>
            {p.bound ? (
              <Button type="button" variant="outline" className="shrink-0" onClick={() => onUnbind(p.key, p.name)}>
                {t('profile.bindings.unbind')}
              </Button>
            ) : (
              <Button type="button" className="shrink-0" disabled={bindProvider.isPending} onClick={() => void onBind(p.key)}>
                {t('profile.bindings.bind')}
              </Button>
            )}
          </div>
        ))}
      </section>
      <PhoneDialog open={phoneOpen} onOpenChange={setPhoneOpen} />
    </div>
  )
}
