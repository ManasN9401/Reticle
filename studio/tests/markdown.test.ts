import test from 'node:test'
import assert from 'node:assert/strict'
import { MARKDOWN_LIMIT, parseInline, parseMarkdown } from '../src/shared/markdown'

test('headings, paragraphs and rules become blocks', () => {
  const { blocks } = parseMarkdown('# Title\n\nFirst line\nsecond line\n\n---\n\n### Small')
  assert.deepEqual(blocks.map((b) => b.type), ['heading', 'paragraph', 'rule', 'heading'])
  assert.equal(blocks[0].type === 'heading' && blocks[0].level, 1)
  assert.equal(blocks[3].type === 'heading' && blocks[3].level, 3)
  // Lines of one paragraph are joined, as a Markdown reader would.
  assert.equal(blocks[1].type === 'paragraph' && blocks[1].spans[0].text, 'First line second line')
})

test('bullet and numbered lists are separate, and indented lines continue an item', () => {
  const { blocks } = parseMarkdown('- one\n- two\n  more on two\n1. first\n2. second')
  assert.equal(blocks.length, 2)
  const [bullets, numbers] = blocks
  assert.ok(bullets.type === 'list' && !bullets.ordered && bullets.items.length === 2)
  assert.ok(bullets.type === 'list' && bullets.items[1].map((s) => s.text).join('').includes('more on two'))
  assert.ok(numbers.type === 'list' && numbers.ordered && numbers.items.length === 2)
})

test('fenced code keeps its text verbatim, including markdown-looking lines and an open fence', () => {
  const { blocks } = parseMarkdown('```json\n{"a": 1}\n# not a heading\n```\n\n```\nunterminated\n- item')
  assert.deepEqual(blocks.map((b) => b.type), ['code', 'code'])
  assert.ok(blocks[0].type === 'code' && blocks[0].lang === 'json' && blocks[0].text === '{"a": 1}\n# not a heading')
  assert.ok(blocks[1].type === 'code' && blocks[1].text === 'unterminated\n- item')
})

test('inline code, bold and italic are recognised and plain text is kept', () => {
  assert.deepEqual(parseInline('a `b` **c** *d* __e__ f'), [
    { kind: 'text', text: 'a ' }, { kind: 'code', text: 'b' }, { kind: 'text', text: ' ' },
    { kind: 'strong', text: 'c' }, { kind: 'text', text: ' ' }, { kind: 'em', text: 'd' },
    { kind: 'text', text: ' ' }, { kind: 'strong', text: 'e' }, { kind: 'text', text: ' f' },
  ])
  assert.deepEqual(parseInline('snake_case_name and 2 * 3 * 4'), [{ kind: 'text', text: 'snake_case_name and 2 * 3 * 4' }])
})

test('block quotes are joined', () => {
  const { blocks } = parseMarkdown('> one\n> two')
  assert.equal(blocks.length, 1)
  assert.ok(blocks[0].type === 'quote' && blocks[0].spans.map((s) => s.text).join('') === 'one two')
})

test('html and links stay text, so nothing can inject markup', () => {
  const { blocks } = parseMarkdown('<script>alert(1)</script> and [x](javascript:alert(1)) <img src=x onerror=alert(1)>')
  assert.equal(blocks.length, 1)
  const text = blocks[0].type === 'paragraph' ? blocks[0].spans.map((s) => s.text).join('') : ''
  assert.ok(text.includes('<script>alert(1)</script>') && text.includes('[x](javascript:alert(1))'))
  assert.ok(blocks[0].type === 'paragraph' && blocks[0].spans.every((s) => s.kind === 'text'))
})

test('oversize input is cut and flagged, and empty input has no blocks', () => {
  const huge = parseMarkdown('word '.repeat(MARKDOWN_LIMIT))
  assert.equal(huge.truncated, true)
  assert.ok(huge.blocks[0].type === 'paragraph' && huge.blocks[0].spans[0].text.length <= MARKDOWN_LIMIT)
  assert.deepEqual(parseMarkdown(''), { blocks: [], truncated: false })
})

test('windows line endings parse like unix ones', () => {
  assert.deepEqual(parseMarkdown('# A\r\n\r\ntext\r\n').blocks.map((b) => b.type), ['heading', 'paragraph'])
})
