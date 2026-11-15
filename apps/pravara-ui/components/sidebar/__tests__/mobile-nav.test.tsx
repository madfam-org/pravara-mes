import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MobileNav } from '../../mobile-nav'

vi.mock('next/navigation', () => ({
  usePathname: () => '/dashboard',
}))

vi.mock('@/lib/auth', () => ({
  usePravaraSignOut: () => vi.fn(),
}))

vi.mock('next/link', () => ({
  default: ({ href, children, ...props }: any) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

describe('MobileNav', () => {
  it('keeps the drawer closed until the menu button is pressed', () => {
    render(<MobileNav />)
    expect(screen.getByLabelText('Open navigation')).toBeInTheDocument()
    expect(screen.queryByText('Orders')).not.toBeInTheDocument()
  })

  it('opens the navigation drawer without a collapse control', () => {
    render(<MobileNav user={{ name: 'Ana', email: 'ana@example.com' }} />)
    fireEvent.click(screen.getByLabelText('Open navigation'))
    expect(screen.getByRole('dialog', { name: 'Navigation' })).toBeInTheDocument()
    expect(screen.getByText('Orders')).toBeInTheDocument()
    expect(screen.queryByLabelText('Collapse sidebar')).not.toBeInTheDocument()
  })

  it('closes the drawer when a destination is chosen', () => {
    render(<MobileNav />)
    fireEvent.click(screen.getByLabelText('Open navigation'))
    fireEvent.click(screen.getByText('Orders'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('closes the drawer from its close button', () => {
    render(<MobileNav />)
    fireEvent.click(screen.getByLabelText('Open navigation'))
    fireEvent.click(screen.getByLabelText('Close navigation'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
