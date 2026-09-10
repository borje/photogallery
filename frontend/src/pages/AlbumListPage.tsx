import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import AlbumCard from '@/components/AlbumCard'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

export default function AlbumListPage() {
  const { data, isPending, isError, error } = useQuery({ queryKey: ['albums'], queryFn: api.listAlbums })

  if (isPending) {
    return (
      <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Loading albums">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="space-y-3">
            <Skeleton className="aspect-[4/3] w-full" />
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
          </div>
        ))}
      </div>
    )
  }
  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load albums</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }
  if (data.length === 0) {
    return <p className="py-24 text-center text-muted-foreground">No albums have been published yet.</p>
  }
  return (
    <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
      {data.map((album) => (
        <AlbumCard key={album.slug} album={album} />
      ))}
    </div>
  )
}
