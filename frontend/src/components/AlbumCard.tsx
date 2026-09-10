import { Link } from 'react-router'
import { ImageOff, Lock } from 'lucide-react'
import { Card } from '@/components/ui/card'
import type { AlbumSummary } from '@/api/types'
import { formatDateRange, photoCount } from '@/lib/format'

export default function AlbumCard({ album }: { album: AlbumSummary }) {
  const dates = formatDateRange(album.taken_from, album.taken_to)
  return (
    <Link to={`/a/${album.slug}`} className="group block focus:outline-none" aria-label={album.name}>
      <Card className="overflow-hidden p-0 transition-shadow group-hover:shadow-md group-focus-visible:ring-2 group-focus-visible:ring-ring">
        <div className="relative aspect-[4/3] w-full overflow-hidden bg-muted">
          {album.cover_url ? (
            <img
              src={album.cover_url}
              alt=""
              loading="lazy"
              className={
                'size-full object-cover transition-transform group-hover:scale-[1.02] ' +
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
        <div className="space-y-1 px-4 pb-4 pt-3">
          <h2 className="truncate font-medium leading-tight">{album.name}</h2>
          <p className="text-sm text-muted-foreground">
            {photoCount(album.photo_count)}
            {dates && <> · {dates}</>}
          </p>
        </div>
      </Card>
    </Link>
  )
}
