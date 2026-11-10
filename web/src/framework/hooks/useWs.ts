// useWs 是框架通用 WebSocket hook:自动带 token 鉴权、断线指数退避重连、
// 按主题订阅。所有插件的实时推送共用一条 WS 连接(框架 boot 时已挂 /ws 端点)。
//
// 用法:
//   const { status, subscribe } = useWs()
//   useEffect(() => subscribe('im:CONV123', (data) => { ... }), [subscribe])
//
// 状态:connecting | open | closed | error

import { useCallback, useEffect, useState } from 'react'

import { SESSION_CLEARED_EVENT } from '@/framework/auth/session-events'
import { api } from '@/framework/request/http'

export type WsStatus = 'connecting' | 'open' | 'closed' | 'error'

export interface WsFrame {
  type: 'subscribe' | 'unsubscribe' | 'ping' | 'pong' | 'message' | 'error'
  topic?: string
  data?: unknown
}

// 进程级单例:多个组件 useWs 共享同一条连接。
let wsInstance: WebSocket | null = null
let wsStatus: WsStatus = 'closed'
const statusListeners = new Set<(s: WsStatus) => void>()
const topicHandlers = new Map<string, Set<(data: unknown) => void>>()
let reconnectDelay = 1000
let reconnectTimer: number | null = null
let pingTimer: number | null = null

function setStatus(s: WsStatus) {
  wsStatus = s
  statusListeners.forEach((fn) => fn(s))
}

let connectInFlight = false

// teardown 彻底拆掉连接与所有定时器：登出、最后订阅者卸载都走它，
// 否则会留下无人使用的连接 + 永久 ping 心跳 + 在途重连定时器（僵尸连接）。
function teardown() {
  if (reconnectTimer) {
    window.clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (pingTimer) {
    window.clearInterval(pingTimer)
    pingTimer = null
  }
  if (wsInstance) {
    const ws = wsInstance
    wsInstance = null
    ws.onclose = null
    ws.close()
  }
  connectInFlight = false
  setStatus('closed')
}

// 建连前先换一张一次性入场票：WebSocket 的握手 URL 会进反代日志与浏览器网络面板，
// 长寿命访问令牌不能出现在那里。
async function connect() {
  if (wsInstance && (wsInstance.readyState === WebSocket.OPEN || wsInstance.readyState === WebSocket.CONNECTING)) {
    return
  }
  if (connectInFlight) {
    return
  }
  connectInFlight = true
  setStatus('connecting')
  let ticket = ''
  try {
    ticket = (await api.post<{ ticket: string }>('/ws/ticket', undefined, { silent: true })).ticket
  } catch {
    // 换票失败多半是登录态失效；http 层会统一清会话并跳登录，这里只按退避重试。
    connectInFlight = false
    setStatus('error')
    scheduleReconnect()
    return
  }
  connectInFlight = false
  if (wsInstance && (wsInstance.readyState === WebSocket.OPEN || wsInstance.readyState === WebSocket.CONNECTING)) {
    return
  }
  // 换票等 await 期间订阅者可能已全部退订：没有人听的连接不再建立，
  // 否则每 30s 一次 ping 的空连接永不关闭。
  if (topicHandlers.size === 0) {
    setStatus('closed')
    return
  }
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/ws?ticket=${encodeURIComponent(ticket)}`
  try {
    wsInstance = new WebSocket(url)
  } catch {
    setStatus('error')
    scheduleReconnect()
    return
  }

  wsInstance.onopen = () => {
    setStatus('open')
    reconnectDelay = 1000 // 重置退避
    // 重连后重新订阅全部主题
    topicHandlers.forEach((_handlers, topic) => {
      wsInstance?.send(JSON.stringify({ type: 'subscribe', topic }))
    })
    // 应用层心跳(协议层 ping 由服务端发)
    if (pingTimer) window.clearInterval(pingTimer)
    pingTimer = window.setInterval(() => {
      if (wsInstance?.readyState === WebSocket.OPEN) {
        wsInstance.send(JSON.stringify({ type: 'ping' }))
      }
    }, 30_000)
  }

  wsInstance.onmessage = (ev) => {
    try {
      const frame: WsFrame = JSON.parse(ev.data)
      if (frame.type === 'message' && frame.topic) {
        topicHandlers.get(frame.topic)?.forEach((fn) => fn(frame.data))
      }
    } catch {
      // 非 JSON 帧忽略
    }
  }

  wsInstance.onclose = () => {
    setStatus('closed')
    if (pingTimer) window.clearInterval(pingTimer)
    if (topicHandlers.size > 0) {
      scheduleReconnect() // 有订阅者才重连
    }
  }

  wsInstance.onerror = () => {
    setStatus('error')
  }
}

function scheduleReconnect() {
  // 没有订阅者就不再重连：登出后换票恒 401，这里不拦的话就是每 1s→30s 一次的永久 POST 循环
  if (topicHandlers.size === 0) return
  if (reconnectTimer) return
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = null
    reconnectDelay = Math.min(reconnectDelay * 2, 30_000)
    void connect()
  }, reconnectDelay)
}

function sendSubscribe(topic: string) {
  if (wsInstance?.readyState === WebSocket.OPEN) {
    wsInstance.send(JSON.stringify({ type: 'subscribe', topic }))
  }
}

function sendUnsubscribe(topic: string) {
  if (wsInstance?.readyState === WebSocket.OPEN) {
    wsInstance.send(JSON.stringify({ type: 'unsubscribe', topic }))
  }
}

// useWs 返回连接状态与订阅/退订函数。
export function useWs() {
  const [status, setStatusState] = useState<WsStatus>(wsStatus)

  useEffect(() => {
    const listener = (s: WsStatus) => setStatusState(s)
    const onSessionCleared = () => teardown() // 登出/会话失效：断开并停掉全部定时器，不留僵尸连接
    statusListeners.add(listener)
    window.addEventListener(SESSION_CLEARED_EVENT, onSessionCleared)
    void connect()
    return () => {
      statusListeners.delete(listener)
      window.removeEventListener(SESSION_CLEARED_EVENT, onSessionCleared)
      // 最后一个组件卸载时彻底拆掉(无订阅者的连接没有意义，含在途重连定时器)
      if (statusListeners.size === 0 && topicHandlers.size === 0) {
        teardown()
      }
    }
  }, [])

  // subscribe 返回清理函数(组件卸载时自动退订)。
  const subscribe = useCallback((topic: string, handler: (data: unknown) => void) => {
    let set = topicHandlers.get(topic)
    if (!set) {
      set = new Set()
      topicHandlers.set(topic, set)
      sendSubscribe(topic)
    }
    set.add(handler)
    return () => {
      set?.delete(handler)
      if (set && set.size === 0) {
        topicHandlers.delete(topic)
        sendUnsubscribe(topic)
      }
    }
  }, [])

  return { status, subscribe }
}
