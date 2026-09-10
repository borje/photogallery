// Mirrors the JSON produced by backend/internal/api (albums.go).

export interface AlbumSummary {
  slug: string
  name: string
  description?: string
  locked: boolean
  photo_count: number
  taken_from?: string
  taken_to?: string
  cover_url?: string
}

export interface PhotoURLs {
  thumb: string
  small: string
  medium: string
  large: string
  original: string
  download: string
}

export interface Photo {
  id: string
  filename: string
  width: number
  height: number
  title?: string
  caption?: string
  keywords?: string[]
  taken_at?: string
  exif?: Record<string, string>
  urls: PhotoURLs
}

export interface AlbumDetail extends AlbumSummary {
  download_url: string
  photos: Photo[]
}

/** Body of the 401 returned for a locked album. */
export interface PasswordRequired {
  error: 'password_required'
  slug?: string
  name?: string
  photo_count?: number
}
