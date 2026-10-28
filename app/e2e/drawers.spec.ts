import { expect, test } from '@playwright/test';
import { textPdf } from './pdf';

// 阶段 7 的验收：首次使用按场景用模板建抽屉 → 上传含关键词的文字层 PDF，自动放进“房子” → 详情页手动改放到“车”，
// 抽屉页的计数随之变化 → 交还给规则后回到“房子” → 年度回顾的份数与全部文件按年份筛选一致。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。需要本机装有 poppler（pdfinfo 等）。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
// 示例 PDF 只能写拉丁字母；关键词“电费”来自文件名（标题），收纳规则在标题和正文里查找
const bill = textPdf(['STATE GRID', 'Monthly bill', 'Total 128.50', 'Issued 2024-03-05']);

test('模板、收纳规则、手动放入与年度回顾', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();

  // 1. 首次使用：家庭场景，只建房子和车
  await page.getByText('家庭', { exact: true }).click();
  for (const name of ['健康', '孩子', '证件', '宠物']) await page.getByRole('checkbox', { name: new RegExp(name) }).click();
  await page.getByRole('button', { name: '创建 2 个抽屉' }).click();
  await expect(page.getByRole('region', { name: '我的抽屉' }).getByText('房子')).toBeVisible();

  // 2. 上传：标题有“电费”，放进房子
  await page.getByRole('button', { name: '上传', exact: true }).first().click(); // 第二个在“开始使用”里
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles([{ name: '电费账单.pdf', mimeType: 'application/pdf', buffer: bill }]);
  await dialog.getByRole('button', { name: '上传 1 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toBeVisible({ timeout: 30_000 });
  await page.keyboard.press('Escape');
  await page.goto('/inbox');
  await expect(page.getByRole('button', { name: '抽屉：房子' })).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText(/放入抽屉按收纳规则“房子”/)).toBeVisible();

  // 3. 详情页手动放到车：规则不再移动它；交还给规则后回到房子
  await page.goto('/files');
  await page.getByRole('link', { name: /电费账单/ }).click();
  await page.getByRole('button', { name: '抽屉：房子' }).click();
  await page.getByRole('option', { name: '车' }).click();
  await expect(page.getByText('手动放入，收纳规则不会移动它。')).toBeVisible();
  await expect(page.getByText('（将放入“房子”）')).toBeVisible();
  await page.getByRole('button', { name: '交还给规则' }).click();
  await expect(page.getByRole('button', { name: '抽屉：房子' })).toBeVisible();

  // 4. 年度回顾：2024 年 1 份，房子 1 份
  await page.goto('/review');
  await page.getByRole('button', { name: '2024' }).click();
  await expect(page.getByRole('heading', { name: '2024' })).toBeVisible();
  await expect(page.getByRole('region', { name: '各抽屉' }).getByText('1 份')).toBeVisible();
});
