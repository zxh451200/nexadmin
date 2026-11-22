/** 表单 label 的文本可复制（全站统一）：
 *  - 拖选受限于 <label>「按下即聚焦关联控件」的浏览器默认行为，user-select: text 也救不回来（实测）；
 *  - Radix Label 在 mousedown 上 preventDefault，连原生双击选词都被吞掉。
 *  统一路径：双击选中整个 label 文本（随后 Ctrl+C 复制），单击聚焦控件的行为不变；
 *  input/textarea/select/contenteditable 内的双击不劫持，仍是各自的选词。
 *  ui/label.tsx 原有的按元素 onDoubleClick 补偿由此全局监听统一覆盖（含各处裸 <label>）。 */
export function installLabelCopy() {
  document.addEventListener('dblclick', (e) => {
    const target = e.target as HTMLElement | null
    if (!target?.closest) return
    if (target.closest('input, textarea, select') || target.isContentEditable) return
    const label = target.closest('label')
    if (!label) return
    const sel = window.getSelection()
    if (!sel) return
    const range = document.createRange()
    range.selectNodeContents(label)
    sel.removeAllRanges()
    sel.addRange(range)
  })
}
