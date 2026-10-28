# 收好 Kept · 网页端（Vite + React + TypeScript + MUI）

收好的网页端，按网页端的 UI 设计规范实现。v0.1 范围内的页面，以及 v0.2 的连接手机、已连接的设备，都已接入真实接口；仍显示示例数据（页面顶部有提示条）的只有 AI 与智能体（v0.3 起）。生产环境中网页端编译进服务端的二进制（`cd ../server && make web build`）。

## 运行

需要 Node.js 22、pnpm。运行网页端之前需要先启动后端：

```bash
cd ../server && make run   # 监听 :8000
```

然后在 `app/` 目录下：

```bash
pnpm install
pnpm dev            # 本地开发，默认 http://localhost:5173，代理 /api 到后端（KEPT_API_URL 可改后端地址）
pnpm build          # 类型检查并打包到 dist/
pnpm typecheck      # TypeScript 类型检查
pnpm test           # 单元测试（Vitest）
pnpm test:e2e       # 端到端测试（Playwright），见下文
pnpm gen:api        # 根据 ../api/openapi.yaml 生成接口类型 src/api/schema.d.ts
```

### 端到端测试

`e2e/` 下 10 套测试（auth、workspaces、taxonomy、ingest、documents、search、classify、drawers、devices、pages），覆盖设计文档 6.3 的关键流程。每套各启动一个全新的后端（临时数据目录）和 Vite 开发服务器，并行运行，全部约 40 秒。

- 首次运行前：`pnpm exec playwright install chromium`；入库相关的几套需要本机装有 poppler 和 ocrmypdf。
- 默认从 8000 / 5173 起每套占用一对端口；端口被占用时用 `KEPT_E2E_API_PORT`、`KEPT_E2E_WEB_PORT` 指定起始端口。
- 只运行其中几套：`KEPT_E2E_SUITES=documents,search pnpm test:e2e`。

## 版式宽度

- 列表类页面铺满（左右 24px）：全部文件、待整理、抽屉页、目录、回收站。
- 首页铺满；宽度 ≥ 1536px 时分两栏：左栏是待整理提示、提醒和我的抽屉，右栏（400px，滚动时吸附）是最近添加和年度回顾入口。
- 设置页靠左，内容区铺满；表单输入框各自限宽，表格和卡片随宽度伸展。
- 年度回顾铺满：宽度 ≥ 1200px 时分两行两栏——第一行左边是开篇（年份、数字、AI 叙述），右边是放大的每月入库柱状图；第二行左边是各抽屉，右边是用途标签和大事记。窄屏时恢复单栏。
- 详情页为预览 65% / 信息栏 35%。

## 技术栈

- Vite 5、React 18、TypeScript 5
- MUI v9（@mui/material + Emotion）。MUI 默认是 Material Design 2，这里按《文档管理系统UI设计规范》的“清爽现代”令牌定制主题（系统字体、强调色 #2563EB、小圆角、细边框；表单输入框按 MUI 官方 Text Field 的标准描边样式，标签上浮），见 `src/theme.ts`
- lucide-react 图标（线宽 1.75）
- TanStack Query 5（服务端数据）、openapi-fetch（接口类型由 OpenAPI 生成）
- react-router-dom 6（BrowserRouter；生产环境由 Go 服务端托管并做前端路由回退）
- pdf.js（预览）；Vitest + Testing Library（单元测试）；Playwright（端到端测试）
- 字体：Roboto（@fontsource 本地打包）+ 系统中文字体
- 没有使用 Tailwind：样式全部通过 MUI 主题和 `sx` 完成，避免两套样式体系并存

## 目录结构

```
src/
  main.tsx / app.tsx    入口与路由
  theme.ts              MUI 主题（按 UI 设计规范第三部分的设计令牌，浅色与深色）
  features.ts           已接入真实接口的页面；其余页面显示“示例数据”提示条
  api/                  接口层：client.ts（openapi-fetch、CSRF、当前空间）、各业务的查询与修改（documents、taxonomy、drawers、system……）、events.ts（SSE）；schema.d.ts 由 pnpm gen:api 生成
  auth/                 首次初始化、登录、两步验证、忘记密码
  store/app-store.tsx   界面状态（React Context）：侧边栏、提示消息、确认对话框、上传对话框等
  data/mock.ts          后续版本页面（AI 与智能体）的示例数据
  utils/                收纳规则的平铺条件与筛选 JSON 互转（drawer-rules.ts）、要点的显示（facts.ts）
  components/
    layout.tsx          应用框架：侧边栏（抽屉、常用筛选、空间切换）、顶栏、全局快捷键
    doc-list.tsx        文档列表：搜索、筛选条（带数量）、条件胶囊、列表 / 网格、排序、批量操作栏
    uploads.tsx         上传对话框与任务托盘
    command-palette.tsx ⌘K 命令面板
    drawer-rule-panel.tsx 编辑收纳规则（平铺条件与实时预览）
    pickers.tsx / doc-fields.tsx 类型、往来方、标签、抽屉的选择框与字段
    pdf-preview.tsx / doc-preview.tsx 预览
  pages/
    home.tsx            首页（提醒、我的抽屉、最近添加；没有抽屉时的模板选择）
    lists.tsx           全部文件
    inbox/              待整理（逐份整理 / 批量整理）
    detail/             文档详情（就地编辑、备注、修改历史、分类依据；编辑页面与拆分见 components/page-tools.tsx）
    drawer/             抽屉页（要点、规则说明、范围内的文档）
    shared/             共享给当前空间的抽屉与其中的文档（只读）
    directory/          目录与条目页
    trash/              回收站
    yearbook-page.tsx   年度回顾（打印即导出 PDF）
    settings.tsx        设置；settings/ 下是个人资料、密码与两步验证、API Token、空间成员、活动记录、自动分类、任务记录、
                        连接手机与已连接的设备、邮件收取、账号管理、名称与 Logo、系统状态、日志
e2e/                    Playwright 端到端测试
```

文件名统一用小写短横线（kebab-case），例如 `login-form.tsx`、`two-factor.ts`；组件名、类型名仍用大驼峰（`LoginForm`）。

## 快捷键

- 侧边栏：`[` 折叠或展开；⌘K / Ctrl+K 打开命令面板；`U` 上传。
- 待整理：`A` 完成整理，`S` 跳过，`J` / `K` 上下切换，`V` 切换原件和识别文字，`T` / `Y` / `C` 打开标签、类型、往来方选择框。
- 全部文件：`/` 聚焦搜索框，`Esc` 取消选择。
- 前往：`G` 然后 `H` / `I` / `F` / `R`（首页、待整理、全部文件、年度回顾）。

## 后续版本的页面

设置中的“AI 与智能体”仍是原型的示例数据，只为保留交互设计，页面顶部有提示条。AI 在其他页面上的呈现（待整理的识别员标记、整理助手、问问 AI 等，UI 设计规范 5.12）见原型 `../app-orgin`。
