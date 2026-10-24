import type { IconDefinition } from '@ant-design/icons-svg/lib/types'
import DingtalkOutlined from '@ant-design/icons-svg/es/asn/DingtalkOutlined'
import GithubOutlined from '@ant-design/icons-svg/es/asn/GithubOutlined'
import GoogleOutlined from '@ant-design/icons-svg/es/asn/GoogleOutlined'
import WechatWorkOutlined from '@ant-design/icons-svg/es/asn/WechatWorkOutlined'

interface SvgNode {
  tag: string
  attrs: Record<string, string>
  children?: SvgNode[]
}

/** 把 Ant Design 图标数据（MIT）渲染成跟随文字颜色的内联 SVG */
function makeIcon(def: IconDefinition) {
  const root = def.icon as SvgNode // 这几个都是单色图标，icon 是对象而不是函数
  return function BrandIcon({ className }: { className?: string }) {
    return (
      <svg viewBox={root.attrs.viewBox} className={className} fill="currentColor" aria-hidden="true" focusable="false">
        {(root.children ?? []).map((c, i) => (
          <path key={i} d={c.attrs.d} />
        ))}
      </svg>
    )
  }
}

export const WecomIcon = makeIcon(WechatWorkOutlined)
export const DingtalkIcon = makeIcon(DingtalkOutlined)
export const GoogleIcon = makeIcon(GoogleOutlined)
export const GithubIcon = makeIcon(GithubOutlined)

/**
 * 飞书（Lark）：字节跳动 IconPark 的「new-lark」图标（Apache-2.0，@icon-park/svg 1.4.2，npm 包内 icons/NewLark）。
 * 这是官方图标库里的单色轮廓版，不是带品牌色的完整 Logo；需要彩色官方标志时，从飞书开放平台品牌资源
 * 下载 SVG 后替换这里。
 */
export function FeishuIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} fill="none" aria-hidden="true" focusable="false">
      <path
        d="M17 29C21 29 25 26.9339 28 23.4065C36 14 41.4242 16.8166 44 17.9998C38.5 20.9998 40.5 29.6233 33 35.9998C28.382 39.9259 23.4945 41.014 19 41C12.5231 40.9799 6.86226 37.7637 4 35.4063V16.9998"
        stroke="currentColor"
        strokeWidth="4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5.64808 15.8669C5.02231 14.9567 3.77715 14.7261 2.86694 15.3519C1.95673 15.9777 1.72615 17.2228 2.35192 18.1331L5.64808 15.8669ZM36.0021 35.7309C36.958 35.1774 37.2843 33.9539 36.7309 32.9979C36.1774 32.042 34.9539 31.7157 33.9979 32.2691L36.0021 35.7309ZM2.35192 18.1331C5.2435 22.339 10.7992 28.144 16.8865 32.2239C19.9345 34.2667 23.217 35.946 26.449 36.7324C29.6946 37.522 33.0451 37.4428 36.0021 35.7309L33.9979 32.2691C32.2049 33.3072 29.9929 33.478 27.3947 32.8458C24.783 32.2103 21.9405 30.7958 19.1135 28.9011C13.4508 25.106 8.2565 19.661 5.64808 15.8669L2.35192 18.1331Z"
        fill="currentColor"
      />
      <path
        d="M33.5947 17C32.84 14.7027 30.8551 9.94054 27.5947 7H11.5947C15.2174 10.6757 23.0002 16 27.0002 24"
        stroke="currentColor"
        strokeWidth="4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

/** OpenID 官方标志（Simple Icons，CC0） */
export function OidcIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="currentColor" aria-hidden="true" focusable="false">
      <path d="M14.54.889l-3.63 1.773v18.17c-4.15-.52-7.27-2.78-7.27-5.5 0-2.58 2.8-4.75 6.63-5.41v-2.31C4.42 8.322 0 11.502 0 15.332c0 3.96 4.74 7.24 10.91 7.78l3.63-1.71V.888m.64 6.724v2.31c1.43.25 2.71.7 3.76 1.31l-1.97 1.11 7.03 1.53-.5-5.21-1.87 1.06c-1.74-1.06-3.96-1.81-6.45-2.11z" />
    </svg>
  )
}
