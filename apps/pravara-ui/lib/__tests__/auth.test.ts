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
  signOutOfPravara,
  usePravaraSignOut,
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
  it("keeps this application's roles without the prefix", () => {
    expect(rolesFromClaims({ roles: ['pravara-mes:inventory_manager', 'pravara-mes:operator'] })).toEqual({
      roles: ['inventory_manager', 'operator'],
      known: true,
    })
  })

  it("drops other products' roles and bare organization roles", () => {
    expect(
      rolesFromClaims({ roles: ['admin', 'tulana:admin', 'hcm:hr', 'pravara-mes:operator'], role: 'admin' }),
    ).toEqual({ roles: ['operator'], known: true })
  })

  it('says roles are unknown when the claims carry no roles array', () => {
    expect(rolesFromClaims({ id: 'u' })).toEqual({ roles: [], known: false })
    expect(rolesFromClaims({ role: 'admin' })).toEqual({ roles: [], known: false })
  })

  it('ignores values that are not role names', () => {
    expect(rolesFromClaims({ roles: ['pravara-mes:admin', 3, '', 'pravara-mes:', 'pravara-mes:a:b'] })).toEqual({
      roles: ['admin'],
      known: true,
    })
  })
})

describe('signOutOfPravara', () => {
  const assign = vi.fn()
  const original = window.location

  beforeEach(() => {
    assign.mockReset()
    localStorage.clear()
    localStorage.setItem('janua_access_token', 'a')
    localStorage.setItem('janua_refresh_token', 'r')
    localStorage.setItem('janua_token_expires_at', '1')
    localStorage.setItem('unrelated', 'keep')
    Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign } })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { configurable: true, value: original })
  })

  it('signs out through the client, clears the session and goes to the sign-in page', async () => {
    const client = { signOut: vi.fn().mockResolvedValue(undefined) }
    await signOutOfPravara(client)
    expect(client.signOut).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem('janua_access_token')).toBeNull()
    expect(localStorage.getItem('janua_refresh_token')).toBeNull()
    expect(localStorage.getItem('janua_token_expires_at')).toBeNull()
    expect(localStorage.getItem('unrelated')).toBe('keep')
    expect(assign).toHaveBeenCalledWith('/login')
  })

  it('never navigates to the identity service', async () => {
    await signOutOfPravara({ signOut: vi.fn().mockResolvedValue(undefined) }, { callbackUrl: '/login' })
    expect(assign).toHaveBeenCalledTimes(1)
    expect(String(assign.mock.calls[0][0])).not.toMatch(/^https?:/)
  })

  it('still clears the session and leaves when the client fails', async () => {
    const client = { signOut: vi.fn().mockRejectedValue(new Error('network')) }
    await expect(signOutOfPravara(client)).resolves.toBeUndefined()
    expect(localStorage.getItem('janua_access_token')).toBeNull()
    expect(localStorage.getItem('janua_refresh_token')).toBeNull()
    expect(assign).toHaveBeenCalledWith('/login')
  })

  it('works with no client at all', async () => {
    await signOutOfPravara(null)
    expect(localStorage.getItem('janua_access_token')).toBeNull()
    expect(assign).toHaveBeenCalledWith('/login')
  })

  it('honours a callback address', async () => {
    await signOutOfPravara(null, { callbackUrl: '/login?from=settings' })
    expect(assign).toHaveBeenCalledWith('/login?from=settings')
  })
})

describe('usePravaraSignOut', () => {
  it('signs out with the provider client', async () => {
    const assign = vi.fn()
    const original = window.location
    Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign } })
    const client = { signOut: vi.fn().mockResolvedValue(undefined) }
    mockUseJanua.mockReturnValue({ client })
    const { result } = renderHook(() => usePravaraSignOut())
    await act(async () => { await result.current() })
    expect(client.signOut).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith('/login')
    Object.defineProperty(window, 'location', { configurable: true, value: original })
  })
})
