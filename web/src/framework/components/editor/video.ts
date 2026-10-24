import { mergeAttributes, Node } from '@tiptap/react'

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    video: {
      /** 插入视频节点 */
      setVideo: (options: { src: string; poster?: string }) => ReturnType
    }
  }
}

/** 自写的 video 块级节点：<video controls src poster> */
export const Video = Node.create({
  name: 'video',
  group: 'block',
  atom: true,
  draggable: true,

  addAttributes() {
    return {
      src: { default: null },
      poster: { default: null },
    }
  },

  parseHTML() {
    return [{ tag: 'video[src]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return ['video', mergeAttributes({ controls: 'true', preload: 'metadata', playsinline: 'true' }, HTMLAttributes)]
  },

  addCommands() {
    return {
      setVideo:
        (options) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: options }),
    }
  },
})
