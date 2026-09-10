import { Link, Outlet, Route, Routes } from 'react-router'
import { Camera } from 'lucide-react'
import AlbumListPage from '@/pages/AlbumListPage'
import AlbumPage from '@/pages/AlbumPage'
import NotFoundPage from '@/pages/NotFoundPage'

function Layout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-7xl items-center gap-2 px-4 py-3">
          <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
            <Camera className="size-5" aria-hidden="true" />
            Photo Gallery
          </Link>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-4 py-6">
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
