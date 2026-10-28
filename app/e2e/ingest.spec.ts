import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 3 的验收：上传一份带文字层的 PDF → 任务托盘显示进度直到进入待整理 → 待整理里能预览、
// 识别出日期、看到文字；同一份文件再传一次提示重复。需要本机装有 poppler（pdfinfo 等）。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };

const bill = textPdf(['ACME POWER COMPANY', 'Electricity bill for March', 'Issued 2024-03-05', 'Total due 128.50', 'Account 0012345678', 'Thank you for your payment']);

test('上传 → 任务托盘 → 待整理预览；重复文件', async ({ page }) => {
  test.setTimeout(90_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 空的待整理
  await page.goto('/inbox');
  await expect(page.getByText('没有待整理的文档')).toBeVisible();

  // 2. 上传：托盘里从上传到“已进入待整理”，待整理数量随事件流更新
  const upload = async () => {
    await page.getByRole('button', { name: '上传', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '上传文档' });
    await dialog.getByLabel('上传文件').setInputFiles({ name: 'March bill.pdf', mimeType: 'application/pdf', buffer: bill });
    await dialog.getByRole('button', { name: '上传 1 个文件' }).click();
    await expect(dialog).not.toBeVisible();
  };
  await upload();
  await expect(page.getByText('已进入待整理')).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole('heading', { name: /待整理 · 1 份/ })).toBeVisible();

  // 3. 点击托盘里的那一行：待整理定位到它，pdf.js 画出第一页，日期从文字里识别出来
  await page.getByText('March bill.pdf').click();
  await expect(page).toHaveURL(/\/inbox\?doc=\d+/);
  await expect(page.getByLabel('标题')).toHaveValue('March bill');
  await expect(page.getByLabel('第 1 页')).toBeVisible();
  await expect(page.getByLabel('日期')).toHaveValue('2024-03-05');
  await page.getByRole('button', { name: '识别文字' }).click();
  await expect(page.getByText(/ACME POWER COMPANY/)).toBeVisible();

  // 4. 同一份文件再传一次：先问同名的要不要作为新版本（v1.7.13），作为新文档上传时提示重复
  await upload();
  const same = page.getByRole('dialog', { name: '有同名的文档' });
  await same.getByLabel('全部作为新版本').uncheck();
  await same.getByRole('button', { name: '上传' }).click();
  await expect(page.getByText('这份文件已经存在，点击查看已有的文档')).toBeVisible();
  await expect(page.getByRole('heading', { name: /待整理 · 1 份/ })).toBeVisible();
});
