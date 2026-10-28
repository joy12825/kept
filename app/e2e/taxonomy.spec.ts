import { expect, test } from '@playwright/test';

// 阶段 2 的验收：目录页的往来方按拼音分组、标签层级与删除上级、内置字段不能删除、产品名称。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };

test('目录：新建往来方与标签层级、内置字段、产品名称', async ({ page }) => {
  test.setTimeout(90_000);
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(page.getByRole('button', { name: /^当前空间：/ })).toBeVisible();

  // 1. 往来方：按拼音首字母分组，英文开头归入 #
  await page.goto('/directory');
  for (const name of ['国家电网', 'Apple']) {
    await page.getByRole('button', { name: /新建往来方/ }).first().click();
    const panel = page.getByRole('form', { name: '新建往来方' });
    await panel.getByLabel('名称').fill(name);
    await panel.getByRole('button', { name: '创建' }).click();
    await expect(panel).not.toBeVisible();
  }
  await expect(page.locator('#L-G')).toContainText('国家电网');
  await expect(page.locator('#L-\\#')).toContainText('Apple');

  // 2. 标签：财务 > 发票 > 专票；删除“发票”后“专票”挂到“财务”下
  await page.getByRole('button', { name: '标签', exact: true }).click();
  const createTag = async (name: string, parent?: string) => {
    await page.getByRole('button', { name: /新建标签/ }).first().click();
    const panel = page.getByRole('form', { name: '新建标签' });
    await panel.getByLabel('名称').fill(name);
    if (parent) {
      await panel.getByRole('combobox', { name: /上级标签/ }).click();
      await page.getByRole('option', { name: parent, exact: true }).click();
    }
    await panel.getByRole('button', { name: '创建' }).click();
    await expect(panel).not.toBeVisible();
  };
  await createTag('财务');
  await createTag('发票', '财务');
  await createTag('专票', '财务 / 发票');
  // 点一行进入条目页，在那里“编辑”
  await page.getByText('发票', { exact: true }).click();
  await page.getByRole('button', { name: '编辑' }).click();
  const edit = page.getByRole('form', { name: '编辑标签' });
  await edit.getByRole('button', { name: '删除' }).click();
  const confirm = page.getByRole('dialog', { name: /删除标签“发票”/ });
  await expect(confirm).toContainText('它的 1 个子标签会上移一层');
  await confirm.getByRole('button', { name: '删除' }).click();
  await expect(confirm).not.toBeVisible();
  await page.goto('/directory');
  await page.getByRole('button', { name: '标签', exact: true }).click();
  await page.getByRole('textbox', { name: '查找' }).fill('专票');
  await expect(page.getByText('财务 / 专票')).toBeVisible();

  // 3. 内置字段：有“内置”标记，编辑面板里没有删除
  await page.getByRole('button', { name: '自定义字段', exact: true }).click();
  await expect(page.getByText('内置')).toHaveCount(2);
  // 字段没有条目页，点一行直接打开编辑面板
  await page.getByText('金额', { exact: true }).click();
  await expect(page.getByRole('form', { name: '编辑自定义字段' })).toBeVisible();
  await expect(page.getByRole('form', { name: '编辑自定义字段' }).getByRole('button', { name: '删除' })).toHaveCount(0);
  await page.getByRole('button', { name: '关闭' }).click();

  // 4. 产品名称：改名后登录页显示新名称
  await page.goto('/settings/brand');
  await page.getByLabel('产品名称').fill('我家档案');
  await page.getByRole('button', { name: '保存' }).click();
  await expect(page).toHaveTitle('我家档案');
  await page.getByRole('button', { name: /^当前空间：/ }).click();
  await page.getByRole('menuitem', { name: '退出登录' }).click();
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible();
  await expect(page.getByText('我家档案').first()).toBeVisible();
});
