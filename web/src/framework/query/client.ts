import { QueryClient } from '@tanstack/react-query'

/** 应用级单例：登出/会话失效时可从非组件代码清空缓存，避免换账号后读到上一账号的数据。 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 30_000 },
  },
})
