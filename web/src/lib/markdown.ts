/**
 * Simple markdown-to-HTML formatter for trusted agent messages.
 * Mirrors the formatTrusted() function from index.legacy.html.
 *
 * SECURITY: Only use for messages from the broker (trusted source).
 * User-submitted content should be escaped before display.
 */

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

export function formatMarkdown(raw: string): string {
  if (!raw) return ''

  const lines = raw.split('\n')
  const result: string[] = []
  let inCodeBlock = false
  let codeLines: string[] = []
  let inList = false
  let listType = ''

  for (const line of lines) {
    // Code blocks
    if (line.trimStart().startsWith('```')) {
      if (inCodeBlock) {
        result.push(`<div class="msg-codeblock"><code>${escapeHtml(codeLines.join('\n'))}</code></div>`)
        codeLines = []
        inCodeBlock = false
      } else {
        if (inList) { result.push(`</${listType}>`); inList = false }
        inCodeBlock = true
      }
      continue
    }

    if (inCodeBlock) {
      codeLines.push(line)
      continue
    }

    // Close open list if current line is not a list item
    const isUl = /^\s*[-*]\s/.test(line)
    const isOl = /^\s*\d+\.\s/.test(line)
    if (inList && !isUl && !isOl && line.trim() !== '') {
      result.push(`</${listType}>`)
      inList = false
    }

    const trimmed = line.trim()

    // Empty line
    if (trimmed === '') {
      if (inList) { result.push(`</${listType}>`); inList = false }
      result.push('<br/>')
      continue
    }

    // Headings
    if (trimmed.startsWith('### ')) { result.push(`<div class="msg-h3">${formatInline(trimmed.slice(4))}</div>`); continue }
    if (trimmed.startsWith('## ')) { result.push(`<div class="msg-h2">${formatInline(trimmed.slice(3))}</div>`); continue }
    if (trimmed.startsWith('# ')) { result.push(`<div class="msg-h1">${formatInline(trimmed.slice(2))}</div>`); continue }

    // Horizontal rule
    if (/^---+$/.test(trimmed) || /^\*\*\*+$/.test(trimmed)) { result.push('<hr class="msg-hr"/>'); continue }

    // Blockquote
    if (trimmed.startsWith('> ')) { result.push(`<div class="msg-blockquote">${formatInline(trimmed.slice(2))}</div>`); continue }

    // Unordered list
    if (isUl) {
      if (!inList || listType !== 'ul') {
        if (inList) result.push(`</${listType}>`)
        result.push('<ul class="msg-ul">')
        inList = true
        listType = 'ul'
      }
      result.push(`<li>${formatInline(trimmed.replace(/^\s*[-*]\s/, ''))}</li>`)
      continue
    }

    // Ordered list
    if (isOl) {
      if (!inList || listType !== 'ol') {
        if (inList) result.push(`</${listType}>`)
        result.push('<ol class="msg-ol">')
        inList = true
        listType = 'ol'
      }
      result.push(`<li>${formatInline(trimmed.replace(/^\s*\d+\.\s/, ''))}</li>`)
      continue
    }

    // Regular paragraph
    result.push(`<span>${formatInline(trimmed)}</span><br/>`)
  }

  // Close any open blocks
  if (inCodeBlock) {
    result.push(`<div class="msg-codeblock"><code>${escapeHtml(codeLines.join('\n'))}</code></div>`)
  }
  if (inList) {
    result.push(`</${listType}>`)
  }

  return result.join('')
}

function formatInline(text: string): string {
  let s = escapeHtml(text)
  // Bold
  s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  // Italic
  s = s.replace(/\*(.+?)\*/g, '<em>$1</em>')
  // Inline code
  s = s.replace(/`([^`]+)`/g, '<code>$1</code>')
  // Links
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a class="msg-link" href="$2" target="_blank" rel="noopener">$1</a>')
  // @mentions
  s = s.replace(/@(\w[\w-]*)/g, '<span class="mention">@$1</span>')
  return s
}
