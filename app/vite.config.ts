/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// 开发时后端的地址；需要同时跑第二套前后端时用 KEPT_API_URL 指到另一个端口。
const apiTarget = process.env.KEPT_API_URL ?? 'http://127.0.0.1:8000';

// 开源构建（KEPT_OSS=1 或没有 src/ee，v1.13）：'@ee' 换成空实现，不编译也不测试 src/ee 里的收费模块。
// 导出的开源树里没有 src/ee，也按开源构建。
const oss = !!process.env.KEPT_OSS || !existsSync(new URL('./src/ee', import.meta.url));
const ee = fileURLToPath(new URL(oss ? './src/oss/ee.tsx' : './src/ee/index.ts', import.meta.url));

// 生产环境由 Go 服务端托管并做前端路由回退，因此 base 为 '/'，路由用 BrowserRouter。
export default defineConfig({
  base: '/',
  plugins: [react()],
  resolve: { alias: { '@ee': ee } },
  server: {
    port: 5173,
    // 不要设置 changeOrigin：保留浏览器的 Host，后端的同源校验（Origin 与 Host 一致）才能通过。
    // 字符串简写会隐式开启 changeOrigin，所以必须写成对象形式。
    proxy: {
      '/api': { target: apiTarget, changeOrigin: false },
      '/healthz': { target: apiTarget, changeOrigin: false },
      '/manifest.webmanifest': { target: apiTarget, changeOrigin: false },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: oss ? ['src/ee/**', 'node_modules/**'] : ['node_modules/**'],
  },
});
