/** 会话被清空（登出、401、其它标签页登出）时广播，供页面状态订阅重置。 */
export const SESSION_CLEARED_EVENT = 'nex:session-cleared'

export function emitSessionCleared() {
  window.dispatchEvent(new Event(SESSION_CLEARED_EVENT))
}
