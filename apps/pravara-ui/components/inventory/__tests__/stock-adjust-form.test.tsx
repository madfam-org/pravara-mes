import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { InventoryItem } from '@/lib/api'

const adjust = vi.fn()
vi.mock('@/lib/api', () => ({
  inventoryAPI: { adjust: (...args: unknown[]) => adjust(...args) },
}))

import { StockAdjustForm } from '../stock-adjust-form'

const item: InventoryItem = {
  id: 'item-1',
  tenant_id: 't',
  sku: 'FIL-PLA-BLK',
  name: 'PLA black',
  unit: 'g',
  quantity_on_hand: 100,
  quantity_reserved: 0,
  quantity_available: 100,
  reorder_point: 20,
  reorder_quantity: 50,
  currency: 'MXN',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
} as InventoryItem

function renderForm(props: Partial<React.ComponentProps<typeof StockAdjustForm>> = {}) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  const onDone = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <StockAdjustForm item={item} token="t" layout="card" onDone={onDone} {...props} />
    </QueryClientProvider>
  )
  return { onDone }
}

describe('StockAdjustForm', () => {
  beforeEach(() => adjust.mockReset())

  it('records a consumption as a negative delta and previews the result', async () => {
    adjust.mockResolvedValueOnce({})
    const { onDone } = renderForm({ defaultType: 'consumption' })
    fireEvent.change(screen.getByLabelText('Quantity consumed (g)'), { target: { value: '30' } })
    expect(screen.getByText('On hand 100 → 70 g')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(adjust).toHaveBeenCalledWith('t', 'item-1', {
      quantity: -30,
      transaction_type: 'consumption',
      notes: undefined,
    })
  })

  it('records a receipt as a positive delta', async () => {
    adjust.mockResolvedValueOnce({})
    renderForm({ defaultType: 'receipt' })
    fireEvent.change(screen.getByLabelText('Quantity received (g)'), { target: { value: '25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(adjust).toHaveBeenCalled())
    expect(adjust.mock.calls[0][2].quantity).toBe(25)
  })

  it('keeps Save disabled until there is an amount', () => {
    renderForm()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('says when the movement could not be recorded and keeps the form open', async () => {
    adjust.mockRejectedValueOnce(new Error('HTTP error 500'))
    const { onDone } = renderForm()
    fireEvent.change(screen.getByLabelText('Quantity received (g)'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      "Couldn't record the movement: HTTP error 500"
    )
    expect(onDone).not.toHaveBeenCalled()
  })
})
