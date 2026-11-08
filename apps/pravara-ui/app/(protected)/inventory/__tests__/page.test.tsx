import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/auth', () => ({
  usePravaraSession: () => ({
    data: { user: { name: 'Test User', accessToken: 'test-token' } },
    status: 'authenticated',
  }),
}))

const list = vi.fn()
const getLowStock = vi.fn()
vi.mock('@/lib/api', () => ({
  inventoryAPI: {
    list: (...a: unknown[]) => list(...a),
    getLowStock: (...a: unknown[]) => getLowStock(...a),
    adjust: vi.fn(),
  },
}))

import InventoryPage from '../page'

const base = {
  tenant_id: 't',
  quantity_reserved: 0,
  reorder_quantity: 50,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <InventoryPage />
    </QueryClientProvider>
  )
}

describe('InventoryPage', () => {
  beforeEach(() => {
    list.mockReset()
    getLowStock.mockReset()
  })

  it('says the load failed instead of showing an empty inventory', async () => {
    list.mockRejectedValue(new Error('HTTP error 503'))
    getLowStock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText("Couldn't load inventory")).toBeInTheDocument()
    expect(screen.queryByText('No inventory items yet')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('shows a unit cost in its recorded currency and flags a missing one', async () => {
    list.mockResolvedValue({
      data: [
        { ...base, id: 'a', sku: 'A', name: 'Resin', unit: 'ml', quantity_on_hand: 10, quantity_available: 10, reorder_point: 1, unit_cost: 250, currency: 'MXN' },
        { ...base, id: 'b', sku: 'B', name: 'Nozzle', unit: 'pcs', quantity_on_hand: 5, quantity_available: 5, reorder_point: 1, unit_cost: 12, currency: '' },
      ],
      total: 2,
      limit: 50,
      offset: 0,
    })
    getLowStock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('MX$250.00')).toBeInTheDocument()
    expect(screen.getByText('12.00')).toBeInTheDocument()
    expect(screen.getByText('currency not set')).toBeInTheDocument()
    expect(screen.queryByText('$12.00')).not.toBeInTheDocument()
  })

  it('has no dead "Add Item" button', async () => {
    list.mockResolvedValue({ data: [], total: 0, limit: 50, offset: 0 })
    getLowStock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No inventory items yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /add item/i })).not.toBeInTheDocument()
  })

  it('opens the receipt form on a low-stock card', async () => {
    const low = { ...base, id: 'c', sku: 'C', name: 'PETG', unit: 'g', quantity_on_hand: 2, quantity_available: 2, reorder_point: 10, currency: 'MXN' }
    list.mockResolvedValue({ data: [low], total: 1, limit: 50, offset: 0 })
    getLowStock.mockResolvedValue([low])
    renderPage()
    const tab = await screen.findByRole('tab', { name: /low stock/i })
    fireEvent.mouseDown(tab)
    fireEvent.click(tab)
    fireEvent.click(await screen.findByRole('button', { name: 'Record receipt for PETG' }))
    expect(screen.getByLabelText('Quantity received (g)')).toBeInTheDocument()
  })
})
