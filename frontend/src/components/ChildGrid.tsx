import type { AlbumSummary, FolderSummary } from '@/api/types'
import AlbumCard from '@/components/AlbumCard'
import FolderCard from '@/components/FolderCard'

// Folders first, then albums — both already arrive newest-content-first
// from the backend. Shared by the root page and folder pages.
export default function ChildGrid({
  folders,
  albums,
  emptyMessage = 'No albums have been published yet.',
}: {
  folders: FolderSummary[]
  albums: AlbumSummary[]
  emptyMessage?: string
}) {
  if (folders.length === 0 && albums.length === 0) {
    return <p className="py-24 text-center text-muted-foreground">{emptyMessage}</p>
  }
  return (
    <div className="grid gap-x-6 gap-y-8 sm:grid-cols-2 lg:grid-cols-3">
      {folders.map((folder) => (
        <FolderCard key={folder.slug} folder={folder} />
      ))}
      {albums.map((album) => (
        <AlbumCard key={album.slug} album={album} />
      ))}
    </div>
  )
}
