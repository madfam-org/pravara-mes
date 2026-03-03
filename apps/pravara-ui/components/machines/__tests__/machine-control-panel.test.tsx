import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MachineControlPanel } from '../machine-control-panel'
import type { Machine } from '@/lib/api'

// Mock next-auth
vi.mock('next-auth/react', () => ({
  useSession: () => ({
    data: { user: { accessToken: 'test-token' } },
    status: 'authenticated',
  }),
}))

// Mock tanstack query
const mockMutate = vi.fn()
vi.mock('@tanstack/react-query', () => ({
  useMutation: () => ({
    mutate: mockMutate,
    isPending: false,
  }),
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
  }),
}))

// Mock toast
vi.mock('@/lib/hooks/use-toast', () => ({
  useToast: () => ({
    toast: vi.fn(),
  }),
}))

// Mock machinesAPI
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    machinesAPI: {
      sendCommand: vi.fn(),
    },
  }
})

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

describe('MachineControlPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders "Machine Control" title', () => {
    render(<MachineControlPanel machine={makeMachine()} />)
    expect(screen.getByText('Machine Control')).toBeInTheDocument()
  })

  it('shows offline alert when machine is offline', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'offline' })} />)
    expect(screen.getByText('Machine is offline. Commands are unavailable.')).toBeInTheDocument()
  })

  it('does not show offline alert when machine is online', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'online' })} />)
    expect(screen.queryByText('Machine is offline. Commands are unavailable.')).not.toBeInTheDocument()
  })

  it('renders job control buttons (Start, Pause, Resume, Stop)', () => {
    render(<MachineControlPanel machine={makeMachine()} />)
    expect(screen.getByText('Start')).toBeInTheDocument()
    expect(screen.getByText('Pause')).toBeInTheDocument()
    expect(screen.getByText('Resume')).toBeInTheDocument()
    expect(screen.getByText('Stop')).toBeInTheDocument()
  })

  it('renders machine control buttons (Home, Calibrate)', () => {
    render(<MachineControlPanel machine={makeMachine()} />)
    expect(screen.getByText('Home')).toBeInTheDocument()
    expect(screen.getByText('Calibrate')).toBeInTheDocument()
  })

  it('renders Preheat and Cooldown for printer-type machines', () => {
    render(<MachineControlPanel machine={makeMachine({ type: '3D Printer' })} />)
    expect(screen.getByText('Preheat')).toBeInTheDocument()
    expect(screen.getByText('Cooldown')).toBeInTheDocument()
  })

  it('does not render Preheat and Cooldown for non-printer machines', () => {
    render(<MachineControlPanel machine={makeMachine({ type: 'CNC Mill' })} />)
    expect(screen.queryByText('Preheat')).not.toBeInTheDocument()
    expect(screen.queryByText('Cooldown')).not.toBeInTheDocument()
  })

  it('renders Emergency Stop button', () => {
    render(<MachineControlPanel machine={makeMachine()} />)
    expect(screen.getByText('Emergency Stop')).toBeInTheDocument()
  })

  it('disables Start when machine is running', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'running' })} />)
    const startButton = screen.getByText('Start').closest('button')
    expect(startButton).toBeDisabled()
  })

  it('disables Pause when machine is not running', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'online' })} />)
    const pauseButton = screen.getByText('Pause').closest('button')
    expect(pauseButton).toBeDisabled()
  })

  it('disables all commands when machine is offline', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'offline' })} />)
    const emergencyBtn = screen.getByText('Emergency Stop').closest('button')
    expect(emergencyBtn).toBeDisabled()
  })

  it('shows confirmation dialog when Emergency Stop is clicked', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'running' })} />)
    const emergencyBtn = screen.getByText('Emergency Stop').closest('button')!
    fireEvent.click(emergencyBtn)
    expect(screen.getByText('This will immediately halt all machine operations. Use only in emergencies.')).toBeInTheDocument()
  })

  it('shows confirmation dialog when Stop is clicked', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'running' })} />)
    const stopBtn = screen.getByText('Stop').closest('button')!
    fireEvent.click(stopBtn)
    expect(screen.getByText('This will stop the current job. The machine will need to be restarted.')).toBeInTheDocument()
  })

  it('cancel button in confirmation dialog closes it', () => {
    render(<MachineControlPanel machine={makeMachine({ status: 'running' })} />)
    const emergencyBtn = screen.getByText('Emergency Stop').closest('button')!
    fireEvent.click(emergencyBtn)
    const cancelBtn = screen.getByText('Cancel')
    fireEvent.click(cancelBtn)
    expect(screen.queryByText('This will immediately halt all machine operations. Use only in emergencies.')).not.toBeInTheDocument()
  })
})
