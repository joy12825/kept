import { expect, test } from '@playwright/test';

// 阶段 9 的验收：设置 → 连接手机显示配对码 → 用配对码换到设备 token（模拟 App）→ 页面自动跳到已连接的设备 →
// 手机上传（带 clientUploadId，重复上传不重复入库）进入待整理，详情页注明“手机扫描” → 手机不能读文档 → 网页端断开后 token 失效。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };

test('连接手机、手机上传与断开', async ({ page, playwright, baseURL }) => {
  test.setTimeout(90_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 连接手机：显示二维码和配对码
  await page.goto('/settings/pair');
  await expect(page.getByRole('img', { name: '配对二维码' })).toBeVisible();
  const code = (await page.getByLabel('配对码').textContent())!.trim();
  expect(code).toMatch(/^\d{4} \d{4}$/);

  // 2. 模拟 App：用配对码换设备 token（与浏览器不共享 Cookie）
  const phone = await playwright.request.newContext({ baseURL });
  const paired = await phone.post('/api/v1/devices/pair', { data: { code, deviceName: '测试 iPhone' } });
  expect(paired.status()).toBe(200);
  const { token } = await paired.json();
  const auth = { Authorization: `Bearer ${token}` };

  // 3. 页面发现新设备，跳到已连接的设备
  await expect(page).toHaveURL(/\/settings\/devices$/, { timeout: 10_000 });
  await expect(page.getByRole('heading', { name: '测试 iPhone' })).toBeVisible();

  // 4. 手机上传两次（同一个 clientUploadId），只入库一份
  const upload = () => phone.post('/api/v1/documents', {
    headers: auth,
    multipart: { clientUploadId: 'scan-1', title: '手机扫描的收据', file: { name: 'scan.txt', mimeType: 'text/plain', buffer: Buffer.from('收据 2024-05-06 金额 88 元') } },
  });
  const first = await (await upload()).json();
  const again = await (await upload()).json();
  expect(again.jobId).toBe(first.jobId);
  await expect.poll(async () => {
    const r = await phone.post('/api/v1/uploads/status', { headers: auth, data: { clientUploadIds: ['scan-1'] } });
    return (await r.json())[0].status;
  }, { timeout: 30_000 }).toBe('succeeded');

  await page.goto('/inbox');
  await expect(page.getByRole('heading', { name: /待整理 · 1 份/ })).toBeVisible();
  await page.goto('/files');
  await page.getByText('手机扫描的收据').first().click();
  await page.getByRole('button', { name: '文件信息' }).click();
  await expect(page.getByText(/· 手机扫描$/)).toBeVisible();

  // 5. 手机不能读文档
  expect((await phone.get('/api/v1/documents', { headers: auth })).status()).toBe(403);

  // 6. 断开后 token 失效
  await page.goto('/settings/devices');
  await page.getByRole('button', { name: '断开' }).click();
  await page.getByRole('dialog').getByRole('button', { name: '断开' }).click();
  await expect(page.getByText('还没有连接任何手机')).toBeVisible();
  expect((await phone.get('/api/v1/mobile/bootstrap', { headers: auth })).status()).toBe(401);
  await phone.dispose();
});
