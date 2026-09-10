import { screen, within } from '@testing-library/react'
import { renderApp } from '@/test/render'

describe('FolderPage', () => {
  it('lists the folder’s albums and links into them', async () => {
    renderApp('/f/travel')
    expect(await screen.findByRole('heading', { name: 'Travel' })).toBeInTheDocument()
    const iceland = screen.getByRole('link', { name: 'Iceland' })
    expect(iceland).toHaveAttribute('href', '/a/iceland')
  })

  it('shows the full path in the breadcrumb, even for a root-level folder', async () => {
    renderApp('/f/travel')
    await screen.findByRole('heading', { name: 'Travel' })
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
