import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest'
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

// ---- session with switchable roles ----
let roles: string[] = ['inventory_manager']
vi.mock('@/lib/auth', () => ({
  usePravaraSession: () => ({
    data: { user: { name: 'Test', accessToken: 'test-token', role: roles[0] ?? 'operator', roles, rolesKnown: true } },
    status: 'authenticated',
  }),
}))

const push = vi.fn()
let params: Record<string, string> = {}
vi.mock('next/navigation', () => ({
  usePathname: () => '/inventory/receipts',
  useRouter: () => ({ push }),
  useSearchParams: () => new URLSearchParams(),
  useParams: () => params,
}))

// ---- API mocked from the contract's shapes ----
const api = {
  materialsList: vi.fn(),
  materialsUnlinked: vi.fn(),
  receiptsList: vi.fn(),
  receiptsCreate: vi.fn(),
  countsGet: vi.fn(),
  countsClose: vi.fn(),
  consumptionList: vi.fn(),
  coverage: vi.fn(),
  gaps: vi.fn(),
}
vi.mock('@/lib/inventory-records-api', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/lib/inventory-records-api')>()
  return {
    ...real,
    materialsAPI: {
      ...real.materialsAPI,
      list: (...a: unknown[]) => api.materialsList(...a),
      unlinked: (...a: unknown[]) => api.materialsUnlinked(...a),
    },
    receiptsAPI: { list: (...a: unknown[]) => api.receiptsList(...a), create: (...a: unknown[]) => api.receiptsCreate(...a) },
    countsAPI: { ...real.countsAPI, get: (...a: unknown[]) => api.countsGet(...a), close: (...a: unknown[]) => api.countsClose(...a) },
    consumptionAPI: {
      ...real.consumptionAPI,
      list: (...a: unknown[]) => api.consumptionList(...a),
      coverage: (...a: unknown[]) => api.coverage(...a),
    },
    conversionGapsAPI: { list: (...a: unknown[]) => api.gaps(...a) },
  }
})

import { ApiError } from '@/lib/inventory-records-api'
import ReceiptsPage from '../receipts/page'
import CountDetailPage from '../counts/[id]/page'
import ConsumptionPage from '../consumption/page'
import AttentionPage from '../attention/page'
import MaterialsPage from '../materials/page'

beforeAll(() => {
  // Radix Select needs these in jsdom.
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.releasePointerCapture = () => {}
  Element.prototype.scrollIntoView = () => {}
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

const page = <T,>(data: T[]) => ({ data, total: data.length, limit: 100, offset: 0 })
const material = {
  id: 'm-1', code: 'FIL-PLA-BLK', name: 'PLA negro 1 kg', category: 'filament', base_unit: 'g', active: true,
  inventory_item_id: 'i-1', stock_sku: 'FIL-PLA-BLK', stock_unit: 'g', quantity_on_hand: 2976,
  created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z',
}

function renderWith(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

beforeEach(() => {
  roles = ['inventory_manager']
  params = {}
  Object.values(api).forEach((f) => f.mockReset())
  api.materialsList.mockResolvedValue(page([material]))
  api.materialsUnlinked.mockResolvedValue({ items: [], lots: [], consumption_codes: [] })
  api.receiptsList.mockResolvedValue(page([]))
})

async function pickMaterial() {
  const user = userEvent.setup()
  await user.click(await screen.findByRole('combobox', { name: 'Material' }))
  await user.click(await screen.findByRole('option', { name: /FIL-PLA-BLK · PLA negro/ }))
  return user
}

describe('Receipts', () => {
  it('does not let the invoice choice be skipped', async () => {
    renderWith(<ReceiptsPage />)
    await pickMaterial()
    fireEvent.change(screen.getByLabelText('Cantidad'), { target: { value: '2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Revisar y registrar' }))
    expect(await screen.findByText(/Indica si la entrada tiene factura o no/)).toBeInTheDocument()
    expect(api.receiptsCreate).not.toHaveBeenCalled()
  })

  it('confirms exactly what is recorded, then shows a 403 as its own state and reuses the key on resend', async () => {
    api.receiptsCreate.mockRejectedValueOnce(new ApiError(403, 'forbidden', 'role required'))
    renderWith(<ReceiptsPage />)
    await pickMaterial()
    fireEvent.change(screen.getByLabelText('Cantidad'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Unidad'), { target: { value: 'kg' } })
    fireEvent.click(screen.getByLabelText('Sin factura'))
    fireEvent.click(screen.getByRole('button', { name: 'Revisar y registrar' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Se registrará la entrada de 2 kg de FIL-PLA-BLK \(PLA negro 1 kg\), recibida el .*, sin lote, marcada como sin factura\.$/)).toBeInTheDocument()
    expect(within(dialog).getByText(/no se pueden editar ni anular/)).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Registrar entrada' }))

    expect(await within(dialog).findByText(/No tienes permiso para registrar entradas. No se registró nada./)).toBeInTheDocument()
    expect(within(dialog).queryByText(/El servidor/)).not.toBeInTheDocument()

    api.receiptsCreate.mockResolvedValueOnce({
      receipt: { ...material, id: 'r-1', material_id: 'm-1', material_code: 'FIL-PLA-BLK', quantity: 2, unit: 'kg', status: 'posted', stock_quantity: 2000, invoice_status: 'no_invoice' },
      stock_movement: {}, replayed: false,
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Registrar entrada' }))
    expect(await screen.findByText(/Entrada registrada: 2 kg de FIL-PLA-BLK/)).toBeInTheDocument()
    const keys = api.receiptsCreate.mock.calls.map((c) => c[1].idempotency_key)
    expect(keys[0]).toBeTruthy()
    expect(keys[1]).toBe(keys[0])
  })

  it('shows the list but not the form to someone without the inventory role', async () => {
    roles = ['operator']
    renderWith(<ReceiptsPage />)
    expect(await screen.findByText(/Solo una persona con rol de administrador o de responsable de inventario puede registrar entradas/)).toBeInTheDocument()
    expect(screen.queryByRole('form', { name: 'Registrar entrada' })).not.toBeInTheDocument()
    expect(await screen.findByText('Aún no hay entradas')).toBeInTheDocument()
  })

  it('says a failed list load failed instead of showing it empty', async () => {
    api.receiptsList.mockRejectedValue(new ApiError(500, 'internal_error', 'Failed to list receipts'))
    renderWith(<ReceiptsPage />)
    // Reads retry twice on a server failure before showing the error.
    expect(await screen.findByText('No se pudo cargar las entradas', {}, { timeout: 8000 })).toBeInTheDocument()
    expect(screen.getByText(/Detalle: Failed to list receipts/)).toBeInTheDocument()
    expect(screen.queryByText('Aún no hay entradas')).not.toBeInTheDocument()
  }, 12000)
})

describe('Count detail', () => {
  const openCount = {
    id: 'c-1', status: 'open', is_opening: true, created_at: '2026-11-14T09:00:00Z', line_count: 1,
    lines: [{ id: 'l-1', count_id: 'c-1', material_id: 'm-1', material_code: 'FIL-PLA-BLK', counted_quantity: 3, unit: 'kg', counted_at: '2026-11-14T09:30:00Z', updated_at: '2026-11-14T09:30:00Z' }],
  }

  it('reviews the difference and says plainly that closing cannot be undone', async () => {
    params = { id: 'c-1' }
    api.countsGet.mockResolvedValue(openCount)
    renderWith(<CountDetailPage />)
    expect(await screen.findByRole('heading', { name: 'Conteo de apertura' })).toBeInTheDocument()
    expect(await screen.findByText('+24 g')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cerrar conteo…' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Se cerrará el conteo con 1 material/)).toBeInTheDocument()
    expect(within(dialog).getByText(/no se puede deshacer/)).toBeInTheDocument()
  })

  it('says a newer count superseded the materials when closing is refused', async () => {
    params = { id: 'c-1' }
    api.countsGet.mockResolvedValue(openCount)
    api.countsClose.mockRejectedValue(new ApiError(409, 'newer_count_exists', 'a newer count closed', {
      error: 'newer_count_exists',
      materials: [{ material_id: 'm-1', material_code: 'FIL-PLA-BLK', newer_count_id: 'c-2' }],
    }))
    renderWith(<CountDetailPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Cerrar conteo…' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cerrar y registrar ajustes' }))
    expect(await within(dialog).findByText(/Un conteo más reciente ya cerró/)).toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: 'el conteo más reciente' })).toHaveAttribute('href', '/inventory/counts/c-2')
    expect(api.countsClose).toHaveBeenCalledTimes(1)
  })

  it('offers no count actions to someone without the inventory role', async () => {
    roles = ['operator']
    params = { id: 'c-1' }
    api.countsGet.mockResolvedValue(openCount)
    renderWith(<CountDetailPage />)
    expect(await screen.findByText(/puede registrar o cerrar conteos/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Cerrar conteo…' })).not.toBeInTheDocument()
  })
})

describe('Consumption', () => {
  it('keeps the corrected row in the history and offers "Corregir" only on the effective row', async () => {
    const base = { job_kind: 'print_job', job_id: 'a1b2c3d4-0000-0000-0000-000000000000', kind: 'material', material_code: 'PLA', registry_code: 'FIL-PLA-BLK', unit: 'g', occurred_at: '2026-11-14T10:00:00Z', disposition: 'good' }
    api.consumptionList.mockResolvedValue(page([
      { ...base, id: 'est', quantity: 5.2, basis: 'estimate', source: 'slicer', created_at: '2026-11-14T10:00:00Z' },
      { ...base, id: 'old', quantity: 25, basis: 'actual', source: 'operator', created_at: '2026-11-14T10:01:00Z', superseded_by: 'new' },
      { ...base, id: 'new', quantity: 30, basis: 'actual', source: 'operator', disposition: 'scrap', created_at: '2026-11-14T10:02:00Z', supersedes_id: 'old' },
    ]))
    renderWith(<ConsumptionPage />)
    expect(await screen.findByText('Historial (se conserva; ya no cuenta)')).toBeInTheDocument()
    expect(screen.getByText(/25 g · Real · operador · Bueno · registrado el .* sustituido por corrección/)).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^Corregir/ })).toHaveLength(1)
    expect(screen.getByText('No cuenta: otro registro de este trabajo es el vigente')).toBeInTheDocument()
    expect(screen.getByText(/Solo registra: no envía nada a ninguna impresora/)).toBeInTheDocument()
  })
})

describe('Needs attention', () => {
  it('shows the coverage figure, open conversion gaps and unlinked lots', async () => {
    api.coverage.mockResolvedValue({ material_rows: 120, with_registry_code: 118, without_registry_code: 2, unresolved_codes: [] })
    api.gaps.mockResolvedValue(page([
      { id: 'g-1', inventory_item_id: 'i-2', sku: 'SHT-ACR-3MM', material_id: 'm-2', material_code: 'SHT-ACR-3MM', source_type: 'job_consumption', source_id: 's', job_kind: 'task', job_id: 'j', quantity: 2, from_unit: 'sheet', to_unit: 'pcs', status: 'open', created_at: '2026-11-14T10:00:00Z' },
    ]))
    api.materialsUnlinked.mockResolvedValue({ items: [], lots: [{ id: 'l', lot_number: 'LOT-2', product_code: 'NOPE', product_name: 'Lote de prueba' }], consumption_codes: [] })
    renderWith(<AttentionPage />)
    expect(await screen.findByText('2 no lo tienen')).toBeInTheDocument()
    expect(await screen.findByText('sheet → pcs')).toBeInTheDocument()
    expect(await screen.findByText('LOT-2')).toBeInTheDocument()
  })
})

describe('Materials', () => {
  it('shows a permission state, not an empty registry, when reads are refused', async () => {
    api.materialsList.mockRejectedValue(new ApiError(403, 'forbidden', 'scope'))
    renderWith(<MaterialsPage />)
    expect(await screen.findByText('No tienes acceso a los materiales')).toBeInTheDocument()
    expect(screen.queryByText('Aún no hay materiales registrados')).not.toBeInTheDocument()
  })

  it('hides registry changes from someone without the inventory role', async () => {
    roles = ['operator']
    renderWith(<MaterialsPage />)
    await waitFor(() => expect(screen.getByText('FIL-PLA-BLK')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'Nuevo material' })).not.toBeInTheDocument()
    expect(screen.getByText(/puede registrar o cambiar materiales/)).toBeInTheDocument()
  })
})
