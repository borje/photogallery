import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { renderApp } from '@/test/render'
import { summerAlbum } from '@/test/fixtures'
import { formatDateRange } from '@/lib/format'

describe('AlbumListPage', () => {
  it('renders cards with cover, count, dates and lock state', async () => {
    renderApp('/')
    const wedding = await screen.findByRole('link', { name: 'Wedding' })
    expect(within(wedding).getByText(/120 photos/)).toBeInTheDocument()
    expect(within(wedding).getByRole('img', { name: 'Password protected' })).toBeInTheDocument()
    const coverImgs = wedding.querySelectorAll('img')
    expect(coverImgs[0]).toHaveAttribute('src', '/api/albums/wedding/cover')
    expect(coverImgs[0].className).toContain('blur')

    const summer = screen.getByRole('link', { name: 'Summer 2026' })
    expect(summer).toHaveAttribute('href', '/a/summer-2026')
    expect(within(summer).getByText(/2 photos/)).toBeInTheDocument()
    const dates = formatDateRange(summerAlbum.taken_from, summerAlbum.taken_to)
    expect(within(summer).getByText((_, el) => el?.tagName === 'P' && (el.textContent ?? '').includes(dates))).toBeInTheDocument()
    expect(within(summer).queryByRole('img', { name: 'Password protected' })).toBeNull()

    const empty = screen.getByRole('link', { name: 'Empty' })
    expect(within(empty).getByText('0 photos')).toBeInTheDocument()
    expect(empty.querySelector('img')).toBeNull()
  })

  it('shows an empty state and errors', async () => {
    server.use(http.get('/api/albums', () => HttpResponse.json({ albums: [] })))
    renderApp('/')
    expect(await screen.findByText(/No albums have been published yet/)).toBeInTheDocument()

    server.use(http.get('/api/albums', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderApp('/')
    expect(await screen.findByText(/Could not load albums/)).toBeInTheDocument()
  })
})
