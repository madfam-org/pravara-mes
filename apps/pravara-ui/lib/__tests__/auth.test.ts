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
import { usePravaraSession, pravaraSignIn, pravaraSignOut } from '../auth'

describe('usePravaraSession', () => {
  it('returns loading status when auth is loading', () => {
    mockUseJanua.mockReturnValue({ client: null })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: true })

    const result = usePravaraSession()
    expect(result.status).toBe('loading')
    expect(result.data).toBeNull()
  })

  it('returns unauthenticated when not authenticated', () => {
    mockUseJanua.mockReturnValue({ client: null })
    mockUseUser.mockReturnValue({ user: null })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: false, isLoading: false })

    const result = usePravaraSession()
    expect(result.status).toBe('unauthenticated')
    expect(result.data).toBeNull()
  })

  it('returns authenticated session with user data', () => {
    mockUseJanua.mockReturnValue({
      client: { getAccessToken: () => 'test-access-token' },
    })
    mockUseUser.mockReturnValue({
      user: {
        id: 'user-1',
        name: 'Jane Doe',
        email: 'jane@example.com',
        role: 'operator',
        tenant_id: 'tenant-1',
      },
    })
    mockUseJanuaAuth.mockReturnValue({ isAuthenticated: true, isLoading: false })

    const result = usePravaraSession()
    expect(result.status).toBe('authenticated')
    expect(result.data?.user.id).toBe('user-1')
    expect(result.data?.user.name).toBe('Jane Doe')
    expect(result.data?.user.role).toBe('operator')
    expect(result.data?.user.tenantId).toBe('tenant-1')
    expect(result.data?.accessToken).toBe('test-access-token')
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
