import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

// Production: static export (`out/`) served by Caddy, with no Node.js server (ADR 0009).
// Development: Next rewrites proxy the API so the browser stays same-origin (ADR 0005).
// Rewrites and `output: "export"` cannot coexist, so each phase gets only what it supports.
export default function config(phase: string): NextConfig {
  const base: NextConfig = {
    reactStrictMode: true,
    turbopack: {
      rules: {
        "*.css": { loaders: ["@tailwindcss/turbopack"], as: "*.css" },
      },
    },
  };

  if (phase !== PHASE_DEVELOPMENT_SERVER) {
    return { ...base, output: "export" };
  }

  const apiUrl = process.env.API_URL ?? "http://localhost:8090";
  return {
    ...base,
    async rewrites() {
      return [
        { source: "/api/:path*", destination: `${apiUrl}/api/:path*` },
        { source: "/health", destination: `${apiUrl}/health` },
      ];
    },
  };
}
