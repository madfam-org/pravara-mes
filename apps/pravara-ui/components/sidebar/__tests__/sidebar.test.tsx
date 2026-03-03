import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Sidebar } from '../../sidebar'

// Mock next/navigation
const mockPathname = vi.fn().mockReturnValue('/dashboard')
vi.mock('next/navigation', () => ({
  usePathname: () => mockPathname(),
}))

// Mock next-auth
vi.mock('next-auth/react', () => ({
  signOut: vi.fn(),
}))

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ href, children, ...props }: any) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

vi.mock('@/lib/utils', () => ({
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
}))

describe('Sidebar', () => {
  beforeEach(() => {
    mockPathname.mockReturnValue('/dashboard')
  })

  it('renders PravaraMES branding', () => {
    render(<Sidebar />)
    expect(screen.getByText('PravaraMES')).toBeInTheDocument()
  })

  it('renders all navigation items', () => {
    render(<Sidebar />)
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('Factory Floor')).toBeInTheDocument()
    expect(screen.getByText('G-Code Viewer')).toBeInTheDocument()
    expect(screen.getByText('Kanban Board')).toBeInTheDocument()
    expect(screen.getByText('Orders')).toBeInTheDocument()
    expect(screen.getByText('Machines')).toBeInTheDocument()
  })

  it('renders Settings link', () => {
    render(<Sidebar />)
    expect(screen.getByText('Settings')).toBeInTheDocument()
  })

  it('renders Sign out button', () => {
    render(<Sidebar />)
    expect(screen.getByLabelText('Sign out of your account')).toBeInTheDocument()
  })

  it('marks current page link as active via aria-current', () => {
    mockPathname.mockReturnValue('/dashboard')
    render(<Sidebar />)
    const dashboardLink = screen.getByText('Dashboard').closest('a')
    expect(dashboardLink).toHaveAttribute('aria-current', 'page')
  })

  it('does not mark non-active pages with aria-current', () => {
    mockPathname.mockReturnValue('/dashboard')
    render(<Sidebar />)
    const ordersLink = screen.getByText('Orders').closest('a')
    expect(ordersLink).not.toHaveAttribute('aria-current')
  })

  it('collapses sidebar when collapse button is clicked', () => {
    render(<Sidebar />)
    const collapseButton = screen.getByLabelText('Collapse sidebar')
    fireEvent.click(collapseButton)
    // After collapse, the branding text should be hidden
    expect(screen.queryByText('PravaraMES')).not.toBeInTheDocument()
    // Expand button should now appear
    expect(screen.getByLabelText('Expand sidebar')).toBeInTheDocument()
  })

  it('expands sidebar when expand button is clicked', () => {
    render(<Sidebar />)
    const collapseButton = screen.getByLabelText('Collapse sidebar')
    fireEvent.click(collapseButton)
    const expandButton = screen.getByLabelText('Expand sidebar')
    fireEvent.click(expandButton)
    expect(screen.getByText('PravaraMES')).toBeInTheDocument()
  })

  it('displays user information when user prop is provided and not collapsed', () => {
    render(<Sidebar user={{ name: 'John Doe', email: 'john@example.com' }} />)
    expect(screen.getByText('John Doe')).toBeInTheDocument()
    expect(screen.getByText('john@example.com')).toBeInTheDocument()
  })

  it('does not display user info when collapsed', () => {
    render(<Sidebar user={{ name: 'John Doe', email: 'john@example.com' }} />)
    const collapseButton = screen.getByLabelText('Collapse sidebar')
    fireEvent.click(collapseButton)
    expect(screen.queryByText('John Doe')).not.toBeInTheDocument()
  })

  it('has proper aria-label on navigation', () => {
    render(<Sidebar />)
    expect(screen.getByRole('navigation', { name: 'Main navigation' })).toBeInTheDocument()
  })

  it('navigation links have correct href values', () => {
    render(<Sidebar />)
    expect(screen.getByText('Dashboard').closest('a')).toHaveAttribute('href', '/dashboard')
    expect(screen.getByText('Orders').closest('a')).toHaveAttribute('href', '/orders')
    expect(screen.getByText('Machines').closest('a')).toHaveAttribute('href', '/machines')
  })
})
