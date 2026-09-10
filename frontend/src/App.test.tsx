import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { renderApp } from '@/test/render'

describe('App', () => {
  it('uses the configured site title in the header and document title', async () => {
    server.use(http.get('/api/site', () => HttpResponse.json({ title: 'The Granberg Archive' })))
    renderApp('/')
    expect(await screen.findByRole('link', { name: /The Granberg Archive/ })).toBeInTheDocument()
    expect(document.title).toBe('The Granberg Archive')
  })

  it('falls back to "Photo Gallery" when the site config request fails', async () => {
    server.use(http.get('/api/site', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderApp('/')
    expect(await screen.findByRole('link', { name: /Photo Gallery/ })).toBeInTheDocument()
  })
})
