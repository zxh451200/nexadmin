import { jsonrepair } from 'jsonrepair'

export type JsonRepairResult = { ok: true; text: string } | { ok: false; message: string }

/**
 * 尽力把常见笔误修成合法 JSON：单引号、裸键、尾逗号、注释、未闭合括号、全角引号等。
 * 只做文本修复，不解析成对象——凭据里的长数字（订单号 / 账号 ID）不会被 JS 数字精度截断。
 */
export function repairJsonText(text: string): JsonRepairResult {
  try {
    return { ok: true, text: jsonrepair(text) }
  } catch (err) {
    return { ok: false, message: err instanceof Error ? err.message : String(err) }
  }
}
