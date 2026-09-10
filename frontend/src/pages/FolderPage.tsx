import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router'
import { api, isApiError } from '@/api/client'
import Breadcrumb from '@/components/Breadcrumb'
import ChildGrid from '@/components/ChildGrid'
import NotFoundPage from '@/pages/NotFoundPage'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

export default function FolderPage() {
  const { slug = '' } = useParams<{ slug: string }>()
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['folder', slug],
    queryFn: () => api.getFolder(slug),
    retry: false,
  })

  if (isPending) {
    return (
      <div className="grid gap-x-6 gap-y-8 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Loading folder">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="space-y-3">
            <Skeleton className="aspect-[4/3] w-full" />
            <Skeleton className="h-4 w-2/3" />
          </div>
        ))}
      </div>
    )
  }
  if (isError) {
    if (isApiError(error, 404)) {
      return <NotFoundPage message="There is no folder at this address." />
    }
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the folder</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Breadcrumb crumbs={data.breadcrumb} current={data.name} />
        <h1 className="text-3xl font-semibold tracking-tight">{data.name}</h1>
      </div>
      <ChildGrid folders={data.folders} albums={data.albums} emptyMessage="This folder is empty." />
    </div>
  )
}
