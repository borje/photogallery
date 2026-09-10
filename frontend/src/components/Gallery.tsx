import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { RowsPhotoAlbum } from 'react-photo-album'
import 'react-photo-album/rows.css'
import './Gallery.css'
import Lightbox from 'yet-another-react-lightbox'
import Captions from 'yet-another-react-lightbox/plugins/captions'
import Download from 'yet-another-react-lightbox/plugins/download'
import Zoom from 'yet-another-react-lightbox/plugins/zoom'
import 'yet-another-react-lightbox/styles.css'
import 'yet-another-react-lightbox/plugins/captions.css'
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

export default function Gallery({ photos }: { photos: Photo[] }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const items = useMemo(() => photos.map(toGalleryPhoto), [photos])
  const slides = useMemo(
    () => photos.map((p) => ({ ...toSlide(p), title: p.title, description: slideDescription(p) })),
    [photos],
  )

  // ?photo=<id> (the link Lightroom records per photo) opens the lightbox.
  const initial = photos.findIndex((p) => p.id === searchParams.get('photo'))
  const [index, setIndex] = useState(initial)
  useEffect(() => {
    if (initial >= 0) setIndex(initial)
  }, [initial])

  function close() {
    setIndex(-1)
    if (searchParams.has('photo')) {
      const next = new URLSearchParams(searchParams)
      next.delete('photo')
      setSearchParams(next, { replace: true })
    }
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
        onClick={({ index: i }) => setIndex(i)}
      />
      <Lightbox
        open={index >= 0}
        index={Math.max(index, 0)}
        slides={slides}
        close={close}
        on={{ view: ({ index: i }) => setIndex(i) }}
        plugins={[Captions, Zoom, Download]}
        captions={{ descriptionTextAlign: 'center', descriptionMaxLines: 4 }}
        zoom={{ maxZoomPixelRatio: 2 }}
        controller={{ closeOnBackdropClick: true }}
      />
    </>
  )
}
