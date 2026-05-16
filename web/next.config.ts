import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // 生产构建为静态站点(Nginx 直接 serve out/ 目录)
  output: "export",
  images: { unoptimized: true },

  // 开发环境把 /api/* 代理到 Go API 服务,避免跨域。
  // 生产环境由 Nginx 反代,这段规则只在 next dev 生效。
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination:
          process.env.NEXT_PUBLIC_API_BASE
            ? `${process.env.NEXT_PUBLIC_API_BASE}/api/:path*`
            : "http://localhost:8080/api/:path*",
      },
    ];
  },
};

export default nextConfig;
