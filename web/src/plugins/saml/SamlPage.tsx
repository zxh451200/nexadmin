import { useState } from 'react'
import { KeyRound, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'

interface SamlSource {
  id: string
  name: string
  enabled: boolean
  idpMetadata: string
  attrUsername: string
  attrEmail: string
  attrNickname: string
  autoCreate: boolean
  matchExisting: boolean
  defaultRole: string
}

const empty: Partial<SamlSource> = { name: '', enabled: false, idpMetadata: '', attrUsername: 'uid', attrEmail: 'mail', attrNickname: 'displayName' }

export default function SamlPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['saml', 'sources'], queryFn: () => api.get<SamlSource[]>('/plugins/saml/sources') })
  const [editing, setEditing] = useState<Partial<SamlSource> | null>(null)

  const save = useMutation({
    mutationFn: (f: Partial<SamlSource>) =>
      f.id ? api.put(`/plugins/saml/sources/${f.id}`, f) : api.post('/plugins/saml/sources', f),
    onSuccess: () => {
      toast.success(t('common.operateSuccess'))
      setEditing(null)
      void qc.invalidateQueries({ queryKey: ['saml'] })
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/plugins/saml/sources/${id}`),
    onSuccess: () => {
      toast.success(t('common.operateSuccess'))
      void qc.invalidateQueries({ queryKey: ['saml'] })
    },
  })

  const urls = (id: string) => `${window.location.origin}/api/v1/auth/saml/${id}`

  return (
    <PageContainer>
      <PageHeader title={t('plugin_saml.title')} description={t('plugin_saml.description')} />
      <div className="mb-4 flex justify-end">
        <Button onClick={() => setEditing({ ...empty })}>
          <Plus aria-hidden="true" />
          {t('plugin_saml.add')}
        </Button>
      </div>
      <div className="space-y-3">
        {(list.data ?? []).map((s) => (
          <Card key={s.id} className="space-y-2 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <KeyRound className="size-4 text-muted-foreground" aria-hidden="true" />
              <span className="font-medium">{s.name}</span>
              <Badge variant={s.enabled ? 'success' : 'secondary'}>
                {s.enabled ? t('plugin_saml.on') : t('plugin_saml.off')}
              </Badge>
              <div className="ms-auto flex gap-2">
                <Button size="sm" variant="outline" onClick={() => setEditing(s)}>
                  {t('common.edit')}
                </Button>
                <Button size="sm" variant="outline" onClick={() => remove.mutate(s.id)}>
                  <Trash2 aria-hidden="true" />
                </Button>
              </div>
            </div>
            <div className="space-y-1 text-xs text-muted-foreground">
              <div className="break-all">
                ACS: <code>{urls(s.id)}/acs</code>
              </div>
              <div className="break-all">
                Metadata: <code>{urls(s.id)}/metadata</code>
              </div>
              <div className="break-all">
                Login: <code>{urls(s.id)}/login</code>
              </div>
            </div>
          </Card>
        ))}
        {list.data?.length === 0 ? <p className="text-sm text-muted-foreground">{t('plugin_saml.empty')}</p> : null}
      </div>

      <ResponsiveDialog
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing?.id ? t('plugin_saml.edit') : t('plugin_saml.add')}
        footer={
          <>
            <Button variant="outline" onClick={() => setEditing(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={save.isPending} onClick={() => editing && save.mutate(editing)}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        {editing ? (
          <div className="space-y-3">
            <Label className="flex flex-col gap-1 text-sm">
              {t('plugin_saml.name')}
              <Input value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
            </Label>
            <Label className="flex items-center gap-2 text-sm">
              {t('plugin_saml.enabled')}
              <Switch checked={!!editing.enabled} onCheckedChange={(v) => setEditing({ ...editing, enabled: v })} />
            </Label>
            <Label className="flex flex-col gap-1 text-sm">
              {t('plugin_saml.idpMetadata')}
              <Textarea
                rows={6}
                className="font-mono text-xs"
                value={editing.idpMetadata ?? ''}
                onChange={(e) => setEditing({ ...editing, idpMetadata: e.target.value })}
                placeholder="<EntityDescriptor …>"
              />
            </Label>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <Label className="flex flex-col gap-1 text-sm">
                Username
                <Input value={editing.attrUsername ?? ''} onChange={(e) => setEditing({ ...editing, attrUsername: e.target.value })} />
              </Label>
              <Label className="flex flex-col gap-1 text-sm">
                Email
                <Input value={editing.attrEmail ?? ''} onChange={(e) => setEditing({ ...editing, attrEmail: e.target.value })} />
              </Label>
              <Label className="flex flex-col gap-1 text-sm">
                Nickname
                <Input value={editing.attrNickname ?? ''} onChange={(e) => setEditing({ ...editing, attrNickname: e.target.value })} />
              </Label>
            </div>
            <div className="flex flex-wrap gap-4 text-sm">
              <Label className="flex items-center gap-2">
                {t('plugin_saml.autoCreate')}
                <Switch checked={!!editing.autoCreate} onCheckedChange={(v) => setEditing({ ...editing, autoCreate: v })} />
              </Label>
              <Label className="flex items-center gap-2">
                {t('plugin_saml.matchExisting')}
                <Switch checked={!!editing.matchExisting} onCheckedChange={(v) => setEditing({ ...editing, matchExisting: v })} />
              </Label>
            </div>
            <Label className="flex flex-col gap-1 text-sm">
              {t('plugin_saml.defaultRole')}
              <Input value={editing.defaultRole ?? ''} onChange={(e) => setEditing({ ...editing, defaultRole: e.target.value })} placeholder="guest" />
            </Label>
          </div>
        ) : null}
      </ResponsiveDialog>
    </PageContainer>
  )
}
