import type { NextConfig } from "next";

// Selva Atrium iframe allowance — see apps/pravara-ui/next.config.ts for the rationale.
const SELVA_FRAME_ANCESTORS =
  "frame-ancestors 'self' https://selva.town https://*.selva.town https://*.madfam.io";

const nextConfig: NextConfig = {
  output: "standalone",
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
