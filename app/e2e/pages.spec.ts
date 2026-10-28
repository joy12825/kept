import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 12 的验收：两份 PDF 合并为一份（原来的移到回收站）→ 详情页删除一页后页数变为 1 → 恢复原样后又是 2 页。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。需要本机装有 poppler 和 qpdf。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };

test('合并文档、删除页面与恢复原样', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 上传两份一页的 PDF
  await page.goto('/files');
  await page.getByRole('button', { name: '上传', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles([
    { name: 'Lease part one.pdf', mimeType: 'application/pdf', buffer: textPdf(['HOUSE LEASE AGREEMENT', 'Part one of the lease, signed 2023-09-01']) },
    { name: 'Lease part two.pdf', mimeType: 'application/pdf', buffer: textPdf(['HOUSE LEASE AGREEMENT', 'Part two: deposit and monthly rent terms']) },
  ]);
  await dialog.getByRole('button', { name: '上传 2 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toHaveCount(2, { timeout: 30_000 });

  // 2. 全部文件里选中两份，合并为一份，原来的移到回收站
  await page.goto('/files');
  await page.getByRole('checkbox', { name: '选择“Lease part one”' }).check();
  await page.getByRole('checkbox', { name: '选择“Lease part two”' }).check();
  await page.getByRole('button', { name: '合并为一份' }).click();
  const merge = page.getByRole('dialog', { name: '合并为一份' });
  await merge.getByLabel('合并后把原来的文档移到回收站').check();
  await merge.getByRole('button', { name: '合并' }).click();
  await expect(page.getByText(/正在合并 2 份文档/)).toBeVisible();

  await expect(async () => {
    await page.goto('/settings/tasks');
    await expect(page.getByText('合并文档 · 2 份合并为一份')).toBeVisible({ timeout: 1000 });
    await expect(page.getByText('已完成').first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 30_000 });
  await page.goto('/files');
  await expect(page.getByRole('heading', { name: /全部文件 · 1 份/ })).toBeVisible();

  // 3. 打开合并后的文档，删除第 2 页
  await page.getByText('Lease part one').first().click();
  await page.getByRole('button', { name: '文件信息' }).click();
  const pages = page.locator('text=页数').locator('xpath=following-sibling::*[1]');
  await expect(pages).toHaveText('2');
  await page.getByRole('button', { name: '更多' }).click();
  await page.getByRole('menuitem', { name: '编辑页面' }).click();
  const editor = page.getByRole('dialog', { name: '编辑页面' });
  await editor.getByRole('button', { name: '删除第 2 页' }).click();
  await editor.getByRole('button', { name: '保存' }).click();
  await expect(page.getByText('页面已保存')).toBeVisible();
  await expect(pages).toHaveText('1');

  // 4. 恢复原样
  await page.getByRole('button', { name: '更多' }).click();
  await page.getByRole('menuitem', { name: '编辑页面' }).click();
  await editor.getByRole('button', { name: '恢复原样' }).click();
  await expect(page.getByText('已恢复原样')).toBeVisible();
  await expect(pages).toHaveText('2');
});
