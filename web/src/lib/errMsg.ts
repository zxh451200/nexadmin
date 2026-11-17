/**
 * 取异常里可展示的 message，取不到就用调用方给的兜底文案。
 * 页面 catch 里拼「服务端 message + 兜底」时统一用它，避免各处复制一份三元表达式。
 */
export function errMsg(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback
}
