export type UploadKind = 'image' | 'video' | 'file'
export type UploadVisibility = 'public' | 'private'

export interface UploadedFile {
  id: string
  url: string
  name: string
  size: number
  type: string
  posterUrl?: string
}

/** GET /config/upload 的返回；大小单位 MB，0 表示不限制 */
export interface UploadConfig {
  maxImageMb: number
  maxVideoMb: number
  maxFileMb: number
  allowedTypes: string[]
}
