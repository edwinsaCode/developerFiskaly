import type { NextConfig } from "next";

// Proxy same-origin browser → backend. Browser memanggil "/api/v1/*" (same-origin),
// Next.js (server) meneruskan ke backend lewat URL internal. Ini menghilangkan
// CORS dan masalah hostname Docker ("backend") yang tak resolvable di browser.
const BACKEND = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  // distDir override for local dev (root-owned .next from container build blocks
  // `next dev`). Defaults to ".next" in production/container — no behavior change.
  distDir: process.env.NEXT_DIST_DIR || ".next",
  async rewrites() {
    return [
      { source: "/api/v1/:path*", destination: `${BACKEND}/api/v1/:path*` },
      { source: "/health", destination: `${BACKEND}/health` },
    ];
  },
};

export default nextConfig;
