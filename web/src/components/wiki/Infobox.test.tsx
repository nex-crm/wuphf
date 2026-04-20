import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import Infobox from './Infobox'

describe('<Infobox>', () => {
  it('renders title, fields, and sections', () => {
    // Arrange
    const props = {
      title: 'Customer X',
      fields: [
        { dt: 'Stage', dd: 'Pilot' },
        { dt: 'Contract', dd: '$47k/yr' },
      ],
      sections: [
        {
          fields: [
            { dt: 'Account owner', dd: 'CEO' },
          ],
        },
      ],
    }
    // Act
    render(<Infobox {...props} />)
    // Assert
    expect(screen.getByText('Customer X')).toBeInTheDocument()
    expect(screen.getByText('Stage')).toBeInTheDocument()
    expect(screen.getByText('Pilot')).toBeInTheDocument()
    expect(screen.getByText('Contract')).toBeInTheDocument()
    expect(screen.getByText('$47k/yr')).toBeInTheDocument()
    expect(screen.getByText('Account owner')).toBeInTheDocument()
  })

  it('renders without sections', () => {
    render(
      <Infobox title="Minimal" fields={[{ dt: 'Key', dd: 'Value' }]} />,
    )
    expect(screen.getByText('Minimal')).toBeInTheDocument()
  })
})
