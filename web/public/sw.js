/* NexAdmin 最小 Service Worker：只缓存应用壳，不碰任何接口与带凭据的请求。
 * 改动本文件即触发浏览器更新；需要强制刷新壳缓存时同步递增 VERSION。 */
const VERSION = 'v8'
const SHELL_CACHE = 'nex-shell-' + VERSION
const RUNTIME_CACHE = 'nex-runtime-' + VERSION
const SHELL_URLS = ['/offline.html', '/manifest.webmanifest', '/icon.svg', '/icon-maskable.svg', '/icon-192.png', '/icon-512.png']
const RUNTIME_MAX_ENTRIES = 200

// 这些前缀永不缓存、永不拦截（接口、鉴权、SSO、探活、指标）
const BYPASS_PREFIXES = ['/api', '/oauth', '/sso', '/healthz', '/metrics']

function isBypassPath(pathname) {
  return BYPASS_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'))
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL_URLS.map((u) => new Request(u, { cache: 'reload' }))))
      .then(() => notifyClients({ type: 'SW_UPDATE_READY' })),
  )
  // 不自动 skipWaiting：由页面提示用户后发 SKIP_WAITING，避免页面与资源版本错配
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k.startsWith('nex-') && k !== SHELL_CACHE && k !== RUNTIME_CACHE).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('message', (event) => {
  const type = event.data && event.data.type
  if (type === 'SKIP_WAITING') self.skipWaiting()
  else if (type === 'CLEAR_RUNTIME') event.waitUntil(caches.delete(RUNTIME_CACHE))
})

function notifyClients(msg) {
  return self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
    // 首次安装（没有任何受控页面）不算“新版本”
    if (self.registration.active) list.forEach((c) => c.postMessage(msg))
  })
}

function cacheable(res) {
  if (!res || res.status !== 200 || res.type !== 'basic') return false
  const cc = (res.headers.get('Cache-Control') || '').toLowerCase()
  return !cc.includes('no-store') && !cc.includes('private')
}

function trim(cache) {
  return cache.keys().then((keys) => (keys.length > RUNTIME_MAX_ENTRIES ? Promise.all(keys.slice(0, keys.length - RUNTIME_MAX_ENTRIES).map((k) => cache.delete(k))) : null))
}

function isStaticAsset(url, request) {
  return (
    url.pathname.startsWith('/assets/') ||
    request.destination === 'font' ||
    /\.(?:woff2?|ttf|otf)$/i.test(url.pathname) ||
    /^\/(?:icon[\w-]*|apple-touch-icon)\.(?:svg|png)$/.test(url.pathname)
  )
}

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  if (req.headers.has('Authorization') || req.headers.has('Range')) return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin || isBypassPath(url.pathname)) return

  // 页面导航：network-first，离线回退 offline.html（不缓存 HTML 本身）
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).catch(() => caches.match('/offline.html', { cacheName: SHELL_CACHE }).then((r) => r || Response.error())),
    )
    return
  }

  // 带 hash 的资源与字体/图标：cache-first，运行时缓存
  if (isStaticAsset(url, req)) {
    event.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((res) => {
            if (cacheable(res)) {
              const copy = res.clone()
              event.waitUntil(caches.open(RUNTIME_CACHE).then((c) => c.put(req, copy).then(() => trim(c))))
            }
            return res
          }),
      ),
    )
  }
})
