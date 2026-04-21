import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ImageUploader from './ImageUploader'
import type { ImageUploadResult } from '../../api/images'

// Mock the api/images module so we exercise the component wiring without
// standing up a broker in-process. Each test overrides the mock as needed.
vi.mock('../../api/images', async () => {
  const actual = await vi.importActual<typeof import('../../api/images')>('../../api/images')
  return {
    ...actual,
    uploadImage: vi.fn(),
  }
})

import { uploadImage } from '../../api/images'

const uploadImageMock = vi.mocked(uploadImage)

function makeFile(name = 'photo.png', size = 1024): File {
  const blob = new Blob([new Uint8Array(size)], { type: 'image/png' })
  return new File([blob], name, { type: 'image/png' })
}

describe('<ImageUploader>', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the empty drop zone with format + size hint', () => {
    render(<ImageUploader onUploaded={vi.fn()} />)
    expect(screen.getByText(/Drop image or click to browse/i)).toBeInTheDocument()
    expect(screen.getByText(/PNG, JPEG, WebP, GIF, or SVG/i)).toBeInTheDocument()
  })

  it('shows an optimistic preview as soon as a file is selected', async () => {
    const result: ImageUploadResult = {
      asset_path: 'team/assets/202604/abcdef123456-photo.png',
      thumb_path: 'team/assets/202604/thumbs/abcdef123456.jpg',
      sha256: 'abcdef',
      size_bytes: 1024,
      format: 'png',
      width: 200,
      height: 150,
    }
    uploadImageMock.mockResolvedValue(result)

    const onUploaded = vi.fn()
    const user = userEvent.setup()
    render(<ImageUploader onUploaded={onUploaded} />)

    const input = screen.getByTestId('image-uploader-input') as HTMLInputElement
    await user.upload(input, makeFile('photo.png', 128))

    await waitFor(() => expect(onUploaded).toHaveBeenCalled())
    const [markdown, passedResult] = onUploaded.mock.calls[0]
    expect(markdown).toBe('![photo](team/assets/202604/abcdef123456-photo.png)')
    expect(passedResult).toEqual(result)
    expect(await screen.findByText(/Uploaded/)).toBeInTheDocument()
  })

  it('shows an error state when upload fails and surfaces the message', async () => {
    uploadImageMock.mockRejectedValue(new Error('queue saturated'))
    const onError = vi.fn()
    const user = userEvent.setup()
    render(<ImageUploader onUploaded={vi.fn()} onError={onError} />)

    const input = screen.getByTestId('image-uploader-input') as HTMLInputElement
    await user.upload(input, makeFile('big.png'))

    await waitFor(() => expect(onError).toHaveBeenCalledWith('queue saturated'))
    expect(await screen.findByRole('alert')).toHaveTextContent('queue saturated')
  })

  it('rejects files larger than maxMB before attempting upload', async () => {
    const onError = vi.fn()
    const user = userEvent.setup()
    render(<ImageUploader onUploaded={vi.fn()} onError={onError} maxMB={1} />)

    const huge = makeFile('huge.png', 2 * 1024 * 1024)
    const input = screen.getByTestId('image-uploader-input') as HTMLInputElement
    await user.upload(input, huge)

    expect(uploadImageMock).not.toHaveBeenCalled()
    expect(onError).toHaveBeenCalledWith(expect.stringMatching(/exceeds 1 MB/))
  })

  it('switches visual state on drag over + leave', () => {
    render(<ImageUploader onUploaded={vi.fn()} />)
    const root = screen.getByText(/Drop image or click to browse/i).closest('.image-uploader')
    expect(root).toBeInTheDocument()
    fireEvent.dragOver(root!)
    expect(root).toHaveClass('image-uploader--drag')
    fireEvent.dragLeave(root!)
    expect(root).not.toHaveClass('image-uploader--drag')
  })
})
