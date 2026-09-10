import { Link } from 'react-router'
import { ChevronRight } from 'lucide-react'
import type { Crumb } from '@/api/types'

/** Ancestor trail, root first. Renders nothing for an empty trail. */
export default function Breadcrumb({ crumbs }: { crumbs?: Crumb[] }) {
  if (!crumbs || crumbs.length === 0) return null
  return (
    <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
      <Link to="/" aria-label="Gallery" className="hover:text-foreground hover:underline">
        🏠
      </Link>
      {crumbs.map((c) => (
        <span key={c.slug} className="flex items-center gap-1">
          <ChevronRight className="size-3.5" aria-hidden="true" />
          <Link to={`/f/${c.slug}`} className="hover:text-foreground hover:underline">
            {c.name}
          </Link>
        </span>
      ))}
    </nav>
  )
}
