import { mergeAttributes, Node } from '@tiptap/react'

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    embed: {
      /** 插入视频平台嵌入块（B 站 / YouTube） */
      setEmbed: (options: { src: string }) => ReturnType
    }
  }
}

/** 视频嵌入块：只存平台白名单里的 iframe 地址（规范化后写入，存库前还会过服务端白名单）。 */
export const Embed = Node.create({
  name: 'embed',
  group: 'block',
  atom: true,
  draggable: true,

  addAttributes() {
    return {
      src: { default: null },
    }
  },

  parseHTML() {
    return [{ tag: 'iframe[src]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'iframe',
      mergeAttributes(
        {
          frameborder: '0',
          allow: 'autoplay; fullscreen; picture-in-picture',
          allowfullscreen: 'true',
          referrerpolicy: 'strict-origin-when-cross-origin',
          loading: 'lazy',
        },
        HTMLAttributes,
      ),
    ]
  },

  addCommands() {
    return {
      setEmbed:
        (options) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: options }),
    }
  },
})
