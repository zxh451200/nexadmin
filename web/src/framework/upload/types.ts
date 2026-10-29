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
  /** 开启后图片会额外上传一份 WebP 副本（原图保留，服务端按 Accept 下发） */
  imageWebp?: boolean
}
