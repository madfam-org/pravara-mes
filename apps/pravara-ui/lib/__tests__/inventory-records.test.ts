import { describe, it, expect } from 'vitest'
import { ApiError, type ConsumptionRow } from '../inventory-records-api'
import {
  buildConsumptionChains,
  canManageInventory,
  convertBuiltin,
  explainError,
  formatQty,
  localInputToRfc3339,
  parseQuantity,
  previewCountDifference,
  retryRead,
} from '../inventory-records'

function row(over: Partial<ConsumptionRow>): ConsumptionRow {
  return {
    id: 'r', job_kind: 'print_job', job_id: 'job-1', kind: 'material', material_code: 'FIL-A',
    registry_code: 'FIL-A', quantity: 10, unit: 'g', basis: 'estimate', source: 'slicer',
    occurred_at: '2026-11-14T10:00:00Z', created_at: '2026-11-14T10:00:00Z', disposition: 'good',
    ...over,
  }
}

describe('explainError', () => {
  it('gives the meaning of a documented code and keeps the server message', () => {
    const e = explainError(new ApiError(409, 'material_code_taken', 'code taken'))
    expect(e.meaning).toMatch(/Ese código ya existe/)
    expect(e.serverMessage).toBe('code taken')
  })

  it('explains the refusals of the final consumption contract', () => {
    expect(explainError(new ApiError(409, 'print_job_not_started', 'queued')).meaning).toMatch(/sigue en cola/)
    expect(explainError(new ApiError(409, 'not_effective', 'x')).meaning).toMatch(/registro vigente/)
    expect(explainError(new ApiError(409, 'disposition_differs', 'x')).meaning).toMatch(/corrige su consumo/)
    expect(explainError(new ApiError(409, 'idempotency_key_reused', 'x')).meaning).toMatch(/otros datos/)
    expect(explainError(new ApiError(409, 'newer_count_exists', 'x')).meaning).toMatch(/más reciente/)
  })

  it('falls back by status for an unknown code', () => {
    expect(explainError(new ApiError(503, 'http_503', 'HTTP error 503')).meaning).toBe('El servidor tuvo un error.')
    expect(explainError(new ApiError(403, 'forbidden', 'no')).meaning).toMatch(/permiso/)
  })
})

describe('retryRead', () => {
  it('never retries a 4xx and retries server failures twice', () => {
    expect(retryRead(0, new ApiError(403, 'forbidden', ''))).toBe(false)
    expect(retryRead(0, new ApiError(500, 'internal_error', ''))).toBe(true)
    expect(retryRead(2, new ApiError(500, 'internal_error', ''))).toBe(false)
  })
})

describe('canManageInventory', () => {
  it('offers gated writes to admin and inventory_manager only', () => {
    expect(canManageInventory({ roles: ['inventory_manager'], rolesKnown: true })).toBe(true)
    expect(canManageInventory({ roles: ['admin'], rolesKnown: true })).toBe(true)
    expect(canManageInventory({ roles: ['operator'], rolesKnown: true })).toBe(false)
  })

  it('leaves the decision to the API when the token names no roles', () => {
    expect(canManageInventory({ roles: [], rolesKnown: false })).toBe(true)
  })
})

describe('quantities and dates', () => {
  it('parses up to 4 decimals and accepts a decimal comma', () => {
    expect(parseQuantity('2,5')).toBe(2.5)
    expect(parseQuantity('1.23456')).toBeNull()
    expect(parseQuantity('abc')).toBeNull()
    expect(parseQuantity('')).toBeNull()
  })

  it('formats in es-MX without currency', () => {
    expect(formatQty(2976.5)).toBe('2,976.5')
    expect(formatQty(undefined)).toBe('—')
  })

  it('turns a datetime-local value into RFC 3339 with an offset', () => {
    expect(localInputToRfc3339('2026-11-14T10:00')).toMatch(/^2026-11-14T10:00:00[+-]\d{2}:\d{2}$/)
    expect(localInputToRfc3339('nope')).toBeNull()
  })

  it('converts with the built-in conversions only', () => {
    expect(convertBuiltin(2, 'kg', 'g')).toBe(2000)
    expect(convertBuiltin(500, 'ml', 'l')).toBe(0.5)
    expect(convertBuiltin(1, 'spool', 'g')).toBeNull()
  })

  it('previews a count difference against today, or says it cannot', () => {
    const m = { quantity_on_hand: 2976, stock_unit: 'g' }
    expect(previewCountDifference(3, 'kg', m)).toEqual({ onHand: 2976, counted: 3000, difference: 24, unit: 'g' })
    expect(previewCountDifference(1, 'spool', m)).toBeNull()
    expect(previewCountDifference(1, 'g', undefined)).toBeNull()
  })
})

describe('buildConsumptionChains', () => {
  it('keeps what a correction replaced as history, newest first', () => {
    const rows = [
      row({ id: 'c2', basis: 'actual', source: 'operator', quantity: 30, supersedes_id: 'c1', created_at: '2026-11-14T12:00:00Z' }),
      row({ id: 'c1', basis: 'actual', source: 'operator', quantity: 25, supersedes_id: 'a', superseded_by: 'c2', created_at: '2026-11-14T11:00:00Z' }),
      row({ id: 'a', basis: 'actual', source: 'printer', quantity: 20, superseded_by: 'c1', created_at: '2026-11-14T10:30:00Z' }),
    ]
    const chains = buildConsumptionChains(rows)
    expect(chains).toHaveLength(1)
    expect(chains[0].current.id).toBe('c2')
    expect(chains[0].history.map((h) => h.id)).toEqual(['c1', 'a'])
    expect(chains[0].effective).toBe(true)
  })

  it('marks the latest actual as the only effective row of a job and material', () => {
    const chains = buildConsumptionChains([
      row({ id: 'est', basis: 'estimate', source: 'slicer' }),
      row({ id: 'act', basis: 'actual', source: 'operator', created_at: '2026-11-14T10:05:00Z' }),
      row({ id: 'time', kind: 'machine_time', material_code: undefined, registry_code: undefined, unit: 'min' }),
    ])
    const effective = Object.fromEntries(chains.map((c) => [c.current.id, c.effective]))
    expect(effective).toEqual({ est: false, act: true, time: true })
  })

  it('says when history is outside the loaded page', () => {
    const [chain] = buildConsumptionChains([row({ id: 'c', supersedes_id: 'gone' })])
    expect(chain.historyIncomplete).toBe(true)
  })
})
