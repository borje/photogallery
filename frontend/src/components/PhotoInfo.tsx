import { useEffect } from 'react'
import { IconButton, createIcon, stopNavigationEventsPropagation, useLightboxState } from 'yet-another-react-lightbox'
import { X } from 'lucide-react'
import type { Photo } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { photoDetails } from '@/lib/photos'

declare module 'yet-another-react-lightbox' {
  interface Labels {
    'Show info'?: string
    'Hide info'?: string
  }
}

const InfoIcon = createIcon(
  'Info',
  <path d="M11 17h2v-6h-2v6zm1-15C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zM11 9h2V7h-2v2z" />,
)

interface Props {
  photos: Photo[]
  open: boolean
  onToggle: (open: boolean) => void
}

/** Toolbar button that toggles the details panel. Also bound to the "i" key. */
export function InfoButton({ open, onToggle }: Omit<Props, 'photos'>) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== 'i' || e.altKey || e.ctrlKey || e.metaKey) return
      if (e.target instanceof HTMLElement && /^(input|textarea|select)$/i.test(e.target.tagName)) return
      onToggle(!open)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onToggle])

  return (
    <IconButton
      label={open ? 'Hide info' : 'Show info'}
      icon={InfoIcon}
      aria-pressed={open}
      onClick={() => onToggle(!open)}
    />
  )
}

/**
 * Details panel for the current slide. Rendered through the lightbox
 * `render.controls` slot, so it sits inside the lightbox container;
 * Gallery.css pads the container so the photo shrinks to make room instead
 * of being covered.
 */
export function InfoPanel({ photos, open, onToggle }: Props) {
  const { currentIndex } = useLightboxState()
  const photo = photos[currentIndex]
  if (!open || !photo) return null

  return (
    <aside className="sb-info" aria-label="Photo details" {...stopNavigationEventsPropagation()}>
      <header className="flex items-start justify-between gap-3">
        <h2 className="text-base font-medium leading-snug break-words">{photo.title || photo.filename}</h2>
        <button
          type="button"
          aria-label="Hide info"
          className="-mr-1 -mt-1 shrink-0 rounded-md p-1 text-muted-foreground hover:text-foreground"
          onClick={() => onToggle(false)}
        >
          <X className="size-4" />
        </button>
      </header>
      {photo.caption && <p className="mt-2 text-sm text-muted-foreground whitespace-pre-line">{photo.caption}</p>}
      <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        {photoDetails(photo).map(({ label, value }) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all">{value}</dd>
          </div>
        ))}
      </dl>
      {photo.keywords && photo.keywords.length > 0 && (
        <ul className="mt-4 flex flex-wrap gap-1.5" aria-label="Keywords">
          {photo.keywords.map((k) => (
            <li key={k}>
              <Badge variant="secondary">{k}</Badge>
            </li>
          ))}
        </ul>
      )}
    </aside>
  )
}
