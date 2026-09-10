import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { server } from '@/test/server'
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
