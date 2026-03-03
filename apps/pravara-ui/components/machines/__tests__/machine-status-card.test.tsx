import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MachineStatusCard } from '../machine-status-card'
import type { Machine } from '@/lib/api'

// Mock telemetry hook
vi.mock('@/hooks/useTelemetryUpdates', () => ({
  useTelemetryUpdates: () => ({
    getLatestMetric: vi.fn().mockReturnValue(null),
  }),
}))

// Mock telemetry sub-components
vi.mock('@/components/machines/telemetry', () => ({
  ProgressRing: ({ label }: any) => <div data-testid="progress-ring">{label}</div>,
  TemperatureGauge: ({ label }: any) => <div data-testid="temp-gauge">{label}</div>,
  MetricSparkline: () => <div data-testid="sparkline" />,
}))

// Mock utils
vi.mock('@/lib/utils', () => ({
  formatRelativeTime: (date: string) => `mocked-${date}`,
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
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

describe('MachineStatusCard', () => {
  it('renders the card with "Status" title', () => {
    render(<MachineStatusCard machine={makeMachine()} />)
    expect(screen.getByText('Status')).toBeInTheDocument()
  })

  it('displays "Online" badge for online machine', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'online' })} />)
    expect(screen.getByText('Online')).toBeInTheDocument()
  })

  it('displays "Offline" badge for offline machine', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'offline' })} />)
    expect(screen.getByText('Offline')).toBeInTheDocument()
  })

  it('displays "Error" badge for error machine', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'error' })} />)
    expect(screen.getByText('Error')).toBeInTheDocument()
  })

  it('displays "Running" badge and "Active" for running machine', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'running' })} />)
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
  })

  it('does not show "Active" badge for non-running machines', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'idle' })} />)
    expect(screen.queryByText('Active')).not.toBeInTheDocument()
  })

  it('displays the machine type', () => {
    render(<MachineStatusCard machine={makeMachine({ type: 'CNC Lathe' })} />)
    expect(screen.getByText('CNC Lathe')).toBeInTheDocument()
  })

  it('displays location when provided', () => {
    render(<MachineStatusCard machine={makeMachine({ location: 'Building A' })} />)
    expect(screen.getByText('Building A')).toBeInTheDocument()
  })

  it('does not display location when not provided', () => {
    render(<MachineStatusCard machine={makeMachine({ location: undefined })} />)
    expect(screen.queryByText('Location')).not.toBeInTheDocument()
  })

  it('displays "Never" when last_heartbeat is null', () => {
    render(<MachineStatusCard machine={makeMachine({ last_heartbeat: undefined })} />)
    expect(screen.getByText('Never')).toBeInTheDocument()
  })

  it('displays formatted relative time for last_heartbeat', () => {
    render(<MachineStatusCard machine={makeMachine({ last_heartbeat: '2025-01-01T00:00:00Z' })} />)
    expect(screen.getByText('mocked-2025-01-01T00:00:00Z')).toBeInTheDocument()
  })

  it('displays MQTT topic when provided', () => {
    render(<MachineStatusCard machine={makeMachine({ mqtt_topic: 'machines/cnc-001' })} />)
    expect(screen.getByText('machines/cnc-001')).toBeInTheDocument()
  })

  it('does not show MQTT topic section when not provided', () => {
    render(<MachineStatusCard machine={makeMachine({ mqtt_topic: undefined })} />)
    expect(screen.queryByText('MQTT Topic')).not.toBeInTheDocument()
  })

  it('displays "Maintenance" badge for maintenance status', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'maintenance' })} />)
    expect(screen.getByText('Maintenance')).toBeInTheDocument()
  })

  it('displays "Idle" badge for idle status', () => {
    render(<MachineStatusCard machine={makeMachine({ status: 'idle' })} />)
    expect(screen.getByText('Idle')).toBeInTheDocument()
  })
})
