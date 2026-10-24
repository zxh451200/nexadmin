import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/framework/ui/dialog'

export interface LightboxProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  src: string
  alt: string
}

/** 图片放大预览 */
export function Lightbox({ open, onOpenChange, src, alt }: LightboxProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90dvh] w-[calc(100vw-1.5rem)] max-w-4xl items-center justify-center bg-card p-2 md:p-4">
        <DialogTitle className="sr-only">{alt}</DialogTitle>
        <DialogDescription className="sr-only">{alt}</DialogDescription>
        <img src={src} alt={alt} className="max-h-[80dvh] max-w-full rounded-md object-contain" />
      </DialogContent>
    </Dialog>
  )
}
