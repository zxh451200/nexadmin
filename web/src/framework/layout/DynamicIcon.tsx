import { icons, type LucideProps } from 'lucide-react'
import * as lucide from 'lucide-react'

function toPascal(name: string) {
  return name
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
    .join('')
}

const canonical = icons as Record<string, React.ComponentType<LucideProps>>
const allExports = lucide as unknown as Record<string, unknown>

function resolve(name?: string): React.ComponentType<LucideProps> | undefined {
  if (!name) return undefined
  const key = name.includes('-') || /^[a-z]/.test(name) ? toPascal(name) : name
  const hit = canonical[key]
  if (hit) return hit
  // lucide 弃用别名（如 Fingerprint→FingerprintPattern）仍是命名导出，却不在 icons 规范表里；
  // 旧菜单种子存的是别名，只查 icons 会渲染成圆点。别名组件是带 render 的 forwardRef 对象。
  const alias = allExports[key]
  if (alias && typeof alias === 'object' && typeof (alias as { render?: unknown }).render === 'function') {
    return alias as React.ComponentType<LucideProps>
  }
  return undefined
}

/** 按 lucide 图标名渲染（支持 PascalCase、kebab-case 与弃用别名），找不到时渲染圆点 */
export function DynamicIcon({ name, ...props }: { name?: string } & LucideProps) {
  const Icon = resolve(name)
  if (!Icon) return <span className="inline-block size-1.5 rounded-full bg-current" aria-hidden="true" />
  return <Icon aria-hidden="true" {...props} />
}
