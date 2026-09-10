import { Link } from 'react-router'
import { ImageOff } from 'lucide-react'
import type { FolderSummary } from '@/api/types'

// Same card shape as AlbumCard: cover is the card, name sits below it.
// No stats, no lock overlay — folders never carry a password.
export default function FolderCard({ folder }: { folder: FolderSummary }) {
  return (
    <Link to={`/f/${folder.slug}`} className="group block focus:outline-none" aria-label={folder.name}>
      <div className="relative aspect-[4/3] w-full overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10 transition-shadow group-hover:shadow-lg group-hover:shadow-black/40 group-focus-visible:ring-2 group-focus-visible:ring-ring">
        {folder.cover_url ? (
          <img
            src={folder.cover_url}
            alt=""
            loading="lazy"
            className="size-full object-cover transition-[transform,filter] duration-300 ease-out group-hover:scale-[1.03] group-hover:brightness-110"
          />
        ) : (
          <div className="flex size-full items-center justify-center text-muted-foreground">
            <ImageOff className="size-8" aria-hidden="true" />
          </div>
        )}
      </div>
      <div className="space-y-0.5 px-1 pt-3">
        <h2 className="truncate font-medium leading-tight">{folder.name}</h2>
      </div>
    </Link>
  )
}
