import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { RealtimeLineChart } from '../realtime-line-chart'

// Mock recharts - these are complex SVG components, so we stub them
vi.mock('recharts', () => ({
  LineChart: ({ children }: any) => <div data-testid="line-chart">{children}</div>,
  Line: ({ dataKey }: any) => <div data-testid={`line-${dataKey}`} />,
  XAxis: () => <div data-testid="x-axis" />,
  YAxis: () => <div data-testid="y-axis" />,
  CartesianGrid: () => <div data-testid="cartesian-grid" />,
  Tooltip: () => <div data-testid="tooltip" />,
  Legend: () => <div data-testid="legend" />,
  ResponsiveContainer: ({ children }: any) => <div data-testid="responsive-container">{children}</div>,
}))

vi.mock('@/lib/utils', () => ({
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
}))

const mockGetMetrics = vi.fn()
vi.mock('@/hooks/useTelemetryUpdates', () => ({
  useTelemetryUpdates: () => ({
    getMetrics: mockGetMetrics,
  }),
}))

describe('RealtimeLineChart', () => {
  it('renders "Waiting for telemetry data..." when no data is available', () => {
    mockGetMetrics.mockReturnValue([])
    render(
      <RealtimeLineChart machineId="machine-1" metricTypes={['hotend_temp']} />
    )
    expect(screen.getByText('Waiting for telemetry data...')).toBeInTheDocument()
  })

  it('renders the chart when data is available', () => {
    const now = Date.now()
    mockGetMetrics.mockReturnValue([
      { type: 'hotend_temp', value: 200, timestamp: new Date(now - 1000).toISOString() },
      { type: 'hotend_temp', value: 210, timestamp: new Date(now - 500).toISOString() },
    ])
    render(
      <RealtimeLineChart machineId="machine-1" metricTypes={['hotend_temp']} />
    )
    expect(screen.getByTestId('responsive-container')).toBeInTheDocument()
    expect(screen.getByTestId('line-chart')).toBeInTheDocument()
  })

  it('renders a Line for each metric type when data exists', () => {
    const now = Date.now()
    mockGetMetrics.mockReturnValue([
      { type: 'hotend_temp', value: 200, timestamp: new Date(now - 1000).toISOString() },
      { type: 'bed_temp', value: 60, timestamp: new Date(now - 1000).toISOString() },
    ])
    render(
      <RealtimeLineChart machineId="machine-1" metricTypes={['hotend_temp', 'bed_temp']} />
    )
    expect(screen.getByTestId('line-hotend_temp')).toBeInTheDocument()
    expect(screen.getByTestId('line-bed_temp')).toBeInTheDocument()
  })

  it('filters out metrics outside the time window', () => {
    const now = Date.now()
    mockGetMetrics.mockReturnValue([
      { type: 'hotend_temp', value: 200, timestamp: new Date(now - 10 * 60 * 1000).toISOString() },
    ])
    // Default time window is 5 minutes
    render(
      <RealtimeLineChart machineId="machine-1" metricTypes={['hotend_temp']} />
    )
    // Old data should be filtered, so empty state should appear
    expect(screen.getByText('Waiting for telemetry data...')).toBeInTheDocument()
  })

  it('applies custom className', () => {
    mockGetMetrics.mockReturnValue([])
    const { container } = render(
      <RealtimeLineChart
        machineId="machine-1"
        metricTypes={['hotend_temp']}
        className="custom-class"
      />
    )
    expect(container.firstChild).toHaveClass('custom-class')
  })

  it('uses custom height', () => {
    mockGetMetrics.mockReturnValue([])
    const { container } = render(
      <RealtimeLineChart
        machineId="machine-1"
        metricTypes={['hotend_temp']}
        height={400}
      />
    )
    expect(container.firstChild).toHaveStyle({ height: '400px' })
  })

  it('ignores metric types not in the provided list', () => {
    const now = Date.now()
    mockGetMetrics.mockReturnValue([
      { type: 'cpu_usage', value: 50, timestamp: new Date(now - 1000).toISOString() },
    ])
    render(
      <RealtimeLineChart machineId="machine-1" metricTypes={['hotend_temp']} />
    )
    // cpu_usage is not in metricTypes, so data filtered to empty
    expect(screen.getByText('Waiting for telemetry data...')).toBeInTheDocument()
  })
})
