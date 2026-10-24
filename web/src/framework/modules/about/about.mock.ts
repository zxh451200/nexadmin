import { defineMock } from '@/framework/request/mock'
import pkg from '../../../../package.json'
import type { AboutInfo, LicenseRow } from './api'

// 常见依赖的真实许可证；按精确名优先，其次按作用域前缀，未知写 'Unknown'
const EXACT: Record<string, string> = {
  '@fontsource-variable/inter': 'OFL-1.1',
  '@fontsource-variable/jetbrains-mono': 'OFL-1.1',
  'class-variance-authority': 'Apache-2.0',
  dompurify: 'MPL-2.0 OR Apache-2.0',
  'lucide-react': 'ISC',
  typescript: 'Apache-2.0',
  '@ant-design/icons-svg': 'MIT',
  '@hookform/resolvers': 'MIT',
  '@uppy/core': 'MIT',
  '@uppy/tus': 'MIT',
  axios: 'MIT',
  clsx: 'MIT',
  cmdk: 'MIT',
  cropperjs: 'MIT',
  'date-fns': 'MIT',
  i18next: 'MIT',
  'i18next-browser-languagedetector': 'MIT',
  react: 'MIT',
  'react-day-picker': 'MIT',
  'react-dom': 'MIT',
  'react-hook-form': 'MIT',
  'react-i18next': 'MIT',
  'react-router-dom': 'MIT',
  recharts: 'MIT',
  sonner: 'MIT',
  'tailwind-merge': 'MIT',
  tailwindcss: 'MIT',
  'tailwindcss-animate': 'MIT',
  vaul: 'MIT',
  vite: 'MIT',
  zod: 'MIT',
  zustand: 'MIT',
}
const SCOPES: Record<string, string> = {
  '@dnd-kit': 'MIT',
  '@radix-ui': 'MIT',
  '@tanstack': 'MIT',
  '@tiptap': 'MIT',
}

const licenseOf = (name: string) => EXACT[name] ?? SCOPES[name.split('/')[0]] ?? 'Unknown'

const deps = (pkg as { dependencies?: Record<string, string> }).dependencies ?? {}
const licenses: LicenseRow[] = Object.entries(deps)
  .map(([name, version]) => ({ name, version, license: licenseOf(name) }))
  .sort((a, b) => a.name.localeCompare(b.name))

const about: AboutInfo = {
  version: (pkg as { version?: string }).version ?? '0.1.0',
  buildTime: '2026-10-01T08:30:00.000Z',
  dbEngine: 'PostgreSQL 16.4',
  goVersion: 'go1.23.4',
  os: 'linux/amd64',
}

defineMock([
  { method: 'GET', url: '/about', handler: () => about },
  { method: 'GET', url: '/about/licenses', handler: () => ({ list: licenses }) },
])
