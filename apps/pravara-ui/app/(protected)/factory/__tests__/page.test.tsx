import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import FactoryPage from '../page'

const list = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, machinesAPI: { ...actual.machinesAPI, list: (...a: unknown[]) => list(...a) } }
})

vi.mock('@/lib/auth', () => ({
  usePravaraSession: () => ({ data: { accessToken: 'token', user: {} }, status: 'authenticated' }),
}))

// The 3D view needs WebGL; it is not what this page test is about.
vi.mock('next/dynamic', () => ({
  default: () => () => <div data-testid="factory-3d" />,
}))

vi.mock('next/link', () => ({
  default: ({ href, children, ...props }: any) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

const machine = (id: string, name: string, status: string) => ({
  id, tenant_id: 't', name, code: id.toUpperCase(), type: 'printer', status,
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
})

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <FactoryPage />
    </QueryClientProvider>,
  )
}

/** The figure shown under a stat label. */
function stat(label: string) {
  const title = screen.getByText(label)
  const card = title.closest('.rounded-lg, [class*="card"]') ?? title.parentElement!.parentElement!
  return within(card as HTMLElement)
}

describe('FactoryPage', () => {
  beforeEach(() => {
    // Braces matter: a function returned from beforeEach is run as cleanup.
    list.mockReset()
  })

  it('shows zeros and no alerts for a factory with no machines', async () => {
    list.mockResolvedValue({ data: [], total: 0, limit: 200, offset: 0 })
    renderPage()
    expect(await screen.findByText(/No machines registered yet/)).toBeInTheDocument()
    expect(stat('Machines').getByText('0')).toBeInTheDocument()
    expect(stat('Running').getByText('0')).toBeInTheDocument()
    // Nothing invented: these had hard-coded figures with no source.
    for (const gone of ['Production', 'Efficiency', 'Uptime', '1847', '87.3%', '94.2%']) {
      expect(screen.queryByText(gone)).not.toBeInTheDocument()
    }
    expect(screen.queryByText(/CNC-003|Laser cutter|filament change/)).not.toBeInTheDocument()
  })

  it('counts machines by status and lists the ones needing attention', async () => {
    list.mockResolvedValue({
      data: [
        machine('m1', 'Prusa A', 'running'),
        machine('m2', 'Prusa B', 'idle'),
        machine('m3', 'Form 3', 'error'),
        machine('m4', 'Laser', 'maintenance'),
        machine('m5', 'Old CNC', 'offline'),
      ],
      total: 5, limit: 200, offset: 0,
    })
    renderPage()
    const link = await screen.findByRole('link', { name: /Form 3/ })
    expect(link).toHaveAttribute('href', '/machines/m3')
    expect(screen.getByRole('link', { name: /Laser/ })).toHaveAttribute('href', '/machines/m4')
    expect(screen.queryByRole('link', { name: /Prusa A/ })).not.toBeInTheDocument()
    expect(stat('Machines').getByText('5')).toBeInTheDocument()
    expect(stat('Running').getByText('1')).toBeInTheDocument()
    expect(stat('Offline').getByText('1')).toBeInTheDocument()
  })

  it('says when the status counts cover only part of the fleet', async () => {
    list.mockResolvedValue({ data: [machine('m1', 'Prusa A', 'running')], total: 240, limit: 200, offset: 0 })
    renderPage()
    expect(await screen.findByText('Status counts cover the first 1 of 240 machines.')).toBeInTheDocument()
    expect(stat('Machines').getByText('240')).toBeInTheDocument()
  })

  it('says the load failed instead of showing an empty factory', async () => {
    list.mockRejectedValue(new Error('HTTP error 503'))
    renderPage()
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.queryByText(/No machines registered yet/)).not.toBeInTheDocument()
    expect(screen.getByText(/Machines could not be loaded/)).toBeInTheDocument()
  })

  it('asks the API for one large page of machines', async () => {
    list.mockResolvedValue({ data: [], total: 0, limit: 200, offset: 0 })
    renderPage()
    await screen.findByText(/No machines registered yet/)
    const params = list.mock.calls[0][1] as URLSearchParams
    expect(params.get('limit')).toBe('200')
  })
})
