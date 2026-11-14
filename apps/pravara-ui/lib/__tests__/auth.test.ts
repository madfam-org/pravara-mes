import { describe, it, expect, vi } from 'vitest'

// Mock @janua/nextjs hooks
const mockUseJanua = vi.fn()
const mockUseUser = vi.fn()
const mockUseJanuaAuth = vi.fn()

vi.mock('@janua/nextjs', () => ({
  useJanua: () => mockUseJanua(),
  useUser: () => mockUseUser(),
  useAuth: () => mockUseJanuaAuth(),
}))

// Import after mocks
import { renderHook, waitFor, act } from '@testing-library/react'
import { beforeEach, afterEach } from 'vitest'
import {
  usePravaraSession,
  pravaraSignIn,
  pravaraSignOut,
  rolesFromClaims,
  currentAccessToken,
  resetSessionProbeForTests,
} from '../auth'

const signedOutClient = {
  getAccessToken: async () => null,
  auth: { getCurrentUser: async () => null },
}

const janeUser = {
  id: 'user-1',
  name: 'Jane Doe',
  email: 'jane@example.com',
  role: 'operator',
  tenant_id: 'tenant-1',
}

describe('usePravaraSession', () => {
  beforeEach(() => {
    resetSessionProbeForTests()
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns loading status when auth is loading', () => {
    mockUseJanua.mockReturnValue({ client: signedOutClient })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: true })

    const { result } = renderHook(() => usePravaraSession())
    expect(result.current.status).toBe('loading')
    expect(result.current.data).toBeNull()
  })

  it('returns unauthenticated once no stored sign-in is found', async () => {
    mockUseJanua.mockReturnValue({ client: signedOutClient })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: false })

    const { result } = renderHook(() => usePravaraSession())
    await waitFor(() => expect(result.current.status).toBe('unauthenticated'))
    expect(result.current.data).toBeNull()
  })

  it('stays loading while a stored sign-in is still being resolved', async () => {
    // The provider says "not loading, no user" the instant it starts its
    // fetch; a stored token must not read as signed out in that window.
    let resolveUser: (u: unknown) => void = () => {}
    const client = {
      getAccessToken: async () => 'stored-token',
      auth: { getCurrentUser: () => new Promise((r) => { resolveUser = r }) },
    }
    mockUseJanua.mockReturnValue({ client })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: false })

    const { result, rerender } = renderHook(() => usePravaraSession())
    expect(result.current.status).toBe('loading')
    await act(async () => { await Promise.resolve() })
    expect(result.current.status).toBe('loading')

    // The token resolves to a user: still loading until the provider follows.
    await act(async () => { resolveUser(janeUser); await Promise.resolve() })
    expect(result.current.status).toBe('loading')

    mockUseUser.mockReturnValue({ user: janeUser })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: true, isLoading: false })
    rerender()
    expect(result.current.status).toBe('authenticated')
  })

  it('returns unauthenticated when the stored token does not resolve', async () => {
    const client = {
      getAccessToken: async () => 'expired-token',
      auth: { getCurrentUser: async () => { throw new Error('401') } },
    }
    mockUseJanua.mockReturnValue({ client })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: false })

    const { result } = renderHook(() => usePravaraSession())
    await waitFor(() => expect(result.current.status).toBe('unauthenticated'))
  })

  it('stops waiting when the provider never follows a resolved token', async () => {
    vi.useFakeTimers()
    const client = {
      getAccessToken: async () => 'stored-token',
      auth: { getCurrentUser: async () => janeUser },
    }
    mockUseJanua.mockReturnValue({ client })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: false })

    const { result } = renderHook(() => usePravaraSession())
    await act(async () => { await vi.advanceTimersByTimeAsync(100) })
    expect(result.current.status).toBe('loading')
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(result.current.status).toBe('unauthenticated')
  })

  it('returns authenticated session with user data', () => {
    localStorage.setItem('janua_access_token', 'test-access-token')
    mockUseJanua.mockReturnValue({
      client: { getAccessToken: async () => 'test-access-token', auth: { getCurrentUser: async () => janeUser } },
    })
    mockUseUser.mockReturnValue({ user: janeUser })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: true, isLoading: false })

    const { result } = renderHook(() => usePravaraSession())
    expect(result.current.status).toBe('authenticated')
    expect(result.current.data?.user.id).toBe('user-1')
    expect(result.current.data?.user.name).toBe('Jane Doe')
    expect(result.current.data?.user.role).toBe('operator')
    expect(result.current.data?.user.tenantId).toBe('tenant-1')
    expect(result.current.data?.accessToken).toBe('test-access-token')
  })

  it('never sends a Promise as the access token', () => {
    // client.getAccessToken() is async in the SDK.
    mockUseJanua.mockReturnValue({
      client: { getAccessToken: async () => 'from-client', auth: { getCurrentUser: async () => janeUser } },
      session: { accessToken: 'from-provider' },
    })
    mockUseUser.mockReturnValue({ user: janeUser })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: true, isLoading: false })

    const { result } = renderHook(() => usePravaraSession())
    expect(result.current.data?.accessToken).toBe('from-provider')
    expect(result.current.data?.accessToken).not.toContain('Promise')
  })
})

describe('currentAccessToken', () => {
  beforeEach(() => localStorage.clear())

  it('prefers the token the SDK keeps in storage', () => {
    localStorage.setItem('janua_access_token', 'refreshed')
    expect(currentAccessToken('at-load')).toBe('refreshed')
  })

  it('falls back to the provider token, then to empty', () => {
    expect(currentAccessToken('at-load')).toBe('at-load')
    expect(currentAccessToken(null)).toBe('')
  })
})

describe('rolesFromClaims', () => {
  it('joins the roles array claim with the single role claim', () => {
    expect(rolesFromClaims({ roles: ['inventory_manager', 'operator'], role: 'operator' })).toEqual({
      roles: ['inventory_manager', 'operator'],
      known: true,
    })
  })

  it('says roles are unknown when the token names none', () => {
    expect(rolesFromClaims({ id: 'u' })).toEqual({ roles: [], known: false })
  })

  it('ignores values that are not role names', () => {
    expect(rolesFromClaims({ roles: ['admin', 3, ''] })).toEqual({ roles: ['admin'], known: true })
  })
})

describe('pravaraSignIn', () => {
  it('is a function', () => {
    expect(typeof pravaraSignIn).toBe('function')
  })
})

describe('pravaraSignOut', () => {
  it('is a function', () => {
    expect(typeof pravaraSignOut).toBe('function')
  })
})
