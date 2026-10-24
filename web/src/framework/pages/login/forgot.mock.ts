import { defineMock, mockError } from '@/framework/request/mock'

const FORGOT_CODE = '123456'
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function checkTarget(target: unknown) {
  if (!EMAIL_RE.test(String(target ?? ''))) mockError(400, '邮箱格式不正确')
}

// 与后端契约一致：不论邮箱是否存在，send-code 都返回同样的响应；验证码 6 位、一次性。
defineMock([
  {
    method: 'POST',
    url: '/auth/forgot/send-code',
    handler: ({ body }) => {
      checkTarget(body?.target)
      console.info(`[mock] Forgot-password code for ${body.target}: ${FORGOT_CODE}`)
      return { cooldown: 60 }
    },
  },
  {
    method: 'POST',
    url: '/auth/forgot/reset',
    handler: ({ body }) => {
      checkTarget(body?.target)
      if (body?.code !== FORGOT_CODE) mockError(400, '验证码错误或已过期')
      if (!body?.newPassword) mockError(400, '新密码不能为空')
      return null
    },
  },
])
