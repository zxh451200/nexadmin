import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeSVG } from 'qrcode.react'
import { ShieldCheck } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/framework/request/http'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'

interface TotpStatus { enrolled: boolean; enabled: boolean }

export default function TotpPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const status = useQuery({ queryKey: ['totp', 'status'], queryFn: () => api.get<TotpStatus>('/plugins/totp/status') })
  const [secret, setSecret] = useState('')
  const [uri, setUri] = useState('')
  const [code, setCode] = useState('')

  const enroll = useMutation({
    mutationFn: () => api.post<{ secret: string; uri: string }>('/plugins/totp/enroll'),
    onSuccess: (d) => { setSecret(d.secret); setUri(d.uri) },
  })
  const confirm = useMutation({
    mutationFn: () => api.post('/plugins/totp/confirm', { code }),
    onSuccess: () => { toast.success(t('plugin_totp.confirmed')); setSecret(''); setUri(''); setCode(''); void qc.invalidateQueries({ queryKey: ['totp'] }) },
    onError: () => toast.error(t('plugin_totp.codeWrong')),
  })
  const disable = useMutation({
    mutationFn: () => api.post('/plugins/totp/disable', { code }),
    onSuccess: () => { toast.success(t('plugin_totp.disabled')); setCode(''); void qc.invalidateQueries({ queryKey: ['totp'] }) },
    onError: () => toast.error(t('plugin_totp.codeWrong')),
  })

  const enabled = status.data?.enabled ?? false

  return (
    <PageContainer>
      <PageHeader title={t('plugin_totp.title')} description={t('plugin_totp.description')} />
      <Card className="max-w-md space-y-4 p-6">
        <div className="flex items-center gap-2">
          <ShieldCheck className="size-5 text-muted-foreground" aria-hidden="true" />
          <span className="text-sm font-medium">{t('plugin_totp.statusLabel')}</span>
          <Badge variant={enabled ? 'success' : 'secondary'}>{enabled ? t('plugin_totp.on') : t('plugin_totp.off')}</Badge>
        </div>

        {!enabled ? (
          <div className="space-y-3">
            {!secret ? (
              <Button onClick={() => enroll.mutate()} disabled={enroll.isPending}>
                {t('plugin_totp.startEnroll')}
              </Button>
            ) : (
              <>
                <div className="space-y-1 text-sm">
                  <p className="text-muted-foreground">{t('plugin_totp.uriLabel')}</p>
                  <div className="flex justify-center py-2">
                    <QRCodeSVG value={uri} size={160} />
                  </div>
                  <p className="text-muted-foreground">{t('plugin_totp.secretLabel')}</p>
                  <code className="block rounded bg-muted px-2 py-1 font-mono text-xs break-all">{secret}</code>
                </div>
                <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder={t('plugin_totp.codePlaceholder')} maxLength={6} />
                <Button onClick={() => confirm.mutate()} disabled={confirm.isPending || code.length !== 6}>
                  {t('plugin_totp.confirm')}
                </Button>
              </>
            )}
          </div>
        ) : (
          <div className="space-y-3">
            <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder={t('plugin_totp.codePlaceholder')} maxLength={6} />
            <Button variant="destructive" onClick={() => disable.mutate()} disabled={disable.isPending || code.length !== 6}>
              {t('plugin_totp.disable')}
            </Button>
          </div>
        )}
      </Card>
    </PageContainer>
  )
}
