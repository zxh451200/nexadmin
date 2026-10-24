/**
 * 是否启用前端 mock（假数据，不连后端）。
 *
 * 唯一判定口径：仅当 VITE_MOCK === 'true' 才启用。变量未设置、为空或写错（如 'TRUE' / '1'）一律视为关闭，
 * 这样生产环境漏配 VITE_MOCK 时是连真实后端，而不是静默跑假数据。
 * 开发模式在 web/.env.development 显式写了 VITE_MOCK=true；生产在 web/.env.production 写 VITE_MOCK=false。
 * 代码里不要再直接读 import.meta.env.VITE_MOCK，统一用这里的常量。
 */
export const MOCK_ENABLED: boolean = import.meta.env.VITE_MOCK === 'true'
