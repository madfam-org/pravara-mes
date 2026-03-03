import { describe, it, expect, vi, beforeEach } from 'vitest'

// The auth.ts file exports NextAuth config with a refreshAccessToken function.
// We test the refresh logic by extracting and testing the jwt callback behavior.

// Mock NextAuth to capture the config
let capturedConfig: any = null
vi.mock('next-auth', () => ({
  default: (config: any) => {
    capturedConfig = config
    return {
      handlers: {},
      auth: vi.fn(),
      signIn: vi.fn(),
      signOut: vi.fn(),
    }
  },
}))

// We need to set env vars before importing
const originalEnv = process.env

describe('auth configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()
    process.env = {
      ...originalEnv,
      OIDC_ISSUER: 'https://auth.example.com',
      OIDC_CLIENT_ID: 'test-client',
      OIDC_CLIENT_SECRET: 'test-secret',
    }
  })

  it('configures the janua provider', async () => {
    await import('../auth')
    expect(capturedConfig).not.toBeNull()
    expect(capturedConfig.providers[0].id).toBe('janua')
    expect(capturedConfig.providers[0].name).toBe('Janua SSO')
    expect(capturedConfig.providers[0].type).toBe('oidc')
  })

  it('sets sign-in page to /login', async () => {
    await import('../auth')
    expect(capturedConfig.pages.signIn).toBe('/login')
    expect(capturedConfig.pages.error).toBe('/login')
  })

  it('jwt callback returns token with access token on initial sign-in', async () => {
    await import('../auth')
    const jwtCallback = capturedConfig.callbacks.jwt

    const result = await jwtCallback({
      token: { sub: 'user-1' },
      account: {
        access_token: 'new-access-token',
        refresh_token: 'new-refresh-token',
        expires_at: Math.floor(Date.now() / 1000) + 3600,
      },
      profile: { role: 'admin', tenant_id: 'tenant-1' },
    })

    expect(result.accessToken).toBe('new-access-token')
    expect(result.refreshToken).toBe('new-refresh-token')
    expect(result.role).toBe('admin')
    expect(result.tenantId).toBe('tenant-1')
  })

  it('jwt callback returns existing token if not expired (with 60s buffer)', async () => {
    await import('../auth')
    const jwtCallback = capturedConfig.callbacks.jwt

    const token = {
      sub: 'user-1',
      accessToken: 'existing-token',
      accessTokenExpires: Date.now() + 120 * 1000, // 2 min from now
      refreshToken: 'refresh-token',
    }

    const result = await jwtCallback({ token, account: null, profile: null })
    expect(result.accessToken).toBe('existing-token')
  })

  it('jwt callback attempts refresh when token is expired', async () => {
    const mockResponse = {
      ok: true,
      json: () => Promise.resolve({
        access_token: 'refreshed-token',
        expires_in: 3600,
        refresh_token: 'new-refresh-token',
      }),
    }
    global.fetch = vi.fn().mockResolvedValue(mockResponse)

    await import('../auth')
    const jwtCallback = capturedConfig.callbacks.jwt

    const token = {
      sub: 'user-1',
      accessToken: 'expired-token',
      accessTokenExpires: Date.now() - 1000, // expired
      refreshToken: 'old-refresh-token',
    }

    const result = await jwtCallback({ token, account: null, profile: null })
    expect(result.accessToken).toBe('refreshed-token')
    expect(result.refreshToken).toBe('new-refresh-token')
  })

  it('jwt callback returns error when refresh fails', async () => {
    const mockResponse = {
      ok: false,
      json: () => Promise.resolve({ error: 'invalid_grant' }),
    }
    global.fetch = vi.fn().mockResolvedValue(mockResponse)

    await import('../auth')
    const jwtCallback = capturedConfig.callbacks.jwt

    const token = {
      sub: 'user-1',
      accessToken: 'expired-token',
      accessTokenExpires: Date.now() - 1000,
      refreshToken: 'bad-refresh-token',
    }

    const result = await jwtCallback({ token, account: null, profile: null })
    expect(result.error).toBe('RefreshAccessTokenError')
  })

  it('session callback populates user properties from token', async () => {
    await import('../auth')
    const sessionCallback = capturedConfig.callbacks.session

    const session = { user: { name: 'Test' } }
    const token = {
      sub: 'user-1',
      accessToken: 'test-token',
      role: 'operator',
      tenantId: 'tenant-1',
    }

    const result = await sessionCallback({ session, token })
    expect((result.user as any).id).toBe('user-1')
    expect((result.user as any).accessToken).toBe('test-token')
    expect((result.user as any).role).toBe('operator')
    expect((result.user as any).tenantId).toBe('tenant-1')
  })

  it('session callback passes through token error', async () => {
    await import('../auth')
    const sessionCallback = capturedConfig.callbacks.session

    const session = { user: { name: 'Test' } }
    const token = {
      sub: 'user-1',
      error: 'RefreshAccessTokenError',
    }

    const result = await sessionCallback({ session, token })
    expect((result as any).error).toBe('RefreshAccessTokenError')
  })

  it('profile callback maps OIDC profile to user', async () => {
    await import('../auth')
    const profileFn = capturedConfig.providers[0].profile

    const result = profileFn({
      sub: 'user-1',
      name: 'Jane Doe',
      email: 'jane@example.com',
      picture: 'https://example.com/avatar.jpg',
      role: 'manager',
      tenant_id: 'tenant-5',
    })

    expect(result.id).toBe('user-1')
    expect(result.name).toBe('Jane Doe')
    expect(result.email).toBe('jane@example.com')
    expect(result.role).toBe('manager')
    expect(result.tenantId).toBe('tenant-5')
  })
})
