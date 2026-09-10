import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { RowsPhotoAlbum } from 'react-photo-album'
import 'react-photo-album/rows.css'
import './Gallery.css'
import Lightbox, { IconButton, createIcon, useLightboxState } from 'yet-another-react-lightbox'
import Captions from 'yet-another-react-lightbox/plugins/captions'
import Counter from 'yet-another-react-lightbox/plugins/counter'
import Download from 'yet-another-react-lightbox/plugins/download'
import Fullscreen from 'yet-another-react-lightbox/plugins/fullscreen'
import Slideshow from 'yet-another-react-lightbox/plugins/slideshow'
import Thumbnails from 'yet-another-react-lightbox/plugins/thumbnails'
import Zoom from 'yet-another-react-lightbox/plugins/zoom'
import 'yet-another-react-lightbox/styles.css'
import 'yet-another-react-lightbox/plugins/captions.css'
import 'yet-another-react-lightbox/plugins/counter.css'
import 'yet-another-react-lightbox/plugins/thumbnails.css'
import type { Photo } from '@/api/types'
import { formatDate } from '@/lib/format'
import { exifSummary, toGalleryPhoto, toSlide } from '@/lib/photos'

function slideDescription(photo: Photo) {
  const meta = [formatDate(photo.taken_at), exifSummary(photo.exif)].filter(Boolean).join(' · ')
  if (!photo.caption && !meta) return undefined
  return (
    <>
      {photo.caption && <div>{photo.caption}</div>}
      {meta && <div className="text-xs opacity-75">{meta}</div>}
    </>
  )
}

/** Link to the current page with `?photo=<id>`, i.e. what the lightbox itself syncs to. */
function photoLink(id: string): string {
  const url = new URL(window.location.href)
  url.searchParams.set('photo', id)
  url.hash = ''
  return url.toString()
}

declare module 'yet-another-react-lightbox' {
  interface Labels {
    Share?: string
    'Link copied'?: string
  }
}

const ShareIcon = createIcon(
  'Share',
  <path d="m16 5-1.42 1.42-1.59-1.59V16h-1.98V4.83L9.42 6.42 8 5l4-4 4 4zm4 5v11c0 1.1-.9 2-2 2H6c-1.11 0-2-.9-2-2V10c0-1.11.89-2 2-2h3v2H6v11h12V10h-3V8h3c1.1 0 2 .89 2 2z" />,
)
const CheckIcon = createIcon('Check', <path d="M9 16.17 4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z" />)

// Toolbar button that shares a link to the current photo. The bundled Share
// plugin hides itself when navigator.canShare is missing (desktop Firefox,
// older Safari); this one falls back to copying the link to the clipboard.
function ShareButton({ photos }: { photos: Photo[] }) {
  const { currentIndex } = useLightboxState()
  const [copied, setCopied] = useState(false)

  async function share() {
    const photo = photos[currentIndex]
    if (!photo) return
    const data = { url: photoLink(photo.id), title: photo.title || photo.filename }
    if (typeof navigator.share === 'function') {
      await navigator.share(data).catch(() => {}) // user dismissed the share sheet
      return
    }
    await navigator.clipboard.writeText(data.url)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <IconButton
      label={copied ? 'Link copied' : 'Share'}
      icon={copied ? CheckIcon : ShareIcon}
      onClick={() => void share()}
    />
  )
}

export default function Gallery({ photos }: { photos: Photo[] }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const items = useMemo(() => photos.map(toGalleryPhoto), [photos])
  const slides = useMemo(
    () => photos.map((p) => ({ ...toSlide(p), title: p.title, description: slideDescription(p) })),
    [photos],
  )

  // `?photo=<id>` is the lightbox state: the link Lightroom records per photo
  // opens it, opening pushes a history entry (so Back closes it), and arrowing
  // through slides replaces the entry so the URL is always shareable.
  const photoParam = searchParams.get('photo')
  const index = photoParam ? photos.findIndex((p) => p.id === photoParam) : -1

  function setPhoto(id: string | null, replace: boolean) {
    const next = new URLSearchParams(searchParams)
    if (id) next.set('photo', id)
    else next.delete('photo')
    setSearchParams(next, { replace })
  }

  if (photos.length === 0) {
    return <p className="py-24 text-center text-muted-foreground">This album has no photos yet.</p>
  }

  return (
    <>
      <RowsPhotoAlbum
        photos={items}
        targetRowHeight={260}
        spacing={6}
        defaultContainerWidth={1168}
        sizes={{ size: '1168px', sizes: [{ viewport: '(max-width: 1200px)', size: 'calc(100vw - 32px)' }] }}
        breakpoints={[360, 600, 900, 1200]}
        onClick={({ index: i }) => setPhoto(photos[i].id, false)}
      />
      <Lightbox
        open={index >= 0}
        index={Math.max(index, 0)}
        slides={slides}
        close={() => setPhoto(null, true)}
        on={{
          view: ({ index: i }) => {
            if (photos[i] && photos[i].id !== photoParam) setPhoto(photos[i].id, true)
          },
        }}
        plugins={[Captions, Counter, Fullscreen, Slideshow, Thumbnails, Zoom, Download]}
        toolbar={{ buttons: [<ShareButton key="share" photos={photos} />, 'close'] }}
        captions={{ descriptionTextAlign: 'center', descriptionMaxLines: 4 }}
        counter={{ container: { style: { top: 'unset', bottom: 0 } } }}
        slideshow={{ delay: 4000 }}
        thumbnails={{ width: 96, height: 64, border: 0, gap: 8, padding: 0, imageFit: 'cover' }}
        zoom={{ maxZoomPixelRatio: 2 }}
        controller={{ closeOnBackdropClick: true }}
      />
    </>
  )
}
