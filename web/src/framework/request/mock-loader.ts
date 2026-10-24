import { MOCK_ENABLED } from '@/framework/env'

// 自动加载所有 *.mock.ts（副作用注册）。仅当 VITE_MOCK === 'true'（见 framework/env.ts）。
if (MOCK_ENABLED) {
  import.meta.glob(['/src/framework/**/*.mock.ts', '/src/plugins/**/*.mock.ts', '/src/pages/**/*.mock.ts', '/src/mock/**/*.ts'], { eager: true })
}

export {}
