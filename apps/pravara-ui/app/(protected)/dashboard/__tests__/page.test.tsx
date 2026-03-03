import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import DashboardPage from '../page'

// Mock next-auth
vi.mock('next-auth/react', () => ({
  useSession: () => ({
    data: {
      user: {
        name: 'Test User',
        accessToken: 'test-token',
      },
    },
    status: 'authenticated',
  }),
}))

// Mock tanstack query
vi.mock('@tanstack/react-query', () => ({
  useQuery: ({ queryKey }: any) => {
    if (queryKey[0] === 'orders') {
      return {
        data: {
          data: [
            { id: 'o1', status: 'received', customer_name: 'Customer A' },
            { id: 'o2', status: 'delivered', customer_name: 'Customer B' },
          ],
          total: 2,
          limit: 50,
          offset: 0,
        },
      }
    }
    if (queryKey[0] === 'machines') {
      return {
        data: {
          data: [
            { id: 'm1', name: 'CNC Mill', code: 'CNC-001', status: 'online' },
            { id: 'm2', name: 'Laser', code: 'LASER-001', status: 'offline' },
          ],
          total: 2,
          limit: 50,
          offset: 0,
        },
      }
    }
    if (queryKey[0] === 'kanban-board') {
      return {
        data: {
          columns: {
            backlog: [{ id: 't1' }],
            queued: [],
            in_progress: [{ id: 't2' }, { id: 't3' }],
            quality_check: [],
            completed: [{ id: 't4' }],
            blocked: [],
          },
        },
      }
    }
    return { data: null }
  },
}))

describe('DashboardPage', () => {
  it('renders "Dashboard" heading', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('displays welcome message with user name', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Welcome back, Test User')).toBeInTheDocument()
  })

  it('displays Active Orders stat card', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Active Orders')).toBeInTheDocument()
    // 1 active (received), 1 delivered (not active)
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  it('displays Online Machines stat card', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Online Machines')).toBeInTheDocument()
    // 1 online out of 2
    expect(screen.getByText('1/2')).toBeInTheDocument()
  })

  it('displays Tasks In Progress stat card', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Tasks In Progress')).toBeInTheDocument()
    // 2 in_progress tasks
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('displays Completed Today stat card', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Completed Today')).toBeInTheDocument()
  })

  it('renders Task Overview section', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Task Overview')).toBeInTheDocument()
    expect(screen.getByText('Backlog')).toBeInTheDocument()
    expect(screen.getByText('Queued')).toBeInTheDocument()
    expect(screen.getByText('In Progress')).toBeInTheDocument()
    expect(screen.getByText('Quality Check')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
  })

  it('renders Machine Status section', () => {
    render(<DashboardPage />)
    expect(screen.getByText('Machine Status')).toBeInTheDocument()
  })

  it('displays machine names in the status section', () => {
    render(<DashboardPage />)
    expect(screen.getByText('CNC Mill')).toBeInTheDocument()
    expect(screen.getByText('Laser')).toBeInTheDocument()
  })

  it('displays machine codes in the status section', () => {
    render(<DashboardPage />)
    expect(screen.getByText('CNC-001')).toBeInTheDocument()
    expect(screen.getByText('LASER-001')).toBeInTheDocument()
  })

  it('does not render Blocked section when no blocked tasks', () => {
    render(<DashboardPage />)
    // The "Blocked" text in the conditionally rendered section should not appear
    // (the "blocked" column in our mock data is empty)
    const blockedElements = screen.queryAllByText('Blocked')
    // There should be no blocked section with a count
    expect(blockedElements.length).toBe(0)
  })
})
