import { icons, type LucideProps } from 'lucide-react'

function toPascal(name: string) {
  return name
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
    .join('')
}

/** 按 lucide 图标名渲染（支持 PascalCase 与 kebab-case），找不到时渲染圆点 */
export function DynamicIcon({ name, ...props }: { name?: string } & LucideProps) {
  const Icon = name ? (icons as Record<string, React.ComponentType<LucideProps>>)[name.includes('-') || /^[a-z]/.test(name) ? toPascal(name) : name] : undefined
  if (!Icon) return <span className="inline-block size-1.5 rounded-full bg-current" aria-hidden="true" />
  return <Icon aria-hidden="true" {...props} />
}
