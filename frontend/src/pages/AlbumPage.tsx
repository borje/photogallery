import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from 'react-router'
import { api, isApiError } from '@/api/client'
import type { PasswordRequired } from '@/api/types'
import AlbumHero from '@/components/AlbumHero'
import Breadcrumb from '@/components/Breadcrumb'
import Gallery from '@/components/Gallery'
import PasswordGate from '@/components/PasswordGate'
import NotFoundPage from '@/pages/NotFoundPage'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

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
        <Skeleton className="h-4 w-1/4" />
        <Skeleton className="aspect-[4/3] max-h-[70vh] min-h-[280px] w-full rounded-2xl sm:aspect-[2/1] lg:aspect-[21/9]" />
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
          breadcrumb={info.breadcrumb}
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

  const cover = data.cover_photo_id ? data.photos.find((p) => p.id === data.cover_photo_id) : undefined
  return (
    <div className="space-y-6">
      <Breadcrumb crumbs={data.breadcrumb} current={data.name} />
      <AlbumHero album={data} cover={cover} />
      <Gallery photos={data.photos} />
    </div>
  )
}
