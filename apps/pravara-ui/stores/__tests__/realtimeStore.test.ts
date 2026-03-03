import { describe, it, expect, beforeEach } from 'vitest'
import { useRealtimeStore, selectIsConnected, selectIsConnecting, selectHasError } from '../realtimeStore'

describe('realtimeStore', () => {
  beforeEach(() => {
    // Reset store to initial state
    const { setConnectionState, setError, resetReconnectAttempts } = useRealtimeStore.getState()
    setConnectionState('disconnected')
    setError(null)
    resetReconnectAttempts()
  })

  describe('initial state', () => {
    it('starts with disconnected state', () => {
      const state = useRealtimeStore.getState()
      expect(state.connectionState).toBe('disconnected')
    })

    it('starts with null error', () => {
      const state = useRealtimeStore.getState()
      expect(state.error).toBeNull()
    })

    it('starts with zero reconnect attempts', () => {
      const state = useRealtimeStore.getState()
      expect(state.reconnectAttempts).toBe(0)
    })

    it('starts with null lastConnected', () => {
      const state = useRealtimeStore.getState()
      expect(state.lastConnected).toBeNull()
    })
  })

  describe('setConnectionState', () => {
    it('updates connection state to connected', () => {
      useRealtimeStore.getState().setConnectionState('connected')
      expect(useRealtimeStore.getState().connectionState).toBe('connected')
    })

    it('sets lastConnected when state becomes connected', () => {
      useRealtimeStore.getState().setConnectionState('connected')
      expect(useRealtimeStore.getState().lastConnected).toBeInstanceOf(Date)
    })

    it('clears error when state becomes connected', () => {
      useRealtimeStore.getState().setError('some error')
      useRealtimeStore.getState().setConnectionState('connected')
      expect(useRealtimeStore.getState().error).toBeNull()
    })

    it('preserves lastConnected when transitioning away from connected', () => {
      useRealtimeStore.getState().setConnectionState('connected')
      const lastConnected = useRealtimeStore.getState().lastConnected
      useRealtimeStore.getState().setConnectionState('disconnected')
      expect(useRealtimeStore.getState().lastConnected).toBe(lastConnected)
    })

    it('transitions through connecting state', () => {
      useRealtimeStore.getState().setConnectionState('connecting')
      expect(useRealtimeStore.getState().connectionState).toBe('connecting')
    })

    it('transitions to error state', () => {
      useRealtimeStore.getState().setConnectionState('error')
      expect(useRealtimeStore.getState().connectionState).toBe('error')
    })
  })

  describe('setError', () => {
    it('sets error message', () => {
      useRealtimeStore.getState().setError('Connection timeout')
      expect(useRealtimeStore.getState().error).toBe('Connection timeout')
    })

    it('clears error with null', () => {
      useRealtimeStore.getState().setError('some error')
      useRealtimeStore.getState().setError(null)
      expect(useRealtimeStore.getState().error).toBeNull()
    })
  })

  describe('reconnect attempts', () => {
    it('increments reconnect attempts', () => {
      useRealtimeStore.getState().incrementReconnectAttempts()
      expect(useRealtimeStore.getState().reconnectAttempts).toBe(1)
    })

    it('increments multiple times', () => {
      useRealtimeStore.getState().incrementReconnectAttempts()
      useRealtimeStore.getState().incrementReconnectAttempts()
      useRealtimeStore.getState().incrementReconnectAttempts()
      expect(useRealtimeStore.getState().reconnectAttempts).toBe(3)
    })

    it('resets reconnect attempts to zero', () => {
      useRealtimeStore.getState().incrementReconnectAttempts()
      useRealtimeStore.getState().incrementReconnectAttempts()
      useRealtimeStore.getState().resetReconnectAttempts()
      expect(useRealtimeStore.getState().reconnectAttempts).toBe(0)
    })
  })

  describe('selectors', () => {
    it('selectIsConnected returns true when connected', () => {
      useRealtimeStore.getState().setConnectionState('connected')
      expect(selectIsConnected(useRealtimeStore.getState())).toBe(true)
    })

    it('selectIsConnected returns false when disconnected', () => {
      expect(selectIsConnected(useRealtimeStore.getState())).toBe(false)
    })

    it('selectIsConnecting returns true when connecting', () => {
      useRealtimeStore.getState().setConnectionState('connecting')
      expect(selectIsConnecting(useRealtimeStore.getState())).toBe(true)
    })

    it('selectIsConnecting returns false when not connecting', () => {
      expect(selectIsConnecting(useRealtimeStore.getState())).toBe(false)
    })

    it('selectHasError returns true when error state', () => {
      useRealtimeStore.getState().setConnectionState('error')
      expect(selectHasError(useRealtimeStore.getState())).toBe(true)
    })

    it('selectHasError returns true when error message exists', () => {
      useRealtimeStore.getState().setError('timeout')
      expect(selectHasError(useRealtimeStore.getState())).toBe(true)
    })

    it('selectHasError returns false when no error', () => {
      expect(selectHasError(useRealtimeStore.getState())).toBe(false)
    })
  })
})
