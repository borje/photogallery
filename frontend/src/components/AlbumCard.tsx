import { Link } from 'react-router'
import { ImageOff, Lock } from 'lucide-react'
import type { AlbumSummary } from '@/api/types'
import { formatDateRange, photoCount } from '@/lib/format'

// Editorial card: the cover is the card, the text sits below it on the page
// background. Hover lifts the image slightly; focus draws the ring on the cover.
export default function AlbumCard({ album }: { album: AlbumSummary }) {
  const dates = formatDateRange(album.taken_from, album.taken_to)
  return (
    <Link to={`/a/${album.slug}`} className="group block focus:outline-none" aria-label={album.name}>
      <div className="relative aspect-[4/3] w-full overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10 transition-shadow group-hover:shadow-lg group-hover:shadow-black/40 group-focus-visible:ring-2 group-focus-visible:ring-ring">
        {album.cover_url ? (
          <img
            src={album.cover_url}
            alt=""
            loading="lazy"
            className={
              'size-full object-cover transition-[transform,filter] duration-300 ease-out group-hover:scale-[1.03] group-hover:brightness-110 ' +
              (album.locked ? 'scale-110 blur-md' : '')
            }
          />
        ) : (
          <div className="flex size-full items-center justify-center text-muted-foreground">
            <ImageOff className="size-8" aria-hidden="true" />
          </div>
        )}
        {album.locked && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/30 text-white">
            <Lock className="size-10 drop-shadow" aria-label="Password protected" role="img" />
          </div>
        )}
      </div>
      <div className="space-y-0.5 px-1 pt-3">
        <h2 className="truncate font-medium leading-tight">{album.name}</h2>
        <p className="text-sm text-muted-foreground">
          {photoCount(album.photo_count)}
          {dates && <> · {dates}</>}
        </p>
      </div>
    </Link>
  )
}
