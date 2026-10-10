import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { cn } from '@/design/cn'
import { parseMarkdown } from '@shared/markdown'
import type { InlineSpan, MarkdownBlock } from '@shared/markdown'

function Spans({ spans }: { spans: InlineSpan[] }) {
  return (
    <>
      {spans.map((span, index) => {
        if (span.kind === 'code') {
          return (
            <code key={index} className="mono rounded-[3px] bg-bg-3 px-1 py-px text-[0.92em] text-fg-1">
              {span.text}
            </code>
          )
        }
        if (span.kind === 'strong') return <strong key={index} className="font-semibold text-fg-1">{span.text}</strong>
        if (span.kind === 'em') return <em key={index}>{span.text}</em>
        return <span key={index}>{span.text}</span>
      })}
    </>
  )
}

const HEADING_CLASS: Record<1 | 2 | 3 | 4, string> = {
  1: 'mt-4 mb-2 text-sm font-semibold text-fg-1',
  2: 'mt-3 mb-1.5 text-xs font-semibold text-fg-1',
  3: 'mt-3 mb-1 text-xs font-semibold text-fg-2',
  4: 'mt-2 mb-1 text-2xs font-semibold tracking-wide text-fg-3 uppercase',
}

function Block({ block }: { block: MarkdownBlock }): ReactNode {
  switch (block.type) {
    case 'heading':
      return (
        <div role="heading" aria-level={block.level + 1} className={HEADING_CLASS[block.level]}>
          <Spans spans={block.spans} />
        </div>
      )
    case 'paragraph':
      return (
        <p className="pretty my-1.5 text-xs leading-relaxed text-fg-2">
          <Spans spans={block.spans} />
        </p>
      )
    case 'list': {
      const List = block.ordered ? 'ol' : 'ul'
      return (
        <List className={cn('my-1.5 space-y-0.5 pl-5 text-xs leading-relaxed text-fg-2', block.ordered ? 'list-decimal' : 'list-disc')}>
          {block.items.map((item, index) => (
            <li key={index}>
              <Spans spans={item} />
            </li>
          ))}
        </List>
      )
    }
    case 'quote':
      return (
        <blockquote className="my-1.5 border-l-2 border-line-3 pl-3 text-xs leading-relaxed text-fg-3">
          <Spans spans={block.spans} />
        </blockquote>
      )
    case 'code':
      return (
        <pre className="mono my-2 max-h-72 overflow-auto rounded-[var(--radius-control)] border border-line-1 bg-inset p-2 text-[11px] leading-relaxed whitespace-pre text-fg-2">
          {block.text}
        </pre>
      )
    case 'rule':
      return <hr className="my-3 border-line-1" />
  }
}

/**
 * Model output as readable text. It renders a block tree built by `parseMarkdown`, so every
 * piece of text is escaped by React: a document containing HTML shows the HTML as text.
 */
export function Markdown({ source, className }: { source: string; className?: string }) {
  const parsed = useMemo(() => parseMarkdown(source), [source])
  return (
    <div className={cn('min-w-0 select-text', className)}>
      {parsed.blocks.map((block, index) => (
        <Block key={index} block={block} />
      ))}
      {parsed.truncated ? <p className="mt-2 text-2xs text-fg-4">… shortened for display. Open the file to read the rest.</p> : null}
    </div>
  )
}
