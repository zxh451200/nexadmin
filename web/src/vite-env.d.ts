/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** 仅 'true' 时启用本地 mock；未设置 = 关闭（见 framework/env.ts） */
  readonly VITE_MOCK?: string
  readonly VITE_API_BASE?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

/** 前端版本，构建时由 vite.config.ts 的 define 注入 */
declare const __APP_VERSION__: string
