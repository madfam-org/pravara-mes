import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import React from 'react'
import { useMachineUpdates } from '../useMachineUpdates'

// Capture the subscription callbacks
let capturedSubscriptionCallbacks: any = null
const mockUnsubscribe = vi.fn()

vi.mock('@/lib/realtime/channels', () => ({
  subscribeMachines: (callbacks: any) => {
    capturedSubscriptionCallbacks = callbacks
    return mockUnsubscribe
  },
}))

// Mock query client
const mockSetQueriesData = vi.fn()
const mockSetQueryData = vi.fn()
const mockInvalidateQueries = vi.fn()
const mockRemoveQueries = vi.fn()

vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({
    setQueriesData: mockSetQueriesData,
    setQueryData: mockSetQueryData,
    invalidateQueries: mockInvalidateQueries,
    removeQueries: mockRemoveQueries,
  }),
}))

// Mock store
let mockIsConnected = true
vi.mock('@/stores/realtimeStore', () => ({
  useRealtimeStore: (selector: any) => {
    if (selector) {
      return selector({ connectionState: mockIsConnected ? 'connected' : 'disconnected' })
    }
    return { connectionState: mockIsConnected ? 'connected' : 'disconnected' }
  },
  selectIsConnected: (state: any) => state.connectionState === 'connected',
}))

describe('useMachineUpdates', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    capturedSubscriptionCallbacks = null
    mockIsConnected = true
  })

  it('returns isConnected status', () => {
    const { result } = renderHook(() => useMachineUpdates())
    expect(result.current.isConnected).toBe(true)
  })

  it('subscribes to machine events when connected', () => {
    renderHook(() => useMachineUpdates())
    expect(capturedSubscriptionCallbacks).not.toBeNull()
    expect(capturedSubscriptionCallbacks).toHaveProperty('onStatusChange')
    expect(capturedSubscriptionCallbacks).toHaveProperty('onHeartbeat')
    expect(capturedSubscriptionCallbacks).toHaveProperty('onCommandAck')
    expect(capturedSubscriptionCallbacks).toHaveProperty('onCreate')
    expect(capturedSubscriptionCallbacks).toHaveProperty('onUpdate')
    expect(capturedSubscriptionCallbacks).toHaveProperty('onDelete')
  })

  it('does not subscribe when disconnected', () => {
    mockIsConnected = false
    renderHook(() => useMachineUpdates())
    expect(capturedSubscriptionCallbacks).toBeNull()
  })

  it('unsubscribes on unmount', () => {
    const { unmount } = renderHook(() => useMachineUpdates())
    unmount()
    expect(mockUnsubscribe).toHaveBeenCalled()
  })

  it('calls onStatusChange callback when status changes', () => {
    const onStatusChange = vi.fn()
    renderHook(() => useMachineUpdates({ onStatusChange }))

    const statusData = {
      machine_id: 'machine-1',
      machine_name: 'CNC Mill',
      new_status: 'running',
      updated_at: '2025-01-01T00:00:00Z',
    }
    capturedSubscriptionCallbacks.onStatusChange(statusData)

    expect(onStatusChange).toHaveBeenCalledWith(statusData)
    expect(mockSetQueriesData).toHaveBeenCalled()
    expect(mockSetQueryData).toHaveBeenCalled()
  })

  it('calls onHeartbeat callback and updates cache', () => {
    const onHeartbeat = vi.fn()
    renderHook(() => useMachineUpdates({ onHeartbeat }))

    const heartbeatData = {
      machine_id: 'machine-1',
      last_heartbeat: '2025-01-01T00:00:00Z',
      is_online: true,
    }
    capturedSubscriptionCallbacks.onHeartbeat(heartbeatData)

    expect(onHeartbeat).toHaveBeenCalledWith(heartbeatData)
    expect(mockSetQueriesData).toHaveBeenCalled()
  })

  it('calls onCreate callback and invalidates queries', () => {
    const onCreate = vi.fn()
    renderHook(() => useMachineUpdates({ onCreate }))

    const createData = {
      entity_id: 'machine-new',
      entity_type: 'machine',
      name: 'New Machine',
      created_by: 'user-1',
      created_at: '2025-01-01T00:00:00Z',
    }
    capturedSubscriptionCallbacks.onCreate(createData)

    expect(onCreate).toHaveBeenCalledWith(createData)
    expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: ['machines'] })
  })

  it('calls onDelete callback and removes from cache', () => {
    const onDelete = vi.fn()
    renderHook(() => useMachineUpdates({ onDelete }))

    const deleteData = {
      entity_id: 'machine-1',
      entity_type: 'machine',
      name: 'Old Machine',
      deleted_by: 'user-1',
      deleted_at: '2025-01-01T00:00:00Z',
    }
    capturedSubscriptionCallbacks.onDelete(deleteData)

    expect(onDelete).toHaveBeenCalledWith(deleteData)
    expect(mockSetQueriesData).toHaveBeenCalled()
    expect(mockRemoveQueries).toHaveBeenCalledWith({ queryKey: ['machines', 'machine-1'] })
  })

  it('calls onCommandAck callback', () => {
    const onCommandAck = vi.fn()
    renderHook(() => useMachineUpdates({ onCommandAck }))

    const ackData = {
      command_id: 'cmd-1',
      machine_id: 'machine-1',
      success: true,
      acknowledged_at: '2025-01-01T00:00:00Z',
    }
    capturedSubscriptionCallbacks.onCommandAck(ackData)

    expect(onCommandAck).toHaveBeenCalledWith(ackData)
  })
})
