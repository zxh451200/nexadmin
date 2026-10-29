import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  completionOf,
  initialState,
  localeFormReducer,
  marshalDoc,
  marshalFlags,
  publishableOf,
  REVIEW_KEY,
  splitReview,
  valuesOf,
  type LocaleFormState,
} from './locale-form.ts'
import type { EntitySchema, LocaleOption } from './types.ts'

// 运行：cd web && node --test src/framework/components/i18n/locale-form.test.ts

const schema: EntitySchema = {
  entity: 'post',
  publishable: true,
  translatable: [
    { key: 'title', labelKey: 'plugin_website.fields.postTitle', widget: 'text', max: 200 },
    { key: 'summary', labelKey: 'plugin_website.fields.summary', widget: 'textarea', max: 500, rows: 2 },
    { key: 'content', labelKey: 'plugin_website.fields.content', widget: 'rich' },
  ],
}

const locales: LocaleOption[] = [
  { code: 'zh-CN', label: '简体中文', default: true },
  { code: 'en-US', label: 'English' },
  { code: 'ja-JP', label: '日本語' },
]

const SOURCE = { title: '标题', summary: '简介', content: '<p>正文</p>' }

function fresh(over: Partial<Parameters<typeof initialState>[0]> = {}): LocaleFormState {
  return initialState({
    base: { ...SOURCE },
    defaultLocale: 'zh-CN',
    locales,
    sourcePublished: false,
    ...over,
  })
}

test('切语言不丢输入：三档之间来回切，各档的值互不覆盖', () => {
  let s = fresh()
  s = localeFormReducer(s, { type: 'activate', code: 'ja-JP' })
  s = localeFormReducer(s, { type: 'write', code: 'ja-JP', key: 'title', value: 'タイトル' })
  s = localeFormReducer(s, { type: 'activate', code: 'en-US' })
  s = localeFormReducer(s, { type: 'write', code: 'en-US', key: 'title', value: 'Title' })
  s = localeFormReducer(s, { type: 'activate', code: 'ja-JP' })

  assert.equal(valuesOf(s, 'ja-JP', 'zh-CN').title, 'タイトル')
  assert.equal(valuesOf(s, 'en-US', 'zh-CN').title, 'Title')
  // 原文永远没被动过
  assert.deepEqual(s.base, SOURCE)
})

test('原文态下写字段只改裸列，不会污染任何译文', () => {
  let s = fresh()
  s = localeFormReducer(s, { type: 'write', code: 'ja-JP', key: 'title', value: 'タイトル' })
  s = localeFormReducer(s, { type: 'write', code: null, key: 'title', value: '新标题' })

  assert.equal(s.base.title, '新标题')
  assert.equal(s.doc['ja-JP']?.title, 'タイトル')
})

test('值没变就不置 dirty，避免「只是切了切语言」也弹未保存提示', () => {
  const s = fresh()
  assert.equal(localeFormReducer(s, { type: 'write', code: null, key: 'title', value: SOURCE.title }), s)
  assert.equal(localeFormReducer(s, { type: 'activate', code: 'zh-CN' }), s)
  assert.equal(localeFormReducer(s, { type: 'write', code: null, key: 'title', value: '改过的' }).dirty, true)
})

test('从原文复制只补空白，绝不覆盖已翻译内容', () => {
  let s = fresh({ i18nJson: JSON.stringify({ 'en-US': { title: '已有译文' } }) })
  s = localeFormReducer(s, { type: 'fillBlanks', code: 'en-US', values: s.base })

  assert.equal(s.doc['en-US']?.title, '已有译文')
  assert.equal(s.doc['en-US']?.summary, SOURCE.summary)
})

test('完成度不计入原文本身为空的字段', () => {
  const s = fresh({ base: { ...SOURCE, summary: '   ' } })
  const c = completionOf(schema, s, 'en-US', 'zh-CN')
  // summary 原文为空 → 不该要求译文，也不该计入分母
  assert.equal(c.total, 2)
  assert.deepEqual(c.missing, ['title', 'content'])

  const full = completionOf(schema, s, 'zh-CN', 'zh-CN')
  assert.equal(full.done, full.total)
  assert.deepEqual(full.missing, [])
})

// 这条是发布闸门的正面靶子：删掉「原文为空不计入」的过滤，它不会红；
// 但删掉 missing 的计算，done===total 就会假成立。
test('缺译的语言不可发布，且点名缺哪几个字段', () => {
  let s = fresh()
  assert.deepEqual(publishableOf(schema, s, 'en-US', 'zh-CN'), { ok: false, missing: ['title', 'summary', 'content'] })

  s = localeFormReducer(s, { type: 'fillBlanks', code: 'en-US', values: s.base })
  assert.equal(publishableOf(schema, s, 'en-US', 'zh-CN').ok, true)
})

test('原文恒可发布：它的发布态是行的裸列，与译文完成度无关', () => {
  const s = fresh()
  assert.deepEqual(publishableOf(schema, s, 'zh-CN', 'zh-CN'), { ok: true, missing: [] })
})

test('marshalDoc 丢掉默认语言与空值，全空时返回空串', () => {
  const doc = {
    'zh-CN': { title: '不该存' },
    'en-US': { title: 'T', summary: '', content: '   ' },
    'ja-JP': { title: '', summary: '' },
  }
  assert.equal(marshalDoc(doc, 'zh-CN'), JSON.stringify({ 'en-US': { title: 'T' } }))
  assert.equal(marshalDoc({ 'ja-JP': { title: '' } }, 'zh-CN'), '')
})

// 只敲空格的译文如果落库，后端 pick 会认为它「已翻译」，该语言页面就渲染出一片空白。
test('纯空白译文不落库，与 blank() 同口径', () => {
  assert.equal(marshalDoc({ 'en-US': { title: '  \t ' } }, 'zh-CN'), '')
})

test('marshalFlags 保留显式 false：「没点名」和「明确不发」是两种语义', () => {
  assert.equal(marshalFlags({ 'en-US': true, 'ja-JP': false }, 'zh-CN'), JSON.stringify({ 'en-US': true, 'ja-JP': false }))
  assert.equal(marshalFlags({ 'zh-CN': true }, 'zh-CN'), '')
})

test('老数据（publishedJson 为空）沿用整行发布态，不会被一次保存打掉', () => {
  const on = fresh({ sourcePublished: true })
  assert.equal(on.published['en-US'], true)
  assert.equal(on.published['ja-JP'], true)

  const off = fresh({ sourcePublished: false })
  assert.deepEqual(off.published, {})

  // 已有文档时原样采信，不再做继承
  const stored = fresh({ sourcePublished: true, publishedJson: JSON.stringify({ 'en-US': false }) })
  assert.equal(stored.published['en-US'], false)
  assert.equal(stored.published['ja-JP'], undefined)
})

test('序列化再读回来必须等价，否则保存一次就丢状态', () => {
  let s = fresh({ sourcePublished: true })
  s = localeFormReducer(s, { type: 'fillBlanks', code: 'en-US', values: s.base })
  s = localeFormReducer(s, { type: 'publish', code: 'en-US', on: true })
  s = localeFormReducer(s, { type: 'publish', code: 'ja-JP', on: false })

  const out = {
    base: s.base,
    i18nJson: marshalDoc(s.doc, 'zh-CN'),
    publishedJson: marshalFlags(s.published, 'zh-CN'),
    sourcePublished: s.sourcePublished,
  }
  const again = initialState({
    base: out.base,
    i18nJson: out.i18nJson,
    publishedJson: out.publishedJson,
    sourcePublished: out.sourcePublished,
    defaultLocale: 'zh-CN',
    locales,
  })

  assert.deepEqual(again.doc, s.doc)
  assert.equal(again.published['en-US'], true)
  assert.equal(again.published['ja-JP'], false)
})

test('关掉某语言发布要留显式 false，不能把键删掉', () => {
  let s = fresh({ sourcePublished: true })
  s = localeFormReducer(s, { type: 'publish', code: 'en-US', on: false })
  assert.equal(s.published['en-US'], false)
  assert.equal(marshalFlags(s.published, 'zh-CN').includes('"en-US":false'), true)
})

test('机翻标记与译文同住 i18nJson，但读回时不能混进语言文档', () => {
  const raw = JSON.stringify({
    'en-US': { title: 'Title', content: '<p>Body</p>' },
    [REVIEW_KEY]: { 'en-US': 'mt' },
  })
  const { doc, review } = splitReview(raw)
  assert.deepEqual(doc, { 'en-US': { title: 'Title', content: '<p>Body</p>' } })
  assert.equal(review['en-US'], 'mt')
  assert.equal('_review' in doc, false)

  const s = initialState({ base: { ...SOURCE }, i18nJson: raw, defaultLocale: 'zh-CN', locales, sourcePublished: false })
  assert.equal(s.doc['en-US'].title, 'Title')
  assert.equal(s.review['en-US'], 'mt')
})

test('确认译文后标记消失，且落盘里不再有保留键', () => {
  let s = fresh({ i18nJson: JSON.stringify({ 'en-US': { title: 'Title' }, [REVIEW_KEY]: { 'en-US': 'mt' } }) })
  assert.equal(s.review['en-US'], 'mt')
  s = localeFormReducer(s, { type: 'review', code: 'en-US', on: false })
  assert.equal(s.review['en-US'], undefined)
  const json = marshalDoc(s.doc, 'zh-CN', s.review)
  assert.equal(json.includes(REVIEW_KEY), false)
  assert.equal(json.includes('Title'), true, '清标记不能顺手清掉译文')
})

test('只有标记、没有译文时也要落盘（填完又被清空的场景）', () => {
  const s = fresh({})
  const json = marshalDoc({}, 'zh-CN', { 'en-US': 'mt' })
  assert.equal(json.includes(REVIEW_KEY), true)
  const again = initialState({ base: { ...SOURCE }, i18nJson: json, defaultLocale: 'zh-CN', locales, sourcePublished: false })
  assert.equal(again.review['en-US'], 'mt')
  assert.equal(s.doc['en-US'], undefined)
})

test('重复标记同一语言不产生第二次 dirty', () => {
  let s = fresh({})
  s = localeFormReducer(s, { type: 'review', code: 'en-US', on: true })
  assert.equal(s.dirty, true)
  const same = localeFormReducer({ ...s, dirty: false }, { type: 'review', code: 'en-US', on: true })
  assert.equal(same.dirty, false)
  assert.equal(localeFormReducer(same, { type: 'review', code: 'ja-JP', on: false }), same, '没标记的语言「取消标记」是空操作')
})
