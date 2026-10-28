import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 4 的验收：待整理里设类型并完成整理 → 全部文件按类型筛选 → 详情页改标题、加备注 → 删除 → 回收站恢复 → 批量下载 zip。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。需要本机装有 poppler（pdfinfo 等）。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
const bill = textPdf(['ACME POWER COMPANY', 'Electricity bill for March', 'Issued 2024-03-05', 'Total due 128.50', 'Account 0012345678', 'Thank you for your payment']);
const lease = textPdf(['HOUSE LEASE AGREEMENT', 'Signed 2023-09-01', 'Landlord and tenant agree to the terms below', 'Monthly rent 6000', 'Deposit 12000', 'Term two years']);

test('整理、筛选、编辑、删除与恢复、批量下载', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 新建类型“账单”
  await page.goto('/directory?tab=document-types');
  await page.getByRole('button', { name: /新建类型/ }).first().click();
  const panel = page.getByRole('form', { name: '新建类型' });
  await panel.getByLabel('名称').fill('账单');
  await panel.getByRole('button', { name: '创建' }).click();
  await expect(panel).not.toBeVisible();

  // 2. 上传两份
  await page.goto('/inbox');
  await page.getByRole('button', { name: '上传', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles([
    { name: 'March bill.pdf', mimeType: 'application/pdf', buffer: bill },
    { name: 'Lease.pdf', mimeType: 'application/pdf', buffer: lease },
  ]);
  await dialog.getByRole('button', { name: '上传 2 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toHaveCount(2, { timeout: 30_000 });
  await expect(page.getByRole('heading', { name: /待整理 · 2 份/ })).toBeVisible();

  // 3. 待整理：给账单设类型，按 A 完成整理
  await page.getByRole('navigation', { name: '待整理队列' }).getByText('March bill').click();
  await page.getByRole('button', { name: /^类型：/ }).click();
  await page.getByRole('option', { name: '账单' }).click();
  await expect(page.getByRole('button', { name: '类型：账单' })).toBeVisible();
  await expect(page.locator('.MuiPopover-root')).toHaveCount(0); // 选择面板（含关闭动画）消失后快捷键才生效
  await page.locator('body').press('a');
  await expect(page.getByRole('heading', { name: /待整理 · 1 份/ })).toBeVisible();

  // 4. 全部文件按类型筛选只剩一份
  await page.goto('/files');
  await page.getByRole('button', { name: '类型' }).click();
  await page.getByRole('option', { name: /账单/ }).click();
  await page.keyboard.press('Escape');
  await expect(page.getByText('类型：账单')).toBeVisible();
  await expect(page.getByRole('link', { name: /March bill/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Lease/ })).toHaveCount(0);

  // 5. 详情页：改标题、加备注
  await page.getByRole('link', { name: /March bill/ }).click();
  await page.getByLabel('标题').fill('三月电费');
  await page.getByRole('button', { name: '保存' }).click();
  await expect(page.getByRole('heading', { name: '三月电费' })).toBeVisible();
  await page.getByLabel('添加备注').fill('已缴清');
  await page.getByRole('button', { name: '添加备注' }).click();
  await expect(page.getByText('已缴清')).toBeVisible();

  // 6. 删除 → 回收站恢复
  await page.getByRole('button', { name: '更多' }).click();
  await page.getByRole('menuitem', { name: '删除' }).click();
  await page.getByRole('dialog').getByRole('button', { name: '删除' }).click();
  await expect(page).toHaveURL(/\/files/);
  await page.goto('/trash');
  await expect(page.getByText('还剩 30 天自动清除')).toBeVisible();
  await page.getByRole('button', { name: '恢复' }).click();
  await expect(page.getByText('回收站是空的')).toBeVisible();

  // 7. 全选后批量下载 zip
  await page.goto('/files');
  await page.getByLabel('全选').check();
  await page.getByRole('toolbar', { name: '批量操作' }).getByRole('button', { name: '下载' }).click();
  const download = page.waitForEvent('download');
  await page.getByRole('menuitem', { name: /^原件/ }).click(); // 原件或可搜索版
  expect((await download).suggestedFilename()).toMatch(/^文档-\d{4}-\d{2}-\d{2}\.zip$/);
});
