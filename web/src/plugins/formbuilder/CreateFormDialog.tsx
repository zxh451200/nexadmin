import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import { useCreateForm, type FormDetail } from './api'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (form: FormDetail) => void
}

/** 新建表单：只填名称与说明，创建后进入设计器 */
export function CreateFormDialog({ open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation()
  const create = useCreateForm()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')

  useEffect(() => {
    if (open) {
      setName('')
      setDescription('')
    }
  }, [open])

  const valid = name.trim() !== ''
  const submit = async () => {
    if (!valid) return
    try {
      const form = await create.mutateAsync({ name: name.trim(), description })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
      onCreated(form)
    } catch {
      // 服务端的错误提示已由请求层弹出
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_formbuilder.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="fb-create-form" disabled={!valid || create.isPending}>
            {create.isPending ? t('common.loading') : t('common.create')}
          </Button>
        </>
      }
    >
      <FormLayout
        id="fb-create-form"
        showRequiredHint
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="fb-create-name">{t('plugin_formbuilder.fields.name')}</Label>
          <Input id="fb-create-name" value={name} maxLength={100} autoFocus onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="fb-create-desc">{t('plugin_formbuilder.fields.description')}</Label>
          <Textarea id="fb-create-desc" rows={3} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
        </div>
      </FormLayout>
    </ResponsiveDialog>
  )
}
