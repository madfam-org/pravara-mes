import { describe, it, expect, vi, afterEach } from 'vitest'
import { ApiError, consumptionAPI, materialsAPI, receiptsAPI } from '../inventory-records-api'

function respond(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('inventory records client', () => {
  it('sends the bearer token and only the filters that are set', async () => {
    const fetchMock = respond(200, { data: [], total: 0, limit: 100, offset: 0 })
    vi.stubGlobal('fetch', fetchMock)
    await materialsAPI.list('tok', { active: true, search: '', category: undefined, limit: 100 })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toMatch(/\/v1\/inventory\/materials\?active=true&limit=100$/)
    expect(init.headers.Authorization).toBe('Bearer tok')
    expect(init.method).toBe('GET')
  })

  it('keeps the status, the error code and the body of a refusal', async () => {
    vi.stubGlobal('fetch', respond(409, { error: 'already_superseded', message: 'row was corrected', superseded_by: 'x' }))
    const err = await consumptionAPI.correct('tok', 'row-1', { quantity: 3 }).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(409)
    expect(err.code).toBe('already_superseded')
    expect(err.message).toBe('row was corrected')
    expect(err.body.superseded_by).toBe('x')
  })

  it('reports an unreachable server as network_error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const err = await receiptsAPI.list('tok').catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(0)
    expect(err.code).toBe('network_error')
  })

  it('sends a receipt as JSON with its idempotency key', async () => {
    const fetchMock = respond(201, { receipt: {}, stock_movement: null, replayed: false })
    vi.stubGlobal('fetch', fetchMock)
    await receiptsAPI.create('tok', { material_id: 'm', quantity: 2, unit: 'kg', invoice_status: 'no_invoice', idempotency_key: 'k-1' })
    const [, init] = fetchMock.mock.calls[0]
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toMatchObject({ invoice_status: 'no_invoice', idempotency_key: 'k-1' })
  })

  it('marks a print job finished through the documented route only', async () => {
    const fetchMock = respond(200, { print_job_id: 'p', marked_completed: true, consumption: [], created: 1, stock_movements: [], material_recorded: false })
    vi.stubGlobal('fetch', fetchMock)
    await consumptionAPI.completePrintJob('tok', 'p-1', { disposition: 'failed_print' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toMatch(/\/v1\/print-jobs\/p-1\/complete$/)
  })
})
