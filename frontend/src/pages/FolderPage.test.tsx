import { screen } from '@testing-library/react'
import { renderApp } from '@/test/render'

describe('FolderPage', () => {
  it('lists the folder’s albums and links into them', async () => {
    renderApp('/f/travel')
    expect(await screen.findByRole('heading', { name: 'Travel' })).toBeInTheDocument()
    const iceland = screen.getByRole('link', { name: 'Iceland' })
    expect(iceland).toHaveAttribute('href', '/a/iceland')
  })

  it('shows not found for unknown folders', async () => {
    renderApp('/f/nope')
    expect(await screen.findByText(/There is no folder at this address/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to albums/ })).toHaveAttribute('href', '/')
  })
})
