import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 6 的验收：在目录里新建带匹配规则的往来方并“试一试” → 上传含关键词的文字层 PDF →
// 待整理里往来方已自动填好并打勾、给出依据 → 详情页“分类依据”列出规则。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。需要本机装有 poppler（pdfinfo 等）。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
const bill = textPdf(['ACME POWER COMPANY', 'Electricity bill for March', 'Issued 2024-03-05', 'Total due 128.50']);

test('匹配规则在入库时生效并记下依据', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 目录：新建往来方，匹配方式“任意词”，先试一试再创建
  await page.goto('/directory');
  await page.getByRole('button', { name: '新建往来方' }).click();
  const panel = page.getByRole('form', { name: '新建往来方' });
  await panel.getByLabel('名称').fill('ACME 电力');
  await panel.getByRole('combobox', { name: /匹配方式/ }).click();
  await page.getByRole('option', { name: '任意词' }).click();
  await panel.getByLabel(/关键词/).fill('ACME');
  await panel.getByLabel('试一试').fill('acme power company, bill for March');
  await expect(panel.getByText('匹配：关键词“ACME”')).toBeVisible();
  await panel.getByRole('button', { name: '创建' }).click();
  await expect(panel).toBeHidden();

  // 2. 上传：往来方由规则填好
  await page.goto('/inbox');
  await page.getByRole('button', { name: '上传', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles([{ name: 'Power bill.pdf', mimeType: 'application/pdf', buffer: bill }]);
  await dialog.getByRole('button', { name: '上传 1 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toBeVisible({ timeout: 30_000 });
  await page.keyboard.press('Escape');

  // 3. 待整理：对勾和依据
  await expect(page.getByLabel('往来方：ACME 电力')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('img', { name: '自动填入：匹配规则：关键词“ACME”' })).toBeVisible();
  await expect(page.getByRole('img', { name: '自动填入：从正文识别：“Issued 2024-03-05”' })).toBeVisible();
  await expect(page.getByText('依据：匹配规则：往来方（关键词“ACME”）；日期从正文识别')).toBeVisible();

  // 4. 详情页：分类依据
  await page.goto('/files');
  await page.getByRole('link', { name: /Power bill/ }).click();
  await page.getByRole('button', { name: '分类依据' }).click();
  await expect(page.getByText('匹配规则：关键词“ACME”')).toBeVisible();
});
