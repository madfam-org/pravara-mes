import { describe, it, expect } from 'vitest'
import { machinesNeedingAttention, summarizeMachines } from '../factory-summary'
import type { Machine, MachineStatus } from '../api'

const machine = (name: string, status: MachineStatus): Machine => ({
  id: name,
  tenant_id: 't',
  name,
  code: name.toUpperCase(),
  type: 'printer',
  status,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
})

describe('summarizeMachines', () => {
  it('is all zeros for an empty factory', () => {
    expect(summarizeMachines([])).toEqual({
      counted: 0, running: 0, ready: 0, maintenance: 0, error: 0, offline: 0,
    })
  })

  it('counts each status once and treats online and idle as ready', () => {
    const summary = summarizeMachines([
      machine('a', 'running'),
      machine('b', 'online'),
      machine('c', 'idle'),
      machine('d', 'maintenance'),
      machine('e', 'error'),
      machine('f', 'offline'),
    ])
    expect(summary).toEqual({
      counted: 6, running: 1, ready: 2, maintenance: 1, error: 1, offline: 1,
    })
    const { counted, ...byStatus } = summary
    expect(Object.values(byStatus).reduce((a, b) => a + b, 0)).toBe(counted)
  })

  it('counts a status it does not know as offline rather than dropping it', () => {
    expect(summarizeMachines([machine('a', 'unknown' as MachineStatus)]).offline).toBe(1)
  })
})

describe('machinesNeedingAttention', () => {
  it('lists machines in error before those in maintenance, by name', () => {
    const list = machinesNeedingAttention([
      machine('zeta', 'maintenance'),
      machine('beta', 'error'),
      machine('alpha', 'running'),
      machine('alpha-2', 'error'),
    ])
    expect(list.map((m) => m.name)).toEqual(['alpha-2', 'beta', 'zeta'])
  })

  it('is empty when every machine is fine', () => {
    expect(machinesNeedingAttention([machine('a', 'idle'), machine('b', 'offline')])).toEqual([])
  })
})
