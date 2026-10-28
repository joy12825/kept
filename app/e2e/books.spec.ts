import { expect, test } from '@playwright/test';
import { sampleEpub } from './epub';

// v0.4 的验收：上传 EPUB → 自动成为书籍；开启书房 → 封面墙；阅读器翻页、换底色，离开后再打开回到读到的地方。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
const book = sampleEpub('诗词小集', [['第一章 静夜思', '床前明月光，疑是地上霜。'], ['第二章 春望', '国破山河在，城春草木深。']]);

test('EPUB 入库成为书籍、书房、阅读器记住位置', async ({ page }) => {
  test.setTimeout(90_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 开启书房（新建抽屉对话框里）
  await page.getByRole('button', { name: '新建抽屉' }).click();
  await page.getByRole('button', { name: '开启书房' }).click();
  await expect(page.getByText(/已开启书房/)).toBeVisible();

  // 2. 上传 EPUB：标题用书名，自动成为书籍、进入书房
  await page.getByRole('button', { name: '上传', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '上传文档' });
  await dialog.getByLabel('上传文件').setInputFiles({ name: 'poems.epub', mimeType: 'application/epub+zip', buffer: book });
  await dialog.getByRole('button', { name: '上传 1 个文件' }).click();
  await expect(page.getByText('已进入待整理')).toBeVisible({ timeout: 30_000 });
  await page.getByRole('button', { name: '书房' }).click();
  await expect(page.getByText(/1 本 · 收纳：所有书籍/)).toBeVisible();
  const cover = page.getByRole('listitem', { name: '诗词小集' });
  await expect(cover).toBeVisible();

  // 3. 阅读器：第一章，翻到第二章，换成夜间
  await cover.hover();
  await page.getByRole('button', { name: '阅读“诗词小集”' }).click();
  const text = page.frameLocator('[data-testid="epub-area"] iframe');
  await expect(text.getByText('床前明月光，疑是地上霜。')).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: '目录' }).click();
  await expect(page.getByRole('menuitem', { name: '第二章 春望' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '下一页' }).click();
  await expect(text.getByText('国破山河在，城春草木深。')).toBeVisible();
  await page.getByRole('button', { name: '夜间' }).click();

  // 4. 返回书房，再打开：回到第二章
  await page.getByRole('button', { name: '返回' }).click();
  await expect(page.getByText(/1 本 · 收纳：所有书籍/)).toBeVisible();
  await cover.hover();
  await page.getByRole('button', { name: '阅读“诗词小集”' }).click();
  await expect(text.getByText('国破山河在，城春草木深。')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('button', { name: '夜间' })).toHaveAttribute('aria-pressed', 'true');

  // 5. 详情页的书目与我的阅读：打开过就是“在读”，改为读完后书房里有标记
  await page.keyboard.press('Escape');
  await cover.click();
  await expect(page.getByRole('region', { name: '书目' }).getByText('测试作者')).toBeVisible();
  const mine = page.getByRole('region', { name: '我的阅读' });
  await expect(mine.getByRole('button', { name: '在读' })).toHaveAttribute('aria-pressed', 'true');
  await mine.getByRole('button', { name: '读完' }).click();
  await expect(mine.getByRole('textbox', { name: '读完的日期' })).not.toHaveValue('');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: /^书房/ }).click();
  await expect(page.getByRole('listitem', { name: '诗词小集' }).getByLabel('读完')).toBeVisible();
});
