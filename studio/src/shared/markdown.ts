/**
 * A small, safe Markdown reader for model output shown to a human (plans, summaries).
 *
 * It returns a block tree, never HTML: the renderer turns it into React elements, which escape
 * all text, so a document containing <script> or raw HTML is displayed as text. Covers what
 * models actually write (headings, paragraphs, lists, fenced code, quotes, rules, bold, italic,
 * inline code); links, images and tables are left as plain text.
 */

export const MARKDOWN_LIMIT = 40_000

export type InlineKind = 'text' | 'code' | 'strong' | 'em'
export interface InlineSpan {
  kind: InlineKind
  text: string
}

export type MarkdownBlock =
  | { type: 'heading'; level: 1 | 2 | 3 | 4; spans: InlineSpan[] }
  | { type: 'paragraph'; spans: InlineSpan[] }
  | { type: 'list'; ordered: boolean; items: InlineSpan[][] }
  | { type: 'quote'; spans: InlineSpan[] }
  | { type: 'code'; lang?: string; text: string }
  | { type: 'rule' }

export interface ParsedMarkdown {
  blocks: MarkdownBlock[]
  /** The source was longer than MARKDOWN_LIMIT and was cut. */
  truncated: boolean
}

const INLINE = /(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(__[^_\n]+__)|(\*[^*\s][^*\n]*\*)|(\b_[^_\s][^_\n]*_\b)/g

export function parseInline(text: string): InlineSpan[] {
  const spans: InlineSpan[] = []
  let last = 0
  for (const match of text.matchAll(INLINE)) {
    const index = match.index ?? 0
    if (index > last) spans.push({ kind: 'text', text: text.slice(last, index) })
    const raw = match[0]
    if (match[1]) spans.push({ kind: 'code', text: raw.slice(1, -1) })
    else if (match[2] || match[3]) spans.push({ kind: 'strong', text: raw.slice(2, -2) })
    else spans.push({ kind: 'em', text: raw.slice(1, -1) })
    last = index + raw.length
  }
  if (last < text.length) spans.push({ kind: 'text', text: text.slice(last) })
  return spans.length > 0 ? spans : [{ kind: 'text', text }]
}

const FENCE = /^\s*(```|~~~)\s*([\w+-]*)\s*$/
const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/
const BULLET = /^(\s*)[-*+]\s+(.*)$/
const NUMBERED = /^(\s*)\d+[.)]\s+(.*)$/
const RULE = /^\s*([-*_])(\s*\1){2,}\s*$/

export function parseMarkdown(source: string): ParsedMarkdown {
  const truncated = source.length > MARKDOWN_LIMIT
  const lines = (truncated ? source.slice(0, MARKDOWN_LIMIT) : source).replace(/\r\n?/g, '\n').split('\n')
  const blocks: MarkdownBlock[] = []
  let paragraph: string[] = []

  const flushParagraph = () => {
    if (paragraph.length > 0) blocks.push({ type: 'paragraph', spans: parseInline(paragraph.join(' ')) })
    paragraph = []
  }

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i]
    const fence = FENCE.exec(line)
    if (fence) {
      flushParagraph()
      const body: string[] = []
      i += 1
      while (i < lines.length && !FENCE.test(lines[i])) {
        body.push(lines[i])
        i += 1
      }
      blocks.push({ type: 'code', lang: fence[2] || undefined, text: body.join('\n') })
      continue
    }
    if (line.trim() === '') {
      flushParagraph()
      continue
    }
    const heading = HEADING.exec(line)
    if (heading) {
      flushParagraph()
      blocks.push({ type: 'heading', level: Math.min(heading[1].length, 4) as 1 | 2 | 3 | 4, spans: parseInline(heading[2]) })
      continue
    }
    if (RULE.test(line)) {
      flushParagraph()
      blocks.push({ type: 'rule' })
      continue
    }
    const bullet = BULLET.exec(line)
    const numbered = bullet ? null : NUMBERED.exec(line)
    const item = bullet ?? numbered
    if (item) {
      flushParagraph()
      const ordered = numbered !== null
      const last = blocks[blocks.length - 1]
      const items =
        last?.type === 'list' && last.ordered === ordered
          ? last.items
          : (blocks.push({ type: 'list', ordered, items: [] }), (blocks[blocks.length - 1] as Extract<MarkdownBlock, { type: 'list' }>).items)
      items.push(parseInline(item[2]))
      continue
    }
    if (line.startsWith('>')) {
      flushParagraph()
      const text = line.replace(/^>\s?/, '')
      const last = blocks[blocks.length - 1]
      if (last?.type === 'quote') last.spans.push({ kind: 'text', text: ' ' }, ...parseInline(text))
      else blocks.push({ type: 'quote', spans: parseInline(text) })
      continue
    }
    // An indented line after a list item continues that item instead of starting a paragraph.
    const last = blocks[blocks.length - 1]
    if (/^\s{2,}\S/.test(line) && paragraph.length === 0 && last?.type === 'list') {
      const lastItem = last.items[last.items.length - 1]
      lastItem.push({ kind: 'text', text: ' ' }, ...parseInline(line.trim()))
      continue
    }
    paragraph.push(line.trim())
  }
  flushParagraph()
  return { blocks, truncated }
}
