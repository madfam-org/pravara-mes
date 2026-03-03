import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MachineDialog } from '../machine-dialog'
import type { Machine } from '@/lib/api'

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

vi.mock('@/lib/mutations/use-machine-mutations', () => ({
  useCreateMachine: () => ({
    mutateAsync: mockCreateMutateAsync,
    isPending: false,
  }),
  useUpdateMachine: () => ({
    mutateAsync: mockUpdateMutateAsync,
    isPending: false,
  }),
}))

vi.mock('@/lib/validations/machine', () => ({
  createMachineSchema: { parse: (d: any) => d },
  updateMachineSchema: { parse: (d: any) => d },
}))

vi.mock('@hookform/resolvers/zod', () => ({
  zodResolver: () => async (values: any) => ({ values, errors: {} }),
}))

function makeMachine(overrides: Partial<Machine> = {}): Machine {
  return {
    id: 'machine-1',
    tenant_id: 'tenant-1',
    name: 'CNC Mill #1',
    code: 'CNC-001',
    type: '3D Printer',
    status: 'online',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('MachineDialog', () => {
  const onOpenChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders "Register Machine" title when no machine prop', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Register Machine')).toBeInTheDocument()
  })

  it('renders "Edit Machine" title when machine prop is provided', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} machine={makeMachine()} />)
    expect(screen.getByText('Edit Machine')).toBeInTheDocument()
  })

  it('renders form fields (Machine Name, Machine Code, Machine Type)', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Machine Name')).toBeInTheDocument()
    expect(screen.getByText('Machine Code')).toBeInTheDocument()
    expect(screen.getByText('Machine Type')).toBeInTheDocument()
  })

  it('renders optional fields (Location, MQTT Topic, Description)', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Location (Optional)')).toBeInTheDocument()
    expect(screen.getByText('MQTT Topic (Optional)')).toBeInTheDocument()
    expect(screen.getByText('Description (Optional)')).toBeInTheDocument()
  })

  it('renders Cancel and Register Machine buttons in create mode', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Cancel')).toBeInTheDocument()
    expect(screen.getByText('Register Machine')).toBeInTheDocument()
  })

  it('renders Update Machine button in edit mode', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} machine={makeMachine()} />)
    expect(screen.getByText('Update Machine')).toBeInTheDocument()
  })

  it('calls onOpenChange(false) when Cancel is clicked', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    fireEvent.click(screen.getByText('Cancel'))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('does not render when open is false', () => {
    render(<MachineDialog open={false} onOpenChange={onOpenChange} />)
    expect(screen.queryByText('Register Machine')).not.toBeInTheDocument()
  })

  it('shows Status field only in edit mode', () => {
    const { rerender } = render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.queryByText('Status')).not.toBeInTheDocument()

    rerender(<MachineDialog open={true} onOpenChange={onOpenChange} machine={makeMachine()} />)
    expect(screen.getByText('Status')).toBeInTheDocument()
  })

  it('shows create description text in create mode', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} />)
    expect(screen.getByText('Fill in the details to register a new machine.')).toBeInTheDocument()
  })

  it('shows edit description text in edit mode', () => {
    render(<MachineDialog open={true} onOpenChange={onOpenChange} machine={makeMachine()} />)
    expect(screen.getByText('Update the machine details below.')).toBeInTheDocument()
  })
})
