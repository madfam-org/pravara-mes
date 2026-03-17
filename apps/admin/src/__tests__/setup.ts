/**
 * Vitest setup file for PravaraMES Admin auth route tests.
 *
 * Provides default environment variables used by the auth routes
 * so individual tests don't have to repeat them.
 */

// Default env vars for tests (routes read these at module scope)
process.env.NEXT_PUBLIC_JANUA_BASE_URL = "https://auth.test.io";
process.env.NEXT_PUBLIC_JANUA_PUBLISHABLE_KEY = "test-client-id";
process.env.JANUA_SECRET_KEY = "test-secret-key-at-least-32-chars-long!!";
process.env.NODE_ENV = "test";
