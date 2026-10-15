import { render, screen } from '@testing-library/react'
import { Lamp, StateBadge } from './ui'

test('a state is written as the only text of its badge', () => {
  render(<StateBadge state="error" />)

  expect(screen.getByText('ошибка')).toHaveTextContent(/^ошибка$/)
})

test('a lamp is hidden from assistive technologies', () => {
  const { container } = render(<Lamp tone="err" title="bitbucket: ошибка" />)

  const lamp = container.querySelector('.lamp')!
  expect(lamp).toHaveClass('lamp-err')
  expect(lamp).toHaveAttribute('aria-hidden', 'true')
  expect(lamp).toBeEmptyDOMElement()
})
