import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 5 的验收：上传两份文字层 PDF → 按正文里的词搜索只剩一份并有高亮 → 查询语法排除 → ⌘K 边输边搜打开文档。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。需要本机装有 poppler（pdfinfo 等）。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
const bill = textPdf(['ACME POWER COMPANY', 'Electricity bill for March', 'Issued 2024-03-05', 'Total due 128.50', 'Account 0012345678', 'Thank you for your payment']);
const lease = textPdf(['HOUSE LEASE AGREEMENT', 'Signed 2023-09-01', 'Landlord and tenant agree to the terms below', 'Monthly rent 6000', 'Deposit 12000', 'Term two years']);

test('全文搜索、查询语法与命令面板', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  await page.goto('/files');
  await page.getByRole('button', { name: '上传', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles([
    { name: 'Power bill.pdf', mimeType: 'application/pdf', buffer: bill },
    { name: 'Lease.pdf', mimeType: 'application/pdf', buffer: lease },
  ]);
  await dialog.getByRole('button', { name: '上传 2 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toHaveCount(2, { timeout: 30_000 });

  // 1. 正文里的词：只剩一份，片段里高亮（索引在后台建立，稍等片刻）
  const search = page.getByLabel('搜索文档');
  await search.fill('deposit');
  await expect(page.getByText('搜索结果 · 1 份')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('link', { name: /Lease/ })).toBeVisible();
  await expect(page.locator('mark', { hasText: /deposit/i })).toBeVisible();

  // 2. 查询语法：排除
  await search.fill('2024 -acme');
  await expect(page.getByText('没有符合条件的文档')).toBeVisible();
  await search.fill('"monthly rent"');
  await expect(page.getByText('搜索结果 · 1 份')).toBeVisible();

  // 3. ⌘K：输入标题的前几个字母，回车打开
  await search.fill('');
  await page.keyboard.press('Escape');
  await page.locator('body').press('ControlOrMeta+k');
  await page.getByRole('combobox', { name: '搜索与命令' }).fill('powe');
  await expect(page.getByRole('option', { name: /Power bill/ })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/docs\/\d+/);
  await expect(page.getByRole('heading', { name: 'Power bill' })).toBeVisible();
});
