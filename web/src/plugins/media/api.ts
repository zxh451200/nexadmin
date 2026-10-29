import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface MediaStats {
  total: number
  done: number
  pending: number
}

export interface PendingImage {
  id: string
  name: string
  type: string
  size: number
  url: string
  createdAt: string
}

export interface VideoConfigView {
  enabled: boolean
  accountId: string
  subdomain: string
  hasToken: boolean
}

export interface VideoConfigInput {
  enabled: boolean
  accountId: string
  token: string
  subdomain: string
}

export interface VideoItem {
  id: string
  name: string
  size: number
  provider: string
  videoId: string
  playbackUrl: string
  /** MP4 直链：可直接粘到富文本「插入视频」里（需在 Cloudflare 控制台开启允许下载） */
  downloadUrl: string
  status: string
  uploader: string
  createdAt: string
}

export function useMediaStats() {
  return useQuery({ queryKey: ['media', 'stats'], queryFn: () => api.get<MediaStats>('/plugins/media/stats') })
}

export function usePendingImages(page: number, pageSize: number) {
  return useQuery({
    queryKey: ['media', 'images', page, pageSize],
    queryFn: () => api.get<{ list: PendingImage[]; total: number }>('/plugins/media/images', { page, pageSize }),
  })
}

export function useVideoConfig() {
  return useQuery({ queryKey: ['media', 'video', 'config'], queryFn: () => api.get<VideoConfigView>('/plugins/media/video/config') })
}

export function useSaveVideoConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: VideoConfigInput) => api.put<VideoConfigView>('/plugins/media/video/config', body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['media', 'video'] }),
  })
}

export function useVideos(page: number, pageSize: number) {
  return useQuery({
    queryKey: ['media', 'video', 'assets', page, pageSize],
    queryFn: () => api.get<{ list: VideoItem[]; total: number }>('/plugins/media/video/assets', { page, pageSize }),
  })
}

/** 申请一次性直传地址；文件直传厂商，不经过本站存储。 */
export function createVideoUpload(name: string) {
  return api.post<{ uploadUrl: string; videoId: string; playbackUrl: string }>('/plugins/media/video/upload', { name, size: 0 })
}

export function saveVideo(body: { videoId: string; name: string; size: number }) {
  return api.post<VideoItem>('/plugins/media/video/assets', body)
}

export function deleteVideo(id: string) {
  return api.del(`/plugins/media/video/assets/${id}`)
}
