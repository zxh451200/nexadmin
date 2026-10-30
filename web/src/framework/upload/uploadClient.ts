import Uppy from '@uppy/core'
import Tus from '@uppy/tus'
import { MOCK_ENABLED } from '@/framework/env'
import { tokenStore } from '@/framework/request/http'
import type { UploadedFile, UploadKind, UploadVisibility } from './types'

export const CHUNK_SIZE = 5 * 1024 * 1024
export const UPLOAD_ENDPOINT = '/api/v1/files/upload'

export interface UploadOptions {
  kind?: UploadKind
  visibility?: UploadVisibility
  /** 非空表示这次上传是原文件的派生副本（如 WebP），服务端据此挂到原文件下 */
  variantOf?: string
  /** 直传外部端点时需要附加的请求头（如 Bunny 的签名头） */
  headers?: Record<string, string>
  onProgress?: (percent: number) => void
  signal?: AbortSignal
}


function abortError() {
  return new DOMException('Upload aborted', 'AbortError')
}

export function isAbortError(e: unknown) {
  return e instanceof DOMException && e.name === 'AbortError'
}

function mockUpload(file: File, opts: UploadOptions): Promise<UploadedFile> {
  return new Promise((resolve, reject) => {
    let pct = 0
    const step = Math.max(4, Math.min(20, 100 / Math.max(3, Math.ceil(file.size / (512 * 1024)))))
    const finish = () => {
      resolve({
        id: `${Date.now()}${Math.floor(Math.random() * 1e6)}`.padEnd(19, '0'),
        url: URL.createObjectURL(file),
        name: file.name,
        size: file.size,
        type: file.type || 'application/octet-stream',
      })
    }
    const timer = window.setInterval(() => {
      pct = Math.min(100, pct + step)
      opts.onProgress?.(pct)
      if (pct >= 100) {
        window.clearInterval(timer)
        finish()
      }
    }, 120)
    opts.signal?.addEventListener('abort', () => {
      window.clearInterval(timer)
      reject(abortError())
    })
  })
}

function tusUpload(file: File, opts: UploadOptions): Promise<UploadedFile> {
  return uploadToTus(UPLOAD_ENDPOINT, file, opts)
}

/** 直传到任意 tus 端点（例如外部流媒体的一次性上传地址）；与我们的上传共用进度与取消语义。 */
export function uploadToTus(endpoint: string, file: File, opts: UploadOptions): Promise<UploadedFile> {
  if (MOCK_ENABLED) return mockUpload(file, opts)
  return new Promise((resolve, reject) => {
    if (opts.signal?.aborted) return reject(abortError())
    // 只有自家上传端点才带访问令牌：外部端点（如流媒体的一次性直传地址）自带授权，
    // 把我们的 Bearer 发过去等于泄露会话。
    const own = endpoint === UPLOAD_ENDPOINT
    const uppy = new Uppy({ autoProceed: false }).use(Tus, {
      endpoint,
      chunkSize: CHUNK_SIZE,
      retryDelays: [0, 1000, 3000, 5000],
      removeFingerprintOnSuccess: true,
      headers: (): Record<string, string> => {
        // 外部端点带调用方给的签名头；自家端点带登录令牌，绝不把令牌发给第三方。
        if (!own) return { ...(opts.headers ?? {}) }
        const token = tokenStore.access
        return token ? { Authorization: `Bearer ${token}` } : {}
      },
    })
    const cleanup = () => uppy.destroy()
    let fileId: string
    try {
      fileId = uppy.addFile({
        name: file.name,
        type: file.type,
        data: file,
        meta: own
          ? { filename: file.name, filetype: file.type, visibility: opts.visibility ?? 'private', kind: opts.kind ?? 'file', ...(opts.variantOf ? { variantof: opts.variantOf } : {}) }
          : { filename: file.name, filetype: file.type },
      })
    } catch (e) {
      cleanup()
      return reject(e)
    }
    uppy.on('upload-progress', (f, p) => {
      if (f?.id === fileId && p.bytesTotal) opts.onProgress?.(Math.round((p.bytesUploaded / p.bytesTotal) * 100))
    })
    uppy.on('upload-success', (_f, resp) => {
      const uploadURL = (resp as { uploadURL?: string } | undefined)?.uploadURL ?? ''
      cleanup()
      resolve({
        id: uploadURL.split('/').filter(Boolean).pop() ?? fileId,
        url: uploadURL,
        name: file.name,
        size: file.size,
        type: file.type || 'application/octet-stream',
      })
    })
    uppy.on('upload-error', (_f, err) => {
      cleanup()
      reject(err)
    })
    opts.signal?.addEventListener('abort', () => {
      uppy.cancelAll()
      cleanup()
      reject(abortError())
    })
    uppy.upload().catch((e) => {
      cleanup()
      reject(e)
    })
  })
}

/** 上传一个文件。MOCK_ENABLED 时模拟进度并用 objectURL；否则走 tus 断点续传 */
export function uploadFile(file: File, opts: UploadOptions = {}): Promise<UploadedFile> {
  return MOCK_ENABLED ? mockUpload(file, opts) : tusUpload(file, opts)
}
