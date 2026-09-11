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

/** One entry in a breadcrumb trail, root first. */
export interface Crumb {
  slug: string
  name: string
}

export interface AlbumDetail extends AlbumSummary {
  breadcrumb?: Crumb[]
  /** Id of the photo shown as cover; one of `photos`. Absent for an empty album. */
  cover_photo_id?: string
  download_url: string
  photos: Photo[]
}

/** Body of the 401 returned for a locked album. */
export interface PasswordRequired {
  error: 'password_required'
  slug?: string
  name?: string
  photo_count?: number
  breadcrumb?: Crumb[]
}

/** A folder card: cover image and name only, no stats. */
export interface FolderSummary {
  slug: string
  name: string
  cover_url?: string
}

/** The root of the tree: folders and albums with no parent. */
export interface RootListing {
  folders: FolderSummary[]
  albums: AlbumSummary[]
}

export interface FolderDetail {
  slug: string
  name: string
  breadcrumb?: Crumb[]
  folders: FolderSummary[]
  albums: AlbumSummary[]
}

export interface SiteConfig {
  title: string
}
