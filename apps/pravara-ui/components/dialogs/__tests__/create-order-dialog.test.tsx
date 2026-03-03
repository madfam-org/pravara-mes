import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OrderDialog } from '../order-dialog'
import type { Order } from '@/lib/api'

// Mock next-auth
vi.mock('next-auth/react', () => ({
  useSession: () => ({
    data: { user: { accessToken: 'test-token' } },
    status: 'authenticated',
  }),
}))

// Mock mutation hooks
const mockCreateMutateAsync = vi.fn().mockResolvedValue({})
const mockUpdateMutateAsync = vi.fn().mockResolvedValue({})

vi.mock('@/lib/mutations/use-order-mutations', () => ({
  useCreateOrder: () => ({
    mutateAsync: mockCreateMutateAsync,
    isPending: false,
  }),
  useUpdateOrder: () => ({
    mutateAsync: mockUpdateMutateAsync,
    isPending: false,
  }),
}))

// Mock validations - pass through as basic objects
vi.mock('@/lib/validations/order', () => ({
  createOrderSchema: { parse: (d: any) => d },
  updateOrderSchema: { parse: (d: any) => d },
}))

// Mock hookform resolver to avoid real zod validation for unit tests
vi.mock('@hookform/resolvers/zod', () => ({
  zodResolver: () => async (values: any) => ({ values, errors: {} }),
}))

describe('OrderDialog', () => {
  const onOpenChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders "Create Order" title when no order prop', () => {
    render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Create Order')).toBeInTheDocument()
  })

  it('renders "Edit Order" title when order prop is provided', () => {
    const order: Order = {
      id: 'order-1',
      tenant_id: 'tenant-1',
      customer_name: 'Acme Corp',
      status: 'received',
      priority: 5,
      currency: 'MXN',
      total_amount: 100,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }
    render(<OrderDialog open={true} onOpenChange={onOpenChange} order={order} />)
    expect(screen.getByText('Edit Order')).toBeInTheDocument()
  })

  it('renders the form fields', () => {
    render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Customer Name')).toBeInTheDocument()
    expect(screen.getByText('Priority')).toBeInTheDocument()
    expect(screen.getByText('Currency')).toBeInTheDocument()
  })

  it('renders Cancel and Create Order buttons', () => {
    render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Cancel')).toBeInTheDocument()
    expect(screen.getByText('Create Order')).toBeInTheDocument()
  })

  it('renders Update Order button in edit mode', () => {
    const order: Order = {
      id: 'order-1',
      tenant_id: 'tenant-1',
      customer_name: 'Acme Corp',
      status: 'received',
      priority: 5,
      currency: 'MXN',
      total_amount: 100,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }
    render(<OrderDialog open={true} onOpenChange={onOpenChange} order={order} />)
    expect(screen.getByText('Update Order')).toBeInTheDocument()
  })

  it('calls onOpenChange(false) when Cancel is clicked', () => {
    render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.click(screen.getByText('Cancel'))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('does not render when open is false', () => {
    render(<OrderDialog open={false} onOpenChange={onOpenChange} />)
    expect(screen.queryByText('Create Order')).not.toBeInTheDocument()
  })

  it('shows description text for create mode', () => {
    render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Fill in the details to create a new order.')).toBeInTheDocument()
  })

  it('shows description text for edit mode', () => {
    const order: Order = {
      id: 'order-1',
      tenant_id: 'tenant-1',
      customer_name: 'Acme Corp',
      status: 'received',
      priority: 5,
      currency: 'MXN',
      total_amount: 100,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }
    render(<OrderDialog open={true} onOpenChange={onOpenChange} order={order} />)
    expect(screen.getByText('Update the order details below.')).toBeInTheDocument()
  })

  it('shows Status field only in edit mode', () => {
    const order: Order = {
      id: 'order-1',
      tenant_id: 'tenant-1',
      customer_name: 'Acme Corp',
      status: 'received',
      priority: 5,
      currency: 'MXN',
      total_amount: 100,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }
    const { rerender } = render(<OrderDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.queryByText('Status')).not.toBeInTheDocument()

    rerender(<OrderDialog open={true} onOpenChange={onOpenChange} order={order} />)
    expect(screen.getByText('Status')).toBeInTheDocument()
  })
})
