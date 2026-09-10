import type { AlbumDetail, AlbumSummary, Photo } from '@/api/types'

export function photo(slug: string, id: string, overrides: Partial<Photo> = {}): Photo {
  const base = `/api/albums/${slug}/photos/${id}/`
  return {
    id,
    filename: `${id}.jpg`,
    width: 3000,
    height: 2000,
    urls: {
      thumb: base + 'thumb',
      small: base + 'small',
      medium: base + 'medium',
      large: base + 'large',
      original: base + 'original',
      download: base + 'original?download=1',
    },
    ...overrides,
  }
}

export const summerAlbum: AlbumSummary = {
  slug: 'summer-2026',
  name: 'Summer 2026',
  locked: false,
  photo_count: 2,
  taken_from: '2026-07-01T10:00:00',
  taken_to: '2026-07-14T18:30:00',
  cover_url: '/api/albums/summer-2026/cover',
}

export const secretAlbum: AlbumSummary = {
  slug: 'wedding',
  name: 'Wedding',
  locked: true,
  photo_count: 120,
  taken_from: '2026-06-06T12:00:00',
  taken_to: '2026-06-06T23:00:00',
  cover_url: '/api/albums/wedding/cover',
}

export const emptyAlbum: AlbumSummary = {
  slug: 'empty',
  name: 'Empty',
  locked: false,
  photo_count: 0,
}

export const summerDetail: AlbumDetail = {
  ...summerAlbum,
  description: 'Two weeks by the sea.',
  download_url: '/api/albums/summer-2026/download',
  photos: [
    photo('summer-2026', 'p1', { title: 'Sunrise', caption: 'First morning', taken_at: '2026-07-01T10:00:00', exif: { make: 'Canon', model: 'EOS R5', exposure: '1/250 sec at f/2.8', iso: 'ISO 100' } }),
    photo('summer-2026', 'p2', { width: 600, height: 900 }),
  ],
}

export const weddingDetail: AlbumDetail = {
  ...secretAlbum,
  download_url: '/api/albums/wedding/download',
  photos: [photo('wedding', 'w1')],
}
