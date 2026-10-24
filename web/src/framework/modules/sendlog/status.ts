import type { BadgeProps } from '@/framework/ui/badge'
import type { SendChannel, SendStatus } from './api'

export const STATUS_VARIANT: Record<SendStatus, BadgeProps['variant']> = {
  success: 'success',
  fail: 'destructive',
  skipped: 'warning',
}

export const CHANNEL_VARIANT: Record<SendChannel, BadgeProps['variant']> = {
  sms: 'info',
  email: 'secondary',
  robot: 'outline',
}
