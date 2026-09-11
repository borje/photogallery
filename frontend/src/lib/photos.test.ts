import { photo } from '@/test/fixtures'
import { buildSrcSet, photoDetails, toSlide } from './photos'
import { formatDateRange, formatDateTime, parseTakenAt, photoCount } from './format'

describe('buildSrcSet', () => {
  it('lists every generated size for a large landscape original', () => {
    const set = buildSrcSet(photo('a', 'x', { width: 6000, height: 4000 }))
    expect(set.map((e) => [e.width, e.height])).toEqual([
      [400, 267],
      [800, 533],
      [1600, 1067],
      [2560, 1707],
    ])
    expect(set[3].src).toBe('/api/albums/a/photos/x/large')
  })

  it('never upscales and collapses equal sizes for portraits', () => {
    const set = buildSrcSet(photo('a', 'x', { width: 1000, height: 1500 }))
    expect(set.map((e) => [e.width, e.height])).toEqual([
      [267, 400],
      [533, 800],
      [1000, 1500],
    ])
    // The 1000px entry is the medium variant; large would be identical.
    expect(set[2].src).toContain('/medium')
  })
})

describe('toSlide', () => {
  it('points the download plugin at the original with the Lightroom filename', () => {
    const slide = toSlide(photo('a', 'x', { filename: 'IMG_0001.jpg' }))
    expect(slide.download).toEqual({ url: '/api/albums/a/photos/x/original?download=1', filename: 'IMG_0001.jpg' })
    expect(slide.src).toBe('/api/albums/a/photos/x/large')
  })
})

describe('formatting', () => {
  it('parses Lightroom wall-clock times as local time', () => {
    const d = parseTakenAt('2026-07-01T10:11:12')
    expect(d?.getHours()).toBe(10)
    expect(parseTakenAt('garbage')).toBeNull()
    expect(parseTakenAt(undefined)).toBeNull()
  })
  it('formats ranges and counts', () => {
    expect(formatDateRange('2026-07-01T10:00:00', '2026-07-01T18:00:00')).toBe(formatDateRange('2026-07-01T10:00:00'))
    expect(formatDateRange('2026-07-01T10:00:00', '2026-07-14T18:00:00')).toContain(' – ')
    expect(formatDateRange(undefined, undefined)).toBe('')
    expect(photoCount(1)).toBe('1 photo')
    expect(photoCount(12)).toBe('12 photos')
  })
})

describe('photoDetails', () => {
  it('lists the capture time, camera, lens and Lightroom filename in order, skipping unknown fields', () => {
    const rows = photoDetails(
      photo('a', 'x', {
        filename: 'IMG_0001.jpg',
        width: 6000,
        height: 4000,
        taken_at: '2026-07-01T10:11:00',
        exif: { make: 'Canon', model: 'EOS R5', lens: 'RF 50mm', focal_length: '50 mm', exposure: '1/250 sec at f/2.8', iso: 'ISO 100' },
      }),
    )
    expect(rows).toEqual([
      { label: 'Taken', value: formatDateTime('2026-07-01T10:11:00') },
      { label: 'Camera', value: 'Canon EOS R5' },
      { label: 'Lens', value: 'RF 50mm' },
      { label: 'Focal length', value: '50 mm' },
      { label: 'Exposure', value: '1/250 sec at f/2.8' },
      { label: 'ISO', value: 'ISO 100' },
      { label: 'Dimensions', value: '6000 × 4000' },
      { label: 'Filename', value: 'IMG_0001.jpg' },
    ])
    expect(formatDateTime('2026-07-01T10:11:00')).toMatch(/2026.*10:11/)
  })

  it('builds the exposure from shutter and aperture when Lightroom sent no combined value', () => {
    const rows = photoDetails(photo('a', 'x', { exif: { shutter_speed: '1/60 sec', aperture: 'f/4.0' } }))
    expect(rows).toContainEqual({ label: 'Exposure', value: '1/60 sec at f/4.0' })
    expect(rows.map((r) => r.label)).toEqual(['Exposure', 'Dimensions', 'Filename'])
  })
})
