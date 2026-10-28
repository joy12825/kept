import { expect, test } from '@playwright/test';
import { freshTotp, totpAt } from './totp';

// 端到端覆盖登录页对接的完整链路：首次初始化、邮箱登录、开启两步验证、两步登录、密码错误提示。
// playwright.config.ts 为整个测试运行启动一次全新数据目录的后端，因此这里用一个测试串起全部步骤，
// 复用同一个账号的状态（初始化只能进行一次）。

const NAME = '测试管理员';
const EMAIL = 'admin@example.com';
const PASSWORD = 'Passw0rd!23';

test('首次使用：创建管理员、邮箱登录、开启两步验证、两步登录、密码错误提示', async ({ page }) => {
  // 步骤较多，且第 4 步可能要等到下一个 30 秒时间片，给足够的总时长。
  test.setTimeout(120_000);

  // 1. 初始化：填写名字、邮箱、密码、确认密码，创建并进入。
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '创建管理员账号' })).toBeVisible();
  await page.getByLabel('名字', { exact: true }).fill(NAME);
  await page.getByLabel('邮箱', { exact: true }).fill(EMAIL);
  await page.getByLabel('密码', { exact: true }).fill(PASSWORD);
  await page.getByLabel('确认密码', { exact: true }).fill(PASSWORD);
  await page.getByRole('button', { name: '创建并进入' }).click();
  // 等设置完成、会话 cookie 真正落地后再跳转，避免与仍在进行的初始化请求竞态。
  await expect(page.getByRole('heading', { name: '创建管理员账号' })).not.toBeVisible();

  // 个人资料的“显示名称”“邮箱”是必填字段，MUI 会在可访问名称后追加一个星号，
  // 因此这里不用精确匹配（下面几处同理）。
  await page.goto('/settings/profile');
  await expect(page.getByLabel('显示名称')).toHaveValue(NAME);
  await expect(page.getByLabel('邮箱')).toHaveValue(EMAIL);

  // 2. 通过用户菜单退出，再用邮箱登录。
  await page.goto('/');
  await page.getByText(`${NAME} 的空间`).click();
  await page.getByRole('menuitem', { name: '退出登录' }).click();
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible();

  await page.getByLabel('用户名或邮箱', { exact: true }).fill(EMAIL);
  await page.getByLabel('密码', { exact: true }).fill(PASSWORD);
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByRole('heading', { name: '登录' })).not.toBeVisible();
  await page.goto('/settings/profile');
  await expect(page.getByLabel('显示名称')).toHaveValue(NAME);

  // 3. 在“设置 → 密码与安全”开启两步验证：从页面读取手动输入的密钥，用它算出验证码。
  await page.goto('/settings/password');
  await page.getByRole('button', { name: '开启两步验证' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: '开启两步验证' })).toBeVisible();
  const dialogText = await dialog.innerText();
  const match = dialogText.match(/手动输入密钥：\s*([A-Z0-9 ]+)/);
  if (!match) throw new Error(`没有在对话框中找到密钥：${dialogText}`);
  const secret = match[1].replace(/\s+/g, '');

  const setupTotp = totpAt(secret);
  await dialog.getByLabel('6 位验证码').fill(setupTotp.code);
  await dialog.getByRole('button', { name: '验证并开启' }).click();

  await expect(page.getByRole('heading', { name: '恢复码' })).toBeVisible();
  await page.getByRole('button', { name: '完成' }).click();
  await expect(page.getByRole('heading', { name: '恢复码' })).not.toBeVisible();

  // 4. 退出后重新登录：出现两步验证，输入验证码（避开第 3 步用过的那个时间片）后进入应用。
  await page.goto('/');
  await page.getByText(`${NAME} 的空间`).click();
  await page.getByRole('menuitem', { name: '退出登录' }).click();
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible();

  await page.getByLabel('用户名或邮箱', { exact: true }).fill(EMAIL);
  await page.getByLabel('密码', { exact: true }).fill(PASSWORD);
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByRole('heading', { name: '两步验证' })).toBeVisible();

  const loginTotp = await freshTotp(secret, setupTotp.step);
  await page.getByPlaceholder('000000').fill(loginTotp.code);
  await expect(page.getByRole('heading', { name: '两步验证' })).not.toBeVisible();
  await page.goto('/settings/profile');
  await expect(page.getByLabel('显示名称')).toHaveValue(NAME);

  // 5. 退出后输入错误密码：显示“再错 4 次”。
  await page.goto('/');
  await page.getByText(`${NAME} 的空间`).click();
  await page.getByRole('menuitem', { name: '退出登录' }).click();
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible();

  await page.getByLabel('用户名或邮箱', { exact: true }).fill(EMAIL);
  await page.getByLabel('密码', { exact: true }).fill('wrong-password');
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByText(/再错 4 次/)).toBeVisible();
});
