import { Link, Outlet, Route, Routes } from 'react-router'
import { Camera } from 'lucide-react'
import AlbumListPage from '@/pages/AlbumListPage'
import AlbumPage from '@/pages/AlbumPage'
import NotFoundPage from '@/pages/NotFoundPage'

function Layout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b border-border/60 bg-background/75 backdrop-blur-md">
        <div className="mx-auto flex max-w-7xl items-center gap-2 px-4 py-3">
          <Link
            to="/"
            className="flex items-center gap-2.5 font-semibold tracking-tight transition-opacity hover:opacity-80"
          >
            <span className="flex size-7 items-center justify-center rounded-md bg-muted text-foreground">
              <Camera className="size-4" aria-hidden="true" />
            </span>
            Photo Gallery
          </Link>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-4 py-8">
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
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
