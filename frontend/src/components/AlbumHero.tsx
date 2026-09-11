import { useState } from 'react'
import { Download, Lock } from 'lucide-react'
import type { AlbumDetail, Photo } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { buildSrcSet } from '@/lib/photos'
import { formatDateRange, photoCount } from '@/lib/format'
import { cn } from '@/lib/utils'

// The gallery is capped at max-w-screen-2xl with 16px side padding.
const HERO_SIZES = '(max-width: 1536px) calc(100vw - 32px), 1504px'

function Meta({ album, className }: { album: AlbumDetail; className?: string }) {
  const dates = formatDateRange(album.taken_from, album.taken_to)
  return (
    <p className={cn('text-sm', className)}>
      {photoCount(album.photo_count)}
      {dates && <> · {dates}</>}
    </p>
  )
}

function Title({ album, className }: { album: AlbumDetail; className?: string }) {
  return (
    <h1 className={cn('flex flex-wrap items-center gap-x-3 gap-y-1 font-semibold tracking-tight text-balance', className)}>
      {album.name}
      {album.locked && (
        <Badge variant="secondary" className="gap-1 align-middle">
          <Lock className="size-3" aria-hidden="true" /> Protected
        </Badge>
      )}
    </h1>
  )
}

function DownloadButton({ album, className }: { album: AlbumDetail; className?: string }) {
  if (album.photos.length === 0) return null
  return (
    <Button asChild variant="outline" className={className}>
      <a href={album.download_url} download>
        <Download className="size-4" aria-hidden="true" />
        Download album (zip)
      </a>
    </Button>
  )
}

/**
 * Album header. With a cover photo it is a wide image with the title set
 * into its lower edge; without one (empty album) it is a plain text header.
 *
 * The cover fades in over a blurred blow-up of its own thumbnail, so the
 * block has the photo's colour from the first paint instead of a grey box.
 */
export default function AlbumHero({ album, cover }: { album: AlbumDetail; cover?: Photo }) {
  const [loaded, setLoaded] = useState(false)

  if (!cover) {
    return (
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <Title album={album} className="text-3xl" />
          <Meta album={album} className="text-muted-foreground" />
          {album.description && <p className="max-w-prose pt-2 text-muted-foreground">{album.description}</p>}
        </div>
        <DownloadButton album={album} />
      </div>
    )
  }

  const srcSet = buildSrcSet(cover)
    .map((e) => `${e.src} ${e.width}w`)
    .join(', ')

  return (
    <div className="space-y-5">
      <section
        data-testid="album-hero"
        className="relative aspect-[4/3] max-h-[70vh] min-h-[280px] w-full overflow-hidden rounded-2xl bg-muted ring-1 ring-foreground/10 sm:aspect-[2/1] lg:aspect-[21/9]"
      >
        <img
          src={cover.urls.thumb}
          alt=""
          aria-hidden="true"
          className="absolute inset-0 size-full scale-110 object-cover blur-2xl"
        />
        <img
          src={cover.urls.medium}
          srcSet={srcSet}
          sizes={HERO_SIZES}
          alt=""
          fetchPriority="high"
          decoding="async"
          onLoad={() => setLoaded(true)}
          className={cn(
            'absolute inset-0 size-full object-cover transition-opacity duration-700 ease-out',
            loaded ? 'opacity-100' : 'opacity-0',
          )}
        />
        <div
          aria-hidden="true"
          className="absolute inset-x-0 bottom-0 h-3/4 bg-gradient-to-t from-black/80 via-black/35 to-transparent"
        />
        <div className="absolute inset-x-0 bottom-0 flex flex-wrap items-end justify-between gap-x-6 gap-y-4 p-5 sm:p-8">
          <div className="space-y-2 text-white [text-shadow:0_1px_2px_rgb(0_0_0/0.4)]">
            <Meta album={album} className="text-xs font-medium uppercase tracking-[0.18em] text-white/75" />
            <Title album={album} className="text-3xl leading-[1.05] sm:text-5xl" />
          </div>
          <DownloadButton
            album={album}
            className="border-white/25 bg-white/10 text-white backdrop-blur-md hover:bg-white/20 hover:text-white"
          />
        </div>
      </section>
      {album.description && (
        <p className="max-w-prose text-base leading-relaxed text-muted-foreground">{album.description}</p>
      )}
    </div>
  )
}
