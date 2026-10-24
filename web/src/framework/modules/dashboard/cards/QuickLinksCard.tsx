import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePermission } from '@/framework/auth/usePermission'
import { DynamicIcon } from '@/framework/layout/DynamicIcon'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'

interface QuickLink {
  key: string
  icon: string
  path: string
  perm?: string
}

const QUICK_LINKS: QuickLink[] = [
  { key: 'user', icon: 'Users', path: '/system/user', perm: 'user:list' },
  { key: 'role', icon: 'ShieldCheck', path: '/system/role', perm: 'role:list' },
  { key: 'dept', icon: 'Network', path: '/system/dept', perm: 'dept:list' },
  { key: 'dict', icon: 'BookA', path: '/system/dict', perm: 'dict:list' },
  { key: 'notice', icon: 'Megaphone', path: '/content/notice', perm: 'notice:list' },
  { key: 'message', icon: 'Bell', path: '/message', perm: 'message:list' },
  { key: 'file', icon: 'FolderOpen', path: '/content/file', perm: 'file:list' },
  { key: 'operLog', icon: 'ScrollText', path: '/monitor/oper-log', perm: 'operLog:list' },
]

export default function QuickLinksCard() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const links = QUICK_LINKS.filter((l) => !l.perm || has(l.perm))
  return (
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-base font-medium">{t('dashboard.quickLinks')}</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-2 2xl:grid-cols-4">
          {links.map((l) => (
            <li key={l.key}>
              <Link
                to={l.path}
                className="flex min-h-20 flex-col items-center justify-center gap-2 rounded-lg border border-border p-3 text-center text-sm transition-colors hover:border-primary hover:bg-primary/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none"
              >
                <DynamicIcon name={l.icon} className="size-6 text-primary" />
                <span className="truncate">{t(`dashboard.link.${l.key}`)}</span>
              </Link>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}
