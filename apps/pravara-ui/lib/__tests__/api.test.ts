import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// We need to test the fetchAPI function and API objects.
// Since fetchAPI is not exported directly, we test through the public API objects.

// Mock fetch globally
const mockFetch = vi.fn()
global.fetch = mockFetch

// Import after mock setup
import { ordersAPI, machinesAPI, tasksAPI } from '../api'

describe('API client', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('fetchAPI (tested through ordersAPI)', () => {
    it('includes Authorization header when token is provided', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ data: [], total: 0, limit: 50, offset: 0 }),
      })

      await ordersAPI.list('my-token')

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer my-token',
            'Content-Type': 'application/json',
          }),
        })
      )
    })

    it('throws error on non-ok response with message from body', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: () => Promise.resolve({ message: 'Order not found' }),
      })

      await expect(ordersAPI.get('token', 'bad-id')).rejects.toThrow('Order not found')
    })

    it('throws generic error when error body has no message', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: () => Promise.resolve({}),
      })

      await expect(ordersAPI.get('token', 'bad-id')).rejects.toThrow('HTTP error 500')
    })

    it('handles json parse failure in error response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 502,
        json: () => Promise.reject(new Error('not json')),
      })

      await expect(ordersAPI.get('token', 'id')).rejects.toThrow('HTTP error 502')
    })
  })

  describe('ordersAPI', () => {
    it('list calls GET /v1/orders', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ data: [], total: 0, limit: 50, offset: 0 }),
      })

      await ordersAPI.list('token')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/orders'),
        expect.any(Object)
      )
    })

    it('list includes query params when provided', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ data: [], total: 0, limit: 50, offset: 0 }),
      })

      const params = new URLSearchParams({ status: 'received' })
      await ordersAPI.list('token', params)
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('?status=received'),
        expect.any(Object)
      )
    })

    it('create calls POST /v1/orders with body', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ id: 'order-1' }),
      })

      await ordersAPI.create('token', { customer_name: 'Acme' })
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/orders'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ customer_name: 'Acme' }),
        })
      )
    })

    it('update calls PATCH /v1/orders/:id', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ id: 'order-1' }),
      })

      await ordersAPI.update('token', 'order-1', { priority: 1 })
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/orders/order-1'),
        expect.objectContaining({ method: 'PATCH' })
      )
    })

    it('delete calls DELETE /v1/orders/:id', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ message: 'deleted' }),
      })

      await ordersAPI.delete('token', 'order-1')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/orders/order-1'),
        expect.objectContaining({ method: 'DELETE' })
      )
    })
  })

  describe('machinesAPI', () => {
    it('sendCommand calls POST /v1/machines/:id/command', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ command_id: 'cmd-1' }),
      })

      await machinesAPI.sendCommand('token', 'machine-1', 'start_job')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/machines/machine-1/command'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ command: 'start_job', parameters: undefined }),
        })
      )
    })

    it('getTelemetry calls GET /v1/machines/:id/telemetry', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ machine_id: 'machine-1', data: [] }),
      })

      await machinesAPI.getTelemetry('token', 'machine-1')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/machines/machine-1/telemetry'),
        expect.any(Object)
      )
    })
  })

  describe('tasksAPI', () => {
    it('getBoard calls GET /v1/tasks/board', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ columns: {} }),
      })

      await tasksAPI.getBoard('token')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/tasks/board'),
        expect.any(Object)
      )
    })

    it('move calls POST /v1/tasks/:id/move', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ message: 'moved' }),
      })

      await tasksAPI.move('token', 'task-1', 'in_progress', 2)
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/tasks/task-1/move'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ status: 'in_progress', position: 2 }),
        })
      )
    })

    it('assign calls POST /v1/tasks/:id/assign', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ message: 'assigned' }),
      })

      await tasksAPI.assign('token', 'task-1', 'user-1', 'machine-1')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/tasks/task-1/assign'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ user_id: 'user-1', machine_id: 'machine-1' }),
        })
      )
    })
  })
})
