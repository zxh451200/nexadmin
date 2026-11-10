import { useState } from 'react'
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import {
  useTotpConfirm,
  useTotpDisable,
  useTotpEnroll,
  useTotpRegenRecovery,
  useTotpStatus,
  type TotpConfirmResult,
} from './api'

// 恢复码展示（一次性）：点击复制单枚。生成后必须明确「已保存」才会关掉弹窗。
function RecoveryCodesDialog({
  codes,
  open,
  onClose,
  title,
  hint,
}: {
  codes: string[]
  open: boolean
  onClose: () => void
  title: string
  hint: string
}) {
  const { t } = useTranslation()
  const copy = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code)
      toast.success(t('profile.twofactor.copied'))
    } catch {
      // 剪贴板权限被拒：用户仍可手选复制
    }
  }
  return (
    <ResponsiveDialog open={open} onOpenChange={(v) => !v && onClose()} title={title} size="md">
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">{hint}</p>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {codes.map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => void copy(c)}
              className="flex items-center justify-between gap-2 rounded-md border border-border px-2.5 py-1.5 font-mono text-sm hover:bg-accent"
            >
              {c}
              <Copy className="size-3.5 text-muted-foreground" aria-hidden="true" />
            </button>
          ))}
        </div>
        <Button onClick={onClose}>{t('profile.tokens.reveal.saved')}</Button>
      </div>
    </ResponsiveDialog>
  )
}

export function TwofactorTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const status = useTotpStatus()
  const enroll = useTotpEnroll()
  const confirmPairing = useTotpConfirm()
  const regen = useTotpRegenRecovery()
  const disable = useTotpDisable()

  const [enrollOpen, setEnrollOpen] = useState(false)
  const [secret, setSecret] = useState('')
  const [uri, setUri] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [recovery, setRecovery] = useState<TotpConfirmResult | null>(null)
  const [recoveryTitle, setRecoveryTitle] = useState('')

  const startEnroll = async () => {
    try {
      const res = await enroll.mutateAsync()
      setSecret(res.secret)
      setUri(res.uri)
      setCode('')
      setEnrollOpen(true)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const confirmPair = async () => {
    if (!/^\d{6}$/.test(code.trim())) {
      toast.error(t('profile.twofactor.required'))
      return
    }
    try {
      const res = await confirmPairing.mutateAsync(code.trim())
      setEnrollOpen(false)
      setCode('')
      setRecoveryTitle(t('profile.twofactor.confirmDone'))
      setRecovery(res)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onRegen = async () => {
    try {
      const res = await regen.mutateAsync()
      setRecoveryTitle(t('profile.twofactor.regenDone'))
      setRecovery(res)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onDisable = async () => {
    if (!password) {
      toast.error(t('profile.password.oldRequired'))
      return
    }
    if (!(await confirm({ destructive: true, description: t('profile.twofactor.disableConfirm') }))) return
    try {
      await disable.mutateAsync({ code: code.trim(), password })
      setPassword('')
      setCode('')
      toast.success(t('profile.twofactor.disabled'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  if (status.isLoading) return null
  const enabled = status.data?.enabled

  return (
    <div className="flex max-w-md flex-col gap-5">
      <p className="text-sm text-muted-foreground">{t('profile.twofactor.description')}</p>
      <div className="flex items-center gap-2">
        {enabled ? <Badge variant="success">{t('profile.twofactor.enabledOn')}</Badge> : <Badge variant="outline">{t('profile.twofactor.enabledOff')}</Badge>}
        {enabled ? (
          <span className="text-xs text-muted-foreground">
            {t('profile.twofactor.recoveryLeft', { count: status.data?.recoveryLeft ?? 0 })}
          </span>
        ) : null}
      </div>

      {enabled ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void onRegen()} disabled={regen.isPending}>
              {t('profile.twofactor.regen')}
            </Button>
          </div>
          {/* 解绑前置两样：当前密码 + 验证码（防被盗会话直接关掉二次验证） */}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="totp-disable-code">{t('profile.twofactor.codeLabel')}</Label>
            <Input id="totp-disable-code" inputMode="numeric" maxLength={6} value={code} onChange={(e) => setCode(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="totp-disable-password">{t('profile.password.old')}</Label>
            <Input
              id="totp-disable-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div>
            <Button variant="outline" className="text-destructive" onClick={() => void onDisable()} disabled={disable.isPending}>
              {t('profile.twofactor.disable')}
            </Button>
          </div>
        </div>
      ) : (
        <div>
          <Button onClick={() => void startEnroll()} disabled={enroll.isPending}>
            {t('profile.twofactor.startEnroll')}
          </Button>
        </div>
      )}

      <ResponsiveDialog
        open={enrollOpen}
        onOpenChange={(v) => !v && setEnrollOpen(false)}
        title={t('profile.twofactor.startEnroll')}
        size="sm"
        footer={
          <Button onClick={() => void confirmPair()} disabled={confirmPairing.isPending}>
            {t('profile.twofactor.confirm')}
          </Button>
        }
      >
        <div className="flex flex-col gap-4 text-sm">
          <div className="flex flex-col gap-1.5">
            <Label>{t('profile.twofactor.secretLabel')}</Label>
            <code className="num break-all rounded-md bg-muted px-2 py-1.5">{secret}</code>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>{t('profile.twofactor.uriLabel')}</Label>
            <code className="break-all rounded-md bg-muted px-2 py-1.5 text-xs">{uri}</code>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="totp-confirm-code">{t('profile.twofactor.codeLabel')}</Label>
            <Input id="totp-confirm-code" inputMode="numeric" maxLength={6} value={code} onChange={(e) => setCode(e.target.value)} />
          </div>
        </div>
      </ResponsiveDialog>

      <RecoveryCodesDialog
        codes={recovery?.recoveryCodes ?? []}
        open={recovery !== null}
        onClose={() => setRecovery(null)}
        title={recoveryTitle}
        hint={t('profile.twofactor.saveHint')}
      />
    </div>
  )
}
