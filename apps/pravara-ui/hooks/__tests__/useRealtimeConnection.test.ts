import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useRealtimeConnection } from '../useRealtimeConnection'

// Mock next-auth
const mockSession = vi.fn()
vi.mock('@/lib/auth', () => ({
  usePravaraSession: () => mockSession(),
}))

// Mock realtime client
const mockConnect = vi.fn()
const mockDisconnect = vi.fn()
const mockOnConnectionStateChange = vi.fn().mockReturnValue(vi.fn())

vi.mock('@/lib/realtime/client', () => ({
  realtimeClient: {
    connect: (...args: any[]) => mockConnect(...args),
    disconnect: (...args: any[]) => mockDisconnect(...args),
    onConnectionStateChange: (...args: any[]) => mockOnConnectionStateChange(...args),
  },
}))

// Mock realtime store
const mockStoreState = {
  connectionState: 'disconnected' as const,
  error: null as string | null,
  reconnectAttempts: 0,
  setConnectionState: vi.fn(),
  setError: vi.fn(),
  incrementReconnectAttempts: vi.fn(),
  resetReconnectAttempts: vi.fn(),
}

vi.mock('@/stores/realtimeStore', () => ({
  useRealtimeStore: Object.assign(
    (selector?: any) => {
      if (selector) return selector(mockStoreState)
      return mockStoreState
    },
    { getState: () => mockStoreState }
  ),
  selectIsConnected: (state: any) => state.connectionState === 'connected',
  selectIsConnecting: (state: any) => state.connectionState === 'connecting',
}))

describe('useRealtimeConnection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockStoreState.connectionState = 'disconnected'
    mockStoreState.error = null
    mockStoreState.reconnectAttempts = 0
    mockSession.mockReturnValue({
      data: {
        accessToken: 'test-token',
        user: { tenantId: 'tenant-1' },
      },
      status: 'authenticated',
    })
  })

  it('returns connection state properties', () => {
    const { result } = renderHook(() => useRealtimeConnection())
    expect(result.current.connectionState).toBe('disconnected')
    expect(result.current.isConnected).toBe(false)
    expect(result.current.isConnecting).toBe(false)
    expect(result.current.error).toBeNull()
    expect(result.current.reconnectAttempts).toBe(0)
  })

  it('returns connect and disconnect functions', () => {
    const { result } = renderHook(() => useRealtimeConnection())
    expect(typeof result.current.connect).toBe('function')
    expect(typeof result.current.disconnect).toBe('function')
  })

  it('registers connection state change listener on mount', () => {
    renderHook(() => useRealtimeConnection())
    expect(mockOnConnectionStateChange).toHaveBeenCalled()
  })

  it('calls disconnect and cleanup on unmount when unauthenticated', () => {
    mockSession.mockReturnValue({ data: null, status: 'unauthenticated' })
    renderHook(() => useRealtimeConnection())
    expect(mockDisconnect).toHaveBeenCalled()
  })

  it('does not auto-connect when autoConnect is false', () => {
    renderHook(() => useRealtimeConnection({ autoConnect: false }))
    expect(mockConnect).not.toHaveBeenCalled()
  })

  it('does not attempt connection when session is unauthenticated', () => {
    mockSession.mockReturnValue({ data: null, status: 'unauthenticated' })
    renderHook(() => useRealtimeConnection())
    expect(mockConnect).not.toHaveBeenCalled()
  })

  it('connect sets error when no session available', async () => {
    mockSession.mockReturnValue({ data: null, status: 'authenticated' })
    const { result } = renderHook(() => useRealtimeConnection({ autoConnect: false }))
    await act(async () => {
      await result.current.connect()
    })
    expect(mockStoreState.setError).toHaveBeenCalledWith('No session or tenant ID available')
  })

  it('disconnect calls client disconnect and resets state', () => {
    const { result } = renderHook(() => useRealtimeConnection())
    act(() => {
      result.current.disconnect()
    })
    expect(mockDisconnect).toHaveBeenCalled()
    expect(mockStoreState.resetReconnectAttempts).toHaveBeenCalled()
    expect(mockStoreState.setError).toHaveBeenCalledWith(null)
  })

  it('does not connect when already connected', async () => {
    mockStoreState.connectionState = 'connected'
    const { result } = renderHook(() => useRealtimeConnection({ autoConnect: false }))
    await act(async () => {
      await result.current.connect()
    })
    expect(mockConnect).not.toHaveBeenCalled()
  })
})
