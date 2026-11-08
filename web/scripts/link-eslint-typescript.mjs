// ESLint 的 TS 解析器（typescript-eslint 8.72）要求 typescript <6.1.0，且把 typescript 声明为
// peer：npm 只会把它提升到根 node_modules，而根上装的是 TS 7 的 native 编译器（没有 JS API）。
// 这里给每个 @typescript-eslint 包挂一份私有的 typescript 6（别名依赖 typescript-6），
// 让 require('typescript') 从包自身位置解析到带 JS API 的那份。tsc 本身仍用根上的 TS 7。
import fs from 'node:fs'
import path from 'node:path'

const root = path.resolve(import.meta.dirname, '..')
const ts6 = path.join(root, 'node_modules', 'typescript-6')
if (!fs.existsSync(path.join(ts6, 'package.json'))) {
  console.warn('[link-eslint-typescript] 未找到 typescript-6，跳过')
  process.exit(0)
}

const linkDirs = (dir) => {
  const scoped = path.join(dir, 'node_modules', '@typescript-eslint')
  const candidates = [dir]
  if (fs.existsSync(scoped)) {
    for (const name of fs.readdirSync(scoped)) candidates.push(path.join(scoped, name))
  }
  return candidates
}

const visited = new Set()
const queue = [path.join(root, 'node_modules', 'typescript-eslint'), ...linkDirs(root)]
while (queue.length) {
  const dir = queue.shift()
  if (visited.has(dir) || !fs.existsSync(path.join(dir, 'package.json'))) continue
  visited.add(dir)
  for (const nested of linkDirs(dir)) if (nested !== dir && !visited.has(nested)) queue.push(nested)
  const pkg = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'))
  if (!pkg.peerDependencies?.typescript) continue
  const target = path.join(dir, 'node_modules', 'typescript')
  if (fs.existsSync(target)) continue
  fs.mkdirSync(path.dirname(target), { recursive: true })
  fs.symlinkSync(ts6, target, 'junction')
  console.log(`[link-eslint-typescript] ${path.relative(root, target)} -> typescript-6`)
}
