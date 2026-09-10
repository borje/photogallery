import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from 'react-router'
import { Download, Lock } from 'lucide-react'
import { api, isApiError } from '@/api/client'
import type { PasswordRequired } from '@/api/types'
import Gallery from '@/components/Gallery'
import PasswordGate from '@/components/PasswordGate'
import NotFoundPage from '@/pages/NotFoundPage'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateRange, photoCount } from '@/lib/format'

export default function AlbumPage() {
  const { slug = '' } = useParams<{ slug: string }>()
  const queryClient = useQueryClient()
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['album', slug],
    queryFn: () => api.getAlbum(slug),
    retry: false,
  })

  if (isPending) {
    return (
      <div className="space-y-6" aria-busy="true" aria-label="Loading album">
        <Skeleton className="h-8 w-1/3" />
        <Skeleton className="h-4 w-1/4" />
        <div className="grid gap-2 sm:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="aspect-[3/2] w-full" />
          ))}
        </div>
      </div>
    )
  }
  if (isError) {
    if (isApiError(error, 401, 'password_required')) {
      const info = (error.body ?? {}) as PasswordRequired
      return (
        <PasswordGate
          slug={slug}
          name={info.name}
          count={info.photo_count}
          onUnlocked={() => queryClient.invalidateQueries({ queryKey: ['album', slug] })}
        />
      )
    }
    if (isApiError(error, 404)) {
      return <NotFoundPage message="There is no album at this address." />
    }
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the album</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }

  const dates = formatDateRange(data.taken_from, data.taken_to)
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
            {data.name}
            {data.locked && (
              <Badge variant="secondary" className="gap-1">
                <Lock className="size-3" aria-hidden="true" /> Protected
              </Badge>
            )}
          </h1>
          <p className="text-sm text-muted-foreground">
            {photoCount(data.photo_count)}
            {dates && <> · {dates}</>}
          </p>
          {data.description && <p className="max-w-prose pt-2 text-muted-foreground">{data.description}</p>}
        </div>
        {data.photos.length > 0 && (
          <Button asChild variant="outline">
            <a href={data.download_url} download>
              <Download className="size-4" aria-hidden="true" />
              Download album (zip)
            </a>
          </Button>
        )}
      </div>
      <Gallery photos={data.photos} />
    </div>
  )
}
