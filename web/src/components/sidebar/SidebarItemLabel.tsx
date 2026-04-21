import { useEffect, useRef, type ReactNode } from 'react'

export function SidebarItemLabel({ children }: { children: ReactNode }) {
  const outerRef = useRef<HTMLSpanElement>(null)
  const innerRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const outer = outerRef.current
    const inner = innerRef.current
    if (!outer || !inner) return

    const update = () => {
      const overflows = inner.scrollWidth > outer.clientWidth + 1
      outer.dataset.overflow = overflows ? 'true' : 'false'
    }

    update()
    const ro = new ResizeObserver(update)
    ro.observe(outer)
    ro.observe(inner)
    return () => ro.disconnect()
  }, [children])

  return (
    <span ref={outerRef} className="sidebar-item-label">
      <span ref={innerRef} className="sidebar-item-label-inner">{children}</span>
    </span>
  )
}
