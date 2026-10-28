import { fileURLToPath } from 'node:url';
import { defineConfig } from '@playwright/test';
import { DAD, MUM, SMOKE_PASSWORD } from './e2e/smoke-accounts';

// 每个测试文件需要一个全新的实例（“首次初始化”只能做一次），所以各用一套后端（全新的临时数据目录）和 Vite 开发服务器。
// 默认从 8000/5173 起每个文件占用一对端口；本机这些端口被占用时，用 KEPT_E2E_API_PORT、KEPT_E2E_WEB_PORT 指定起始端口。
const apiPort = Number(process.env.KEPT_E2E_API_PORT ?? 8000);
const webPort = Number(process.env.KEPT_E2E_WEB_PORT ?? 5173);

const ALL = ['auth', 'workspaces', 'taxonomy', 'ingest', 'documents', 'search', 'classify', 'drawers', 'devices', 'pages', 'books', 'smoke'];
// KEPT_E2E_SUITES=documents,search 只运行（并只启动）这几套
const only = process.env.KEPT_E2E_SUITES?.split(',').map((s) => s.trim()).filter(Boolean);
const SUITES = only?.length ? ALL.filter((s) => only.includes(s)) : ALL;

// 后端的程序和数据目录放在仓库里的 .e2e-tmp/<套件>（已忽略）：每次启动前清空这一套的目录，测试结束后整个删除（e2e/global-teardown.ts）。
// 以前用 mktemp -d，exec 之后没有机会清理，每次运行都在系统临时目录里留下一个约 85 MB 的程序。
export const E2E_TMP = fileURLToPath(new URL('../.e2e-tmp', import.meta.url));

// 冒烟测试（v1.9.11）从装好演示数据的实例开始：两个账号，演示数据装在爸爸名下（输出里有恢复码，不打印）。
const SMOKE_SETUP = `export KEPT_PASSWORD='${SMOKE_PASSWORD}' && "$D/kept" user create --admin --email ${DAD.email} ${DAD.login} >/dev/null && "$D/kept" user create --email ${MUM.email} ${MUM.login} >/dev/null && "$D/kept" demo load ${DAD.login} >/dev/null && unset KEPT_PASSWORD && `;

export default defineConfig({
  testDir: './e2e',
  // CI 上重试一次，挡住偶发的超时；本机不重试，失败就看
  retries: process.env.CI ? 1 : 0,
  // 十几套后端和 Vite 同时跑，机器忙时页面要多等一会儿；默认的 5 秒不够
  expect: { timeout: 10_000 },
  forbidOnly: !!process.env.CI,
  globalTeardown: './e2e/global-teardown.ts',
  projects: SUITES.map((name, i) => ({
    name,
    testMatch: `${name}.spec.ts`,
    // KEPT_E2E_CHANNEL=chrome 用本机装好的 Chrome，不用另外下载浏览器
    // 浏览器语言固定中文：界面跟随浏览器语言（v1.10.0），测试按中文找元素；英文由冒烟测试里单独切换
    use: { baseURL: `http://localhost:${webPort + i}`, channel: process.env.KEPT_E2E_CHANNEL || undefined, locale: 'zh-CN' },
  })),
  webServer: SUITES.flatMap((name, i) => [
    {
      // 先编译再 exec：go run 的子进程在测试结束后不会随之退出
      command: `D='${E2E_TMP}/${name}' && rm -rf "$D" && mkdir -p "$D" && go build -o "$D/kept" ./cmd/kept && export KEPT_DATA_DIR="$D/data" && ${name === 'smoke' ? SMOKE_SETUP : ''}KEPT_HTTP_ADDR=127.0.0.1:${apiPort + i} exec "$D/kept" serve`,
      cwd: '../server',
      url: `http://127.0.0.1:${apiPort + i}/healthz`,
      reuseExistingServer: false,
      timeout: 120_000,
      stdout: 'ignore', // 访问日志太多；出错时看 stderr
    },
    {
      // 直接 exec vite：经过 pnpm 时，并行启动的第二个 pnpm 会提前退出，留下没人管的 vite，测试结束时卡住
      command: `KEPT_API_URL=http://127.0.0.1:${apiPort + i} exec node_modules/.bin/vite --port ${webPort + i} --strictPort`,
      url: `http://localhost:${webPort + i}`,
      reuseExistingServer: false,
    },
  ]),
});
