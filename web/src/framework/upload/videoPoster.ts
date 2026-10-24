/** 浏览器端用 canvas 截取视频首帧，返回 JPEG Blob；失败返回 null（不依赖服务端转码） */
export function captureVideoPoster(file: Blob, maxWidth = 640): Promise<Blob | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file)
    const video = document.createElement('video')
    video.muted = true
    video.playsInline = true
    video.preload = 'auto'
    const done = (blob: Blob | null) => {
      URL.revokeObjectURL(url)
      video.removeAttribute('src')
      video.load()
      resolve(blob)
    }
    const timer = window.setTimeout(() => done(null), 8000)
    video.onerror = () => {
      window.clearTimeout(timer)
      done(null)
    }
    video.onloadeddata = () => {
      // 避免首帧全黑，稍微向后取一点
      video.currentTime = Math.min(0.1, (video.duration || 1) / 2)
    }
    video.onseeked = () => {
      window.clearTimeout(timer)
      const w = Math.min(maxWidth, video.videoWidth || maxWidth)
      const h = Math.round((w * (video.videoHeight || 9)) / (video.videoWidth || 16))
      const canvas = document.createElement('canvas')
      canvas.width = w
      canvas.height = h
      const ctx = canvas.getContext('2d')
      if (!ctx) return done(null)
      ctx.drawImage(video, 0, 0, w, h)
      canvas.toBlob((b) => done(b), 'image/jpeg', 0.8)
    }
    video.src = url
  })
}
