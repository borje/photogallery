import { screen, within } from '@testing-library/react'
import { renderApp } from '@/test/render'

describe('FolderPage', () => {
  it('lists the folder’s albums and links into them', async () => {
    renderApp('/f/travel')
    const iceland = await screen.findByRole('link', { name: 'Iceland' })
    expect(iceland).toHaveAttribute('href', '/a/iceland')
  })

  it('shows the full path in the breadcrumb, even for a root-level folder, with no separate heading', async () => {
    renderApp('/f/travel')
    await screen.findByRole('link', { name: 'Iceland' })
    expect(screen.queryByRole('heading', { level: 1 })).toBeNull()
    const nav = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(nav).getByRole('link', { name: 'Gallery' })).toHaveAttribute('href', '/')
    expect(within(nav).getByText('Travel')).toHaveAttribute('aria-current', 'page')
    expect(within(nav).queryByRole('link', { name: 'Travel' })).toBeNull()
  })

  it('shows not found for unknown folders', async () => {
    renderApp('/f/nope')
    expect(await screen.findByText(/There is no folder at this address/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to albums/ })).toHaveAttribute('href', '/')
  })
})
