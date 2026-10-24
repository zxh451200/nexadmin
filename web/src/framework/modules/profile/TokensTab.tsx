import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import { CodeBlock, RevealDialog } from '../openapi/RevealDialog'
import { useAccessTokens, useCreateAccessToken, useRevokeAccessToken, type AccessToken, type AccessTokenCreated, type TokenScope } from './api'

const DAY = 86400000
const SOON_DAYS = 7

/** 有效期选项：值为天数，never = 永不过期 */
const EXPIRY_OPTIONS = ['30', '90', '365', 'never'] as const
type Expiry = (typeof EXPIRY_OPTIONS)[number]

function tokenState(tk: AccessToken): 'expired' | 'soon' | 'active' {
  if (!tk.expiresAt) return 'active'
  const left = new Date(tk.expiresAt).getTime() - Date.now()
  if (left <= 0) return 'expired'
  return left <= SOON_DAYS * DAY ? 'soon' : 'active'
}

function CreateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onCreated: (tk: AccessTokenCreated) => void
}) {
  const { t } = useTranslation()
  const create = useCreateAccessToken()
  const [name, setName] = useState('')
  const [scope, setScope] = useState<TokenScope>('read')
  const [expiry, setExpiry] = useState<Expiry>('30')
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      setName('')
      setScope('read')
      setExpiry('30')
      setError('')
    }
  }, [open])

  const onSubmit = async () => {
    const n = name.trim()
    if (!n) return setError(t('common.required'))
    if (n.length > 50) return setError(t('profile.tokens.nameTooLong'))
    try {
      const res = await create.mutateAsync({
        name: n,
        scope,
        expiresAt: expiry === 'never' ? undefined : new Date(Date.now() + Number(expiry) * DAY).toISOString(),
      })
      onOpenChange(false)
      if (typeof res.token === 'string') onCreated(res)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={t('profile.tokens.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={create.isPending} onClick={() => void onSubmit()}>
            {create.isPending ? t('common.loading') : t('profile.tokens.generate')}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4 [&_input]:text-base md:[&_input]:text-sm"
        onSubmit={(e) => {
          e.preventDefault()
          void onSubmit()
        }}
        noValidate
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="token-name">{t('profile.tokens.fields.name')}</Label>
          <Input
            id="token-name"
            value={name}
            maxLength={50}
            aria-invalid={!!error}
            aria-describedby={error ? 'token-name-err' : undefined}
            placeholder={t('profile.tokens.namePlaceholder')}
            onChange={(e) => {
              setName(e.target.value)
              setError('')
            }}
          />
          {error ? (
            <p id="token-name-err" className="text-sm text-destructive">
              {error}
            </p>
          ) : null}
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="token-scope">{t('profile.tokens.fields.scope')}</Label>
          <Select value={scope} onValueChange={(v) => setScope(v as TokenScope)}>
            <SelectTrigger id="token-scope">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="read">{t('profile.tokens.scope.read')}</SelectItem>
              <SelectItem value="write">{t('profile.tokens.scope.write')}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t(`profile.tokens.scopeHint.${scope}`)}</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="token-expiry">{t('profile.tokens.fields.expiresAt')}</Label>
          <Select value={expiry} onValueChange={(v) => setExpiry(v as Expiry)}>
            <SelectTrigger id="token-expiry">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {EXPIRY_OPTIONS.map((o) => (
                <SelectItem key={o} value={o}>
                  {t(`profile.tokens.expiry.${o}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {expiry === 'never' ? (
            <p role="alert" className="rounded-md border border-warning/40 bg-warning/10 p-3 text-xs text-foreground">
              {t('profile.tokens.neverWarning')}
            </p>
          ) : null}
        </div>
      </form>
    </ResponsiveDialog>
  )
}

export function TokensTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useAccessTokens()
  const revoke = useRevokeAccessToken()
  const [createOpen, setCreateOpen] = useState(false)
  const [created, setCreated] = useState<AccessTokenCreated | null>(null)
  // 关闭后清掉明文，避免残留在内存状态里被再次渲染
  const [shown, setShown] = useState(false)

  const onRevoke = async (tk: AccessToken) => {
    if (!(await confirm({ destructive: true, title: t('profile.tokens.revoke'), description: t('profile.tokens.revokeConfirm', { name: tk.name }) }))) return
    try {
      await revoke.mutateAsync(tk.id)
      toast.success(t('profile.tokens.revoked'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<AccessToken>[] = [
    {
      key: 'name',
      title: t('profile.tokens.fields.name'),
      mobile: 'title',
      render: (tk) => <span className="font-medium text-foreground">{tk.name}</span>,
    },
    {
      key: 'scope',
      title: t('profile.tokens.fields.scope'),
      mobile: 'sub',
      render: (tk) => <Badge variant={tk.scope === 'write' ? 'warning' : 'info'}>{t(`profile.tokens.scope.${tk.scope}`)}</Badge>,
    },
    {
      key: 'prefix',
      title: t('profile.tokens.fields.prefix'),
      render: (tk) => <code className="font-mono text-xs">{tk.prefix}…</code>,
    },
    {
      key: 'lastUsedAt',
      title: t('profile.tokens.fields.lastUsedAt'),
      render: (tk) =>
        tk.lastUsedAt ? <span className="num">{fmtDateTime(tk.lastUsedAt, i18n.language)}</span> : <span className="text-muted-foreground">{t('profile.tokens.neverUsed')}</span>,
    },
    {
      key: 'expiresAt',
      title: t('profile.tokens.fields.expiresAt'),
      render: (tk) => (tk.expiresAt ? <span className="num">{fmtDateTime(tk.expiresAt, i18n.language)}</span> : <span className="text-muted-foreground">{t('profile.tokens.expiry.never')}</span>),
    },
    {
      key: 'status',
      title: t('common.status'),
      render: (tk) => {
        const s = tokenState(tk)
        return (
          <Badge variant={s === 'expired' ? 'destructive' : s === 'soon' ? 'warning' : 'success'}>{t(`profile.tokens.state.${s}`)}</Badge>
        )
      },
    },
  ]

  const origin = window.location.origin
  const curl = created
    ? `curl -H "Authorization: Bearer ${created.token}" \\\n  ${origin}/api/v1/profile`
    : ''

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm text-muted-foreground">{t('profile.tokens.description')}</p>
        <Button type="button" className="shrink-0" onClick={() => setCreateOpen(true)}>
          <Plus aria-hidden="true" />
          {t('profile.tokens.create')}
        </Button>
      </div>
      <DataTable
        columns={columns}
        data={data ?? []}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        emptyText={t('profile.tokens.empty')}
        rowActions={(tk) => (
          <Button variant="ghost" size="icon" className="text-destructive" aria-label={`${t('profile.tokens.revoke')}: ${tk.name}`} onClick={() => void onRevoke(tk)}>
            <Trash2 aria-hidden="true" />
          </Button>
        )}
      />
      <CreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(tk) => {
          setCreated(tk)
          setShown(true)
        }}
      />
      <RevealDialog
        open={shown && !!created}
        onOpenChange={(o) => {
          if (!o) {
            setShown(false)
            setCreated(null)
          }
        }}
        title={t('profile.tokens.reveal.title')}
        description={created?.name}
        warning={t('profile.tokens.reveal.onceHint')}
        closeText={t('profile.tokens.reveal.saved')}
        fields={[{ label: t('profile.tokens.reveal.token'), value: created?.token ?? '' }]}
      >
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-medium text-foreground">{t('profile.tokens.reveal.usage')}</h3>
          <CodeBlock code={curl} label={t('profile.tokens.reveal.copyCurl')} />
        </section>
      </RevealDialog>
    </div>
  )
}
