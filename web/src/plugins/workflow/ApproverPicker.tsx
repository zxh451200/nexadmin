// ApproverPicker：审批人选择器。复用框架 RemoteSelect（/users + 按 ID 回显昵称），
// 对引擎而言审批人就是宿主主体 ID 字符串。
import { useTranslation } from 'react-i18next'

import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { resolveUserLabel } from '@/framework/modules/user/api'

export function ApproverPicker({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string[]
  onChange: (ids: string[]) => void
  placeholder?: string
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <RemoteSelect
      url="/users"
      labelKey="nickname"
      valueKey="id"
      multiple
      value={value}
      onChange={(v) => onChange(Array.isArray(v) ? v : v == null || v === '' ? [] : [String(v)])}
      resolveLabel={resolveUserLabel}
      placeholder={placeholder ?? t('plugin_workflow.approversPlaceholder')}
      className={className}
    />
  )
}
