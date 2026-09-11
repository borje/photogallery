import { useEffect } from 'react'
import { Link, Outlet, Route, Routes } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Camera } from 'lucide-react'
import { api } from '@/api/client'
import AlbumListPage from '@/pages/AlbumListPage'
import AlbumPage from '@/pages/AlbumPage'
import FolderPage from '@/pages/FolderPage'
import NotFoundPage from '@/pages/NotFoundPage'

const DEFAULT_TITLE = 'Smugbox'

function Layout() {
  const { data } = useQuery({ queryKey: ['site-config'], queryFn: api.getSiteConfig })
  const title = data?.title || DEFAULT_TITLE

  useEffect(() => {
    document.title = title
  }, [title])

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b border-border/60 bg-background/75 backdrop-blur-md">
        <div className="mx-auto flex max-w-screen-2xl items-center gap-2 px-4 py-3">
          <Link
            to="/"
            className="flex items-center gap-2.5 font-semibold tracking-tight transition-opacity hover:opacity-80"
          >
            <span className="flex size-7 items-center justify-center rounded-md bg-muted text-foreground">
              <Camera className="size-4" aria-hidden="true" />
            </span>
            {title}
          </Link>
        </div>
      </header>
      <main className="mx-auto max-w-screen-2xl px-4 py-8">
        <Outlet />
      </main>
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<AlbumListPage />} />
        <Route path="a/:slug" element={<AlbumPage />} />
        <Route path="f/:slug" element={<FolderPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
