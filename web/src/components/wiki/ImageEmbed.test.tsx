import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ImageEmbed from './ImageEmbed'

vi.mock('../../api/images', async () => {
  const actual = await vi.importActual<typeof import('../../api/images')>('../../api/images')
  return {
    ...actual,
    fetchAlt: vi.fn(async () => 'A test image of a diagram'),
    assetURL: (p: string) => `/api/wiki/assets/${p.replace(/^team\/assets\//, '')}`,
  }
})

describe('<ImageEmbed>', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders an img with alt-text fetched from the sidecar', async () => {
    render(<ImageEmbed assetPath="team/assets/202604/abc-photo.png" width={400} height={300} />)
    const img = await screen.findByAltText(/A test image of a diagram/)
    expect(img).toBeInTheDocument()
    expect(img).toHaveAttribute('loading', 'lazy')
    expect(img).toHaveAttribute('width', '400')
    expect(img).toHaveAttribute('height', '300')
  })

  it('prefers the thumbnail for the inline render and the full asset for the lightbox', async () => {
    render(
      <ImageEmbed
        assetPath="team/assets/202604/abc-photo.png"
        thumbPath="team/assets/202604/thumbs/abc.jpg"
        alt="An explicitly-provided alt"
      />,
    )
    const user = userEvent.setup()
    const thumbImg = await screen.findByAltText('An explicitly-provided alt')
    expect(thumbImg.getAttribute('src')).toContain('thumbs/abc.jpg')

    // Opening the lightbox swaps to the full-size asset.
    const trigger = screen.getByRole('button', { name: /view full-size/i })
    await user.click(trigger)

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeInTheDocument()
    const fullImg = dialog.querySelector('img.image-embed__full') as HTMLImageElement
    expect(fullImg.getAttribute('src')).toContain('abc-photo.png')
    expect(fullImg.getAttribute('src')).not.toContain('thumbs')
  })

  it('closes the lightbox on Escape', async () => {
    render(
      <ImageEmbed
        assetPath="team/assets/202604/abc-photo.png"
        alt="An image"
      />,
    )
    const user = userEvent.setup()
    const trigger = await screen.findByRole('button', { name: /view full-size/i })
    await user.click(trigger)
    expect(await screen.findByRole('dialog')).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('renders without the lightbox trigger when editorial={false}', () => {
    render(
      <ImageEmbed
        assetPath="team/assets/202604/abc-photo.png"
        alt="Non-editorial"
        editorial={false}
      />,
    )
    expect(screen.queryByRole('button', { name: /view full-size/i })).not.toBeInTheDocument()
    expect(screen.getByAltText('Non-editorial')).toBeInTheDocument()
  })
})
