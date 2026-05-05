import type { NextConfig } from "next";

// Selva Atrium iframe allowance.
// The Atrium is the consumer-side feature in selva-office that surfaces every MADFAM
// platform as a window into a single welcoming central space. Permitting selva.town
// as a frame-ancestor lets the Atrium embed mes-app.madfam.io. X-Frame-Options:
// SAMEORIGIN remains as a legacy fallback. App-wide; auth surfaces inherit the same
// policy. Acceptable because Innovaciones MADFAM runs both Selva and Pravara MES.
const SELVA_FRAME_ANCESTORS =
  "frame-ancestors 'self' https://selva.town https://*.selva.town https://*.madfam.io";

const nextConfig: NextConfig = {
  output: "standalone",
  transpilePackages: ["@janua/nextjs", "@janua/ui", "@janua/react-sdk", "@janua/typescript-sdk"],
  experimental: {
    serverActions: {
      bodySizeLimit: "2mb",
    },
  },
  images: {
    remotePatterns: [
      {
        protocol: "https",
        hostname: "*.cloudflare.com",
      },
    ],
  },
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
