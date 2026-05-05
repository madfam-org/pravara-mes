import type { NextConfig } from "next";

// Selva Atrium iframe allowance — see apps/pravara-ui/next.config.ts for the rationale.
// pravara-landing is the public marketing surface (mes.madfam.io); allowing the Atrium
// to embed it does not expose any authenticated user state.
const SELVA_FRAME_ANCESTORS =
  "frame-ancestors 'self' https://selva.town https://*.selva.town https://*.madfam.io";

const nextConfig: NextConfig = {
  output: "standalone",
  reactStrictMode: true,
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Frame-Options", value: "SAMEORIGIN" },
          { key: "Content-Security-Policy", value: SELVA_FRAME_ANCESTORS },
        ],
      },
    ];
  },
};

export default nextConfig;
