import { http, HttpResponse } from 'msw'
import {
  emptyAlbum,
  icelandDetail,
  secretAlbum,
  summerAlbum,
  summerDetail,
  travelFolder,
  travelFolderDetail,
  weddingDetail,
} from './fixtures'

/** Slugs the fake server considers unlocked for the current test. */
export const unlocked = new Set<string>()

export const handlers = [
  http.get('/api/site', () => HttpResponse.json({ title: 'Photo Gallery' })),

  http.get('/api/albums', () =>
    HttpResponse.json({ folders: [travelFolder], albums: [secretAlbum, summerAlbum, emptyAlbum] }),
  ),

  http.get('/api/folders/:slug', ({ params }) => {
    switch (params.slug) {
      case 'travel':
        return HttpResponse.json(travelFolderDetail)
      default:
        return HttpResponse.json({ error: 'folder_not_found' }, { status: 404 })
    }
  }),

  http.get('/api/albums/:slug', ({ params }) => {
    switch (params.slug) {
      case 'summer-2026':
        return HttpResponse.json(summerDetail)
      case 'iceland':
        return HttpResponse.json(icelandDetail)
      case 'wedding':
        if (unlocked.has('wedding')) return HttpResponse.json(weddingDetail)
        return HttpResponse.json(
          { error: 'password_required', slug: 'wedding', name: 'Wedding', photo_count: 120 },
          { status: 401 },
        )
      default:
        return HttpResponse.json({ error: 'album_not_found' }, { status: 404 })
    }
  }),

  http.post('/api/albums/:slug/unlock', async ({ params, request }) => {
    const { password } = (await request.json()) as { password: string }
    if (password === 'flood') return HttpResponse.json({ error: 'rate_limited' }, { status: 429 })
    if (password !== 'correct') return HttpResponse.json({ error: 'invalid_password' }, { status: 401 })
    unlocked.add(String(params.slug))
    return new HttpResponse(null, {
      status: 204,
      headers: { 'Set-Cookie': 'gallery_session=test; Path=/; HttpOnly' },
    })
  }),
]
