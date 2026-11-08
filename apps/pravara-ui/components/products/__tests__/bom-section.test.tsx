import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ProductDefinition } from '@/lib/api'

const getBOM = vi.fn()
const deleteBOMItem = vi.fn()
vi.mock('@/lib/api', () => ({
  productsAPI: {
    getBOM: (...a: unknown[]) => getBOM(...a),
    addBOMItem: vi.fn(),
    deleteBOMItem: (...a: unknown[]) => deleteBOMItem(...a),
  },
}))

import { BOMSection } from '../bom-section'

const product = { id: 'p1', name: 'Bracket', sku: 'BRK-1' } as ProductDefinition
const row = {
  id: 'b1',
  tenant_id: 't',
  product_definition_id: 'p1',
  material_name: 'PLA Filament',
  quantity: 120,
  unit: 'g',
  estimated_cost: 48,
  currency: 'MXN',
  sort_order: 0,
  created_at: '',
  updated_at: '',
}

function renderSection() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <BOMSection product={product} token="t" />
    </QueryClientProvider>
  )
}

describe('BOMSection', () => {
  beforeEach(() => {
    getBOM.mockReset()
    deleteBOMItem.mockReset()
  })

  it('does not claim "no BOM items" while loading or after a failed load', async () => {
    getBOM.mockRejectedValue(new Error('HTTP error 500'))
    renderSection()
    expect(screen.queryByText(/No BOM items yet/)).not.toBeInTheDocument()
    expect(await screen.findByText("Couldn't load the bill of materials")).toBeInTheDocument()
    expect(screen.queryByText(/No BOM items yet/)).not.toBeInTheDocument()
  })

  it('shows the estimated cost in its recorded currency', async () => {
    getBOM.mockResolvedValue([row])
    renderSection()
    expect(await screen.findByText('MX$48.00')).toBeInTheDocument()
  })

  it('asks before removing a BOM line', async () => {
    getBOM.mockResolvedValue([row])
    deleteBOMItem.mockResolvedValue({})
    renderSection()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove PLA Filament' }))
    expect(deleteBOMItem).not.toHaveBeenCalled()
    expect(screen.getByText('Remove BOM item')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(deleteBOMItem).toHaveBeenCalledWith('t', 'p1', 'b1'))
  })
})
