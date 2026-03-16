import type { NextConfig } from "next";

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
};

export default nextConfig;
