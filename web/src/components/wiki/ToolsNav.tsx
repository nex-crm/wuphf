/** Left-sidebar "Tools" section — Wikipedia's utility links. */

export interface ToolsNavItem {
  label: string
  onClick?: () => void
  href?: string
}

interface ToolsNavProps {
  items?: ToolsNavItem[]
}

const DEFAULT_TOOLS: ToolsNavItem[] = [
  { label: 'Recent changes' },
  { label: 'Random article' },
  { label: 'All pages' },
  { label: 'Orphan articles' },
  { label: 'Cite this page' },
  { label: 'git clone wiki' },
]

export default function ToolsNav({ items = DEFAULT_TOOLS }: ToolsNavProps) {
  return (
    <>
      <hr className="tools-sep" />
      <h3>Tools</h3>
      <ul className="tools">
        {items.map((item) => (
          <li key={item.label}>
            <a
              href={item.href ?? '#'}
              onClick={(e) => {
                if (item.onClick) {
                  e.preventDefault()
                  item.onClick()
                }
              }}
            >
              {item.label}
            </a>
          </li>
        ))}
      </ul>
    </>
  )
}
