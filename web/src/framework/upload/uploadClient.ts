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
  return new Promise((resolve, reject) => {
    if (opts.signal?.aborted) return reject(abortError())
    const uppy = new Uppy({ autoProceed: false }).use(Tus, {
      endpoint: UPLOAD_ENDPOINT,
      chunkSize: CHUNK_SIZE,
      retryDelays: [0, 1000, 3000, 5000],
      removeFingerprintOnSuccess: true,
      headers: (): Record<string, string> => {
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
        meta: { filename: file.name, filetype: file.type, visibility: opts.visibility ?? 'private', kind: opts.kind ?? 'file' },
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
