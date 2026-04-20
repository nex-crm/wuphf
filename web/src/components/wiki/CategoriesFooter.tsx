/** Chip-style category tags row above the page footer. */

interface CategoriesFooterProps {
  tags: string[]
  onSelect?: (tag: string) => void
}

export default function CategoriesFooter({ tags, onSelect }: CategoriesFooterProps) {
  if (tags.length === 0) return null
  return (
    <div className="wk-categories" aria-label="Categories">
      <span className="wk-label">Categories:</span>
      {tags.map((tag) => (
        <a
          key={tag}
          href={`#/wiki?category=${encodeURIComponent(tag)}`}
          onClick={(e) => {
            if (onSelect) {
              e.preventDefault()
              onSelect(tag)
            }
          }}
        >
          {tag}
        </a>
      ))}
    </div>
  )
}
