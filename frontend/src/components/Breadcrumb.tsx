import { Link } from 'react-router'
import { ChevronRight } from 'lucide-react'
import type { Crumb } from '@/api/types'

/**
 * Full path, root first, ending at the current page. Ancestors (`crumbs`)
 * are links; `current` — the page you're already on — is plain text.
 */
export default function Breadcrumb({ crumbs, current }: { crumbs?: Crumb[]; current?: string }) {
  return (
    <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
      <Link to="/" aria-label="Smugbox" className="hover:text-foreground hover:underline">
        🏠
      </Link>
      {crumbs?.map((c) => (
        <span key={c.slug} className="flex items-center gap-1">
          <ChevronRight className="size-3.5" aria-hidden="true" />
          <Link to={`/f/${c.slug}`} className="hover:text-foreground hover:underline">
            {c.name}
          </Link>
        </span>
      ))}
      {current && (
        <span className="flex items-center gap-1">
          <ChevronRight className="size-3.5" aria-hidden="true" />
          <span aria-current="page" className="text-foreground">
            {current}
          </span>
        </span>
      )}
    </nav>
  )
}
