import { expect, test, type Browser, type Page } from '@playwright/test';
import { DAD, MUM, SMOKE_PASSWORD } from './smoke-accounts';

// 冒烟测试（v1.9.11）：在装好演示数据的实例上（见 playwright.config.ts）把常用的页面都走一遍。
// 每个测试都收集页面错误、控制台错误和失败的接口请求，最后必须一个都没有：
// v1.9.2 的整页白屏、详情页对每份文档多余的 /lab 请求，都会在这里被发现。
// 测试之间有先后（妈妈先被加进“王家”），所以按顺序运行。

test.describe.configure({ mode: 'serial' });

type Account = { login: string };

/** 收集问题：页面抛出的错误、控制台的 error、4xx/5xx 的接口请求。 */
function watch(page: Page, allow: (status: number, path: string) => boolean = () => false) {
  const problems: string[] = [];
  page.on('pageerror', (e) => problems.push(`页面错误：${e.message}`));
  page.on('console', (m) => {
    // 失败的请求浏览器也会在控制台报一次，下面按请求记
    if (m.type() === 'error' && !m.text().startsWith('Failed to load resource')) problems.push(`控制台：${m.text()}`);
  });
  page.on('response', (r) => {
    const u = new URL(r.url());
    if (!u.pathname.startsWith('/api/') || r.status() < 400 || allow(r.status(), u.pathname)) return;
    problems.push(`${r.status()} ${r.request().method()} ${u.pathname}`);
  });
  return problems;
}

async function login(page: Page, who: Account) {
  await page.goto('/');
  await page.getByLabel('用户名或邮箱', { exact: true }).fill(who.login);
  await page.getByLabel('密码', { exact: true }).fill(SMOKE_PASSWORD);
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();
}

async function useSpace(page: Page, name: string) {
  const switcher = page.getByRole('button', { name: /^当前空间：/ });
  // 登录后会话还在加载时点下去，菜单可能被重新渲染掉：没切过去就再来一次
  await expect(async () => {
    if ((await switcher.getAttribute('aria-label')) !== `当前空间：${name}`) {
      await switcher.click();
      await page.getByRole('menuitemradio', { name: new RegExp(name) }).click({ timeout: 2000 });
    }
    await expect(switcher).toHaveAccessibleName(`当前空间：${name}`, { timeout: 2000 });
  }).toPass({ timeout: 20_000 });
}

/** 当前登录的人在某个空间里按标题找文档的 ID（经接口，和页面同一个会话）。 */
async function docId(page: Page, space: string, title: string) {
  const spaces = (await (await page.request.get('/api/v1/workspaces')).json()) as { id: number; name: string }[];
  const ws = spaces.find((s) => s.name === space);
  expect(ws, `空间“${space}”`).toBeTruthy();
  const r = await page.request.get(`/api/v1/documents?q=${encodeURIComponent(title)}&limit=50`, { headers: { 'X-Workspace-ID': String(ws!.id) } });
  const items = ((await r.json()) as { items: { id: number; title: string }[] }).items;
  const d = items.find((x) => x.title === title);
  expect(d, `文档“${title}”`).toBeTruthy();
  return d!.id;
}

async function open(page: Page, title: string) {
  await page.goto(`/docs/${await docId(page, '王家', title)}`);
  await expect(page.getByRole('heading', { name: title, level: 1 }).first()).toBeVisible(); // 页头；正文里也可能有同名标题
}

test('列表进详情；各种格式都能打开；版本对比；打印入口', async ({ page }) => {
  test.setTimeout(180_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  const problems = watch(page);

  // 从“全部文件”点进详情（v1.9.2 的白屏就在这一步）
  await page.goto('/files');
  await page.getByRole('link', { name: /家里的网络和设备/ }).first().click();
  await expect(page.getByRole('heading', { name: '家里的网络和设备', level: 1 }).first()).toBeVisible();
  await expect(page.getByRole('heading', { name: '路由器' })).toBeVisible(); // Markdown 排好了

  await open(page, '阳台的花');
  await expect(page.locator('img[src*="/files/"], img[src*="/preview"], img[src*="/original"]').first()).toBeVisible();

  await open(page, '乐乐唱《小星星》');
  await expect(page.locator('audio')).toHaveCount(1);

  await open(page, '家庭账本');
  await expect(page.getByLabel('工作表')).toBeVisible();

  await open(page, '报销收据（压缩包）');
  await expect(page.getByLabel('压缩包里的文件')).toBeVisible();

  await open(page, '家庭应急手册'); // Word 阅读视图
  await page.getByRole('button', { name: '阅读', exact: true }).click();
  await expect(page.getByRole('region', { name: '脚注' })).toBeVisible();

  await open(page, '物业费缴费通知'); // Outlook 邮件
  await open(page, '暑假家庭旅行计划'); // 幻灯片
  await open(page, '餐饮电子发票'); // OFD
  await expect(page.getByLabel('第 1 页')).toBeVisible();

  // 打印入口
  await expect(page.getByRole('button', { name: '打印' }).first()).toBeVisible();

  // 两版 Word 合同：版本对比的两种看法
  await page.goto(`/docs/${await docId(page, '王家', '装修合同')}/compare`);
  await expect(page.getByText('砍价后的第二稿').first()).toBeVisible();

  expect(problems).toEqual([]);
});

test('上传入库', async ({ page }) => {
  test.setTimeout(90_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  const problems = watch(page);
  await page.goto('/inbox');
  await page.getByRole('button', { name: '上传', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles({ name: '冒烟测试便条.txt', mimeType: 'text/plain', buffer: Buffer.from('冒烟测试：记得交物业费') });
  // 更多预设（v1.9.12）：标签和日期
  await dialog.getByRole('button', { name: /更多预设/ }).click();
  await dialog.getByRole('combobox', { name: '标签' }).click();
  const tag = (await page.getByRole('option').first().textContent())!;
  await page.getByRole('option').first().click(); // 选中后弹层自己收起；不按 Esc，免得连上传框一起关掉
  await expect(page.getByRole('listbox')).toBeHidden();
  await dialog.getByLabel('日期').fill('2024-03-05');
  await dialog.getByRole('button', { name: '上传 1 个文件' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('已进入待整理')).toBeVisible({ timeout: 30_000 });
  await page.getByText('冒烟测试便条.txt').click();
  await expect(page.getByLabel('日期')).toHaveValue('2024-03-05');
  await expect(page.getByText(tag, { exact: true }).first()).toBeVisible();
  expect(problems).toEqual([]);
});

test('分享链接：只能在线看', async ({ page, browser }) => {
  test.setTimeout(90_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  const problems = watch(page);
  await open(page, '餐饮电子发票');
  await page.getByRole('button', { name: '更多', exact: true }).click();
  await page.getByRole('menuitem', { name: '分享链接' }).click();
  const dialog = page.getByRole('dialog', { name: '分享链接' });
  await dialog.getByLabel('允许下载').uncheck();
  await dialog.getByRole('button', { name: '生成链接' }).click();
  const url = await dialog.getByRole('textbox', { name: '新链接' }).inputValue();
  expect(problems).toEqual([]);

  // 没登录的人打开：看得到，不能下载
  const anon = await newPage(browser);
  const anonProblems = watch(anon);
  await anon.goto(new URL(url).pathname);
  await expect(anon.getByText('餐饮电子发票').first()).toBeVisible();
  await expect(anon.getByText(/只能在线查看/)).toBeVisible();
  await expect(anon.getByRole('link', { name: '下载' })).toHaveCount(0);
  expect(anonProblems).toEqual([]);
});

async function newPage(browser: Browser) {
  const ctx = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  return ctx.newPage();
}

test('设置里的每一页都能打开', async ({ page }) => {
  test.setTimeout(120_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  const problems = watch(page);
  // 每个分组里的每一项都点开（以后加的设置页自动包括进来）；页面崩了会有页面错误
  await page.goto('/settings/profile');
  const groups = page.getByRole('tablist', { name: '设置分组' }).getByRole('tab');
  await expect(groups).toHaveCount(3); // 个人、空间、实例管理（爸爸是实例管理员）
  let seen = 0;
  for (let g = 0; g < (await groups.count()); g++) {
    const tab = groups.nth(g);
    await tab.click();
    const items = page.getByRole('navigation', { name: (await tab.textContent()) ?? '', exact: true }).getByRole('button');
    await expect(items.first()).toBeVisible();
    for (let i = 0; i < (await items.count()); i++) {
      await items.nth(i).click();
      await expect(page).toHaveURL(/\/settings\/[a-z-]+$/);
      await expect(page.getByRole('main')).not.toBeEmpty();
      seen++;
    }
  }
  expect(seen).toBeGreaterThan(20);
  for (const path of ['/inbox', '/review', '/directory', '/trash', '/tasks', '/health', '/scenes', '/checklists', '/book-lists', '/highlights', '/duplicates', '/cleanup', '/ask']) {
    await page.goto(path);
    await expect(page.getByRole('main')).not.toBeEmpty();
  }
  expect(problems).toEqual([]);
});

test('私密文档：另一位家人看不到', async ({ page, browser }) => {
  test.setTimeout(90_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  // 爸爸自己看得到，再把妈妈加进“王家”
  await search(page, '体检报告');
  const own = page.getByRole('link', { name: /2023年度体检报告/ });
  await expect(own).toBeVisible();
  const id = (await own.getAttribute('href'))!.match(/\/docs\/(\d+)/)![1];
  await page.goto('/settings/members');
  await page.getByRole('button', { name: /添加成员/ }).click();
  const add = page.getByRole('dialog');
  await add.getByLabel(/用户名或邮箱/).fill(MUM.login);
  await add.getByRole('button', { name: '添加' }).click();
  await expect(page.getByText(MUM.login).first()).toBeVisible();

  const mum = await newPage(browser);
  await login(mum, MUM);
  await useSpace(mum, '王家');
  const problems = watch(mum);
  await search(mum, '体检报告');
  await expect(mum.getByRole('link', { name: /体检报告/ }).first()).toBeVisible(); // 公开的那份看得到
  await expect(mum.getByRole('link', { name: /2023年度体检报告/ })).toHaveCount(0);
  // 直接打开它：和不存在的文档一样
  const r = await mum.request.get(`/api/v1/documents/${id}`, { headers: { 'X-Workspace-ID': String(await wsId(mum, '王家')) } });
  expect(r.status()).toBe(404);
  expect(problems).toEqual([]);
});

async function wsId(page: Page, name: string) {
  const spaces = (await (await page.request.get('/api/v1/workspaces')).json()) as { id: number; name: string }[];
  return spaces.find((s) => s.name === name)!.id;
}

async function search(page: Page, q: string) {
  await page.goto('/files');
  await page.getByRole('textbox', { name: '搜索文档' }).fill(q);
  await page.getByRole('textbox', { name: '搜索文档' }).press('Enter');
}

test('英文界面（v1.10.0）', async ({ page }) => {
  test.setTimeout(150_000);
  await login(page, DAD);
  await useSpace(page, '王家');
  const problems = watch(page);
  // 设置 → 界面 → 语言：English，页面按英文重新加载
  await page.goto('/settings/ui');
  await page.getByRole('button', { name: 'English' }).click();
  await expect(page.getByRole('heading', { name: 'Interface', level: 4 })).toBeVisible();
  const CJK = /[㐀-鿿]/;

  await page.goto('/files');
  await expect(page.getByRole('heading', { name: /^All files · \d+ documents$/ })).toBeVisible();
  await page.getByRole('link', { name: /家里的网络和设备/ }).first().click(); // 文档标题是数据，仍是中文
  await expect(page.getByText('Mark as done')).toBeVisible();
  for (const label of ['Drawer', 'Document type', 'Correspondent', 'Person', 'Date', 'Tags', 'ASN', 'Location']) {
    await expect(page.getByText(label, { exact: true }).first()).toBeVisible();
  }

  // 设置的每一页：标题和左边的导航都是英文
  await page.goto('/settings/profile');
  const groups = page.getByRole('tablist').getByRole('tab');
  await expect(groups).toHaveCount(3);
  for (let g = 0; g < 3; g++) {
    const tab = groups.nth(g);
    await tab.click();
    expect(await tab.textContent()).not.toMatch(CJK);
    const items = page.getByRole('navigation', { name: (await tab.textContent()) ?? '', exact: true }).getByRole('button');
    await expect(items.first()).toBeVisible();
    for (let i = 0; i < (await items.count()); i++) {
      await items.nth(i).click();
      expect(await items.nth(i).textContent(), `settings item ${i}`).not.toMatch(CJK);
      const h = page.getByRole('main').getByRole('heading', { level: 4 }).first();
      await expect(h).toBeVisible();
      expect(await h.textContent()).not.toMatch(CJK);
    }
  }
  // 主导航
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  for (const name of ['Home', 'Inbox', 'All files', 'Year in review', 'Ask', 'Checklists', 'Directory', 'Trash', 'Settings']) {
    await expect(nav.getByRole('button', { name: new RegExp(`^${name}( \\d+)?$`) }).first()).toBeVisible(); // 待整理带数量
  }
  expect(problems).toEqual([]);

  // 切回中文，别的测试按中文找
  await page.goto('/settings/ui');
  await page.getByRole('button', { name: '中文' }).click();
  await expect(page.getByRole('heading', { name: '界面', level: 4 })).toBeVisible();
});
