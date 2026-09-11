import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { emptyAlbum } from '@/test/fixtures'
import { unlocked } from '@/test/handlers'
import { renderApp } from '@/test/render'

describe('AlbumPage', () => {
  beforeEach(() => unlocked.clear())

  it('renders a public album with gallery, srcSet and download links', async () => {
    renderApp('/a/summer-2026')
    expect(await screen.findByRole('heading', { name: /Summer 2026/ })).toBeInTheDocument()
    expect(screen.getByText('Two weeks by the sea.')).toBeInTheDocument()
    expect(screen.getByText(/2 photos/)).toBeInTheDocument()

    const zip = screen.getByRole('link', { name: /Download album/ })
    expect(zip).toHaveAttribute('href', '/api/albums/summer-2026/download')

    // The hero shows the album's cover photo (p2, not the first photo) from
    // the real variants, with a blurred thumb behind it as placeholder.
    const hero = screen.getByTestId('album-hero')
    expect(within(hero).getByRole('heading', { name: /Summer 2026/ })).toBeInTheDocument()
    const [placeholder, main] = Array.from(hero.querySelectorAll('img'))
    expect(placeholder).toHaveAttribute('src', '/api/albums/summer-2026/photos/p2/thumb')
    expect(main).toHaveAttribute('src', '/api/albums/summer-2026/photos/p2/medium')
    expect(main.getAttribute('srcset')).toContain('/api/albums/summer-2026/photos/p2/small 533w')
    expect(main).toHaveAttribute('sizes')

    const imgs = await screen.findAllByRole('img')
    const sunrise = imgs.find((img) => img.getAttribute('alt') === 'Sunrise')
    expect(sunrise).toBeDefined()
    expect(sunrise).toHaveAttribute('src', '/api/albums/summer-2026/photos/p1/medium')
    const srcset = sunrise!.getAttribute('srcset') ?? ''
    expect(srcset).toContain('/api/albums/summer-2026/photos/p1/thumb 400w')
    expect(srcset).toContain('/api/albums/summer-2026/photos/p1/small 800w')
    expect(srcset).toContain('/api/albums/summer-2026/photos/p1/medium 1600w')
    expect(srcset).toContain('/api/albums/summer-2026/photos/p1/large 2560w')

    // A 600x900 original is never upscaled: one candidate per real size only.
    const small = imgs.find((img) => img.getAttribute('alt') === 'p2.jpg')
    const smallSet = small!.getAttribute('srcset') ?? ''
    expect(smallSet).toContain('thumb 267w')
    expect(smallSet).toContain('small 533w')
    expect(smallSet).toContain('medium 600w')
    expect(smallSet).not.toContain('large')
  })

  it('keeps the lightbox in sync with ?photo= so the URL is shareable', async () => {
    const user = userEvent.setup()
    renderApp('/a/summer-2026?photo=p2')

    // Deep link opens the lightbox on that photo (the page behind it is inert).
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('2 / 2')).toBeInTheDocument()

    // Closing drops the parameter (replace, not push).
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByTestId('location')).toHaveTextContent('/a/summer-2026')

    // Clicking a photo pushes ?photo=<id> ...
    await user.click(screen.getByRole('img', { name: 'Sunrise' }))
    await screen.findByRole('dialog')
    expect(screen.getByTestId('location')).toHaveTextContent('/a/summer-2026?photo=p1')

    // ... and moving to the next slide follows along.
    await user.click(screen.getByRole('button', { name: 'Next' }))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/a/summer-2026?photo=p2'))
  })

  it('shares a link to the current photo, copying it when the share sheet is unavailable', async () => {
    const user = userEvent.setup()
    renderApp('/a/summer-2026?photo=p1')
    await screen.findByRole('dialog')

    await user.click(screen.getByRole('button', { name: 'Share' }))
    expect(await navigator.clipboard.readText()).toBe('http://localhost:3000/?photo=p1')
    expect(await screen.findByRole('button', { name: 'Link copied' })).toBeInTheDocument()
  })

  it('shows the password gate for a locked album and unlocks it', async () => {
    const user = userEvent.setup()
    let credentials: RequestCredentials | undefined
    const spy = ({ request }: { request: Request }) => {
      if (request.url.endsWith('/unlock')) credentials = request.credentials
    }
    server.events.on('request:start', spy)

    renderApp('/a/wedding')
    expect(await screen.findByRole('heading', { name: 'Wedding' })).toBeInTheDocument()
    expect(screen.getByText(/120 photos/)).toBeInTheDocument()
    expect(screen.getByText(/password protected/i)).toBeInTheDocument()

    const input = screen.getByLabelText('Password')
    const button = screen.getByRole('button', { name: /Unlock album/ })
    expect(button).toBeDisabled()

    await user.type(input, 'wrong')
    await user.click(button)
    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong password.')
    expect(credentials).toBe('include')

    await user.clear(input)
    await user.type(input, 'flood')
    await user.click(button)
    expect(await screen.findByRole('alert')).toHaveTextContent(/Too many attempts/)

    await user.clear(input)
    await user.type(input, 'correct')
    await user.click(button)
    // After a 204 the album is refetched and the gallery replaces the gate.
    expect(await screen.findByRole('link', { name: /Download album/ })).toHaveAttribute(
      'href',
      '/api/albums/wedding/download',
    )
    await waitFor(() => expect(screen.queryByLabelText('Password')).toBeNull())
    expect(screen.getByText('Protected')).toBeInTheDocument()
    server.events.removeListener('request:start', spy)
  })

  it('falls back to a plain header when the album has no cover', async () => {
    server.use(
      http.get('/api/albums/:slug', () =>
        HttpResponse.json({ ...emptyAlbum, download_url: '/api/albums/empty/download', photos: [] }),
      ),
    )
    renderApp('/a/empty')
    expect(await screen.findByRole('heading', { name: 'Empty' })).toBeInTheDocument()
    expect(screen.queryByTestId('album-hero')).toBeNull()
    expect(screen.getByText(/no photos yet/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Download album/ })).toBeNull()
  })

  it('shows a breadcrumb back to the containing folder', async () => {
    renderApp('/a/iceland')
    expect(await screen.findByRole('heading', { name: 'Iceland' })).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(nav).getByRole('link', { name: 'Travel' })).toHaveAttribute('href', '/f/travel')
    expect(within(nav).getByRole('link', { name: 'Gallery' })).toHaveAttribute('href', '/')
    // The current album ends the trail as plain text, not a link.
    expect(within(nav).getByText('Iceland')).toHaveAttribute('aria-current', 'page')
    expect(within(nav).queryByRole('link', { name: 'Iceland' })).toBeNull()
  })

  it('shows not found for unknown albums', async () => {
    renderApp('/a/nope')
    expect(await screen.findByText(/There is no album at this address/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to albums/ })).toHaveAttribute('href', '/')
  })

  it('renders the 404 page for unknown routes', async () => {
    renderApp('/some/where')
    expect(await screen.findByText(/This page does not exist/)).toBeInTheDocument()
  })
})
