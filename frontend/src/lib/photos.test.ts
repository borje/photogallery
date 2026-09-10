import { photo } from '@/test/fixtures'
import { buildSrcSet, exifSummary, toSlide } from './photos'
import { formatDateRange, parseTakenAt, photoCount } from './format'

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
  it('summarises exif', () => {
    expect(exifSummary({ make: 'Canon', model: 'EOS R5', exposure: '1/250 sec at f/2.8', iso: 'ISO 100' })).toBe(
      'Canon EOS R5 · 1/250 sec at f/2.8 · ISO 100',
    )
    expect(exifSummary(undefined)).toBe('')
  })
})
