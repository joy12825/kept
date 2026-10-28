import { expect, test, type Page } from '@playwright/test';

// 阶段 1b 的验收：管理员分配账号，新建家庭空间并添加成员；只看的成员不能修改；
// 被移除后立即看不到该空间；活动记录里能看到添加、角色变更、移除。
// 本文件使用自己的一套全新实例（见 playwright.config.ts），所以从首次初始化开始。

const ADMIN = { name: '小王', email: 'wang@example.com', password: 'Passw0rd!23' };
const MEMBER = { username: 'xiaoli', name: '小李', email: 'xiaoli@example.com', password: 'Passw0rd!45' };

const switcher = (page: Page) => page.getByRole('button', { name: /^当前空间：/ });

test('家庭空间：添加已有账号、只看限制、改角色、移除、活动记录', async ({ page, browser }) => {
  test.setTimeout(120_000);

  // 1. A 首次初始化，然后在“账号管理”里为 B 创建账号（没有注册入口，账号只能由管理员分配）。
  await page.goto('/');
  await page.getByLabel('名字', { exact: true }).fill(ADMIN.name);
  await page.getByLabel('邮箱', { exact: true }).fill(ADMIN.email);
  await page.getByLabel('密码', { exact: true }).fill(ADMIN.password);
  await page.getByLabel('确认密码', { exact: true }).fill(ADMIN.password);
  await page.getByRole('button', { name: '创建并进入' }).click();
  await expect(switcher(page)).toBeVisible();

  await page.goto('/settings/accounts');
  await page.getByRole('button', { name: '新建账号' }).click();
  const create = page.getByRole('dialog');
  await create.getByLabel('邮箱').fill(MEMBER.email);
  await create.getByLabel('用户名').fill(MEMBER.username);
  await create.getByLabel('显示名称').fill(MEMBER.name);
  await create.getByLabel('初始密码').fill(MEMBER.password);
  await create.getByRole('button', { name: '创建' }).click();
  await expect(create).not.toBeVisible();

  // 2. A 新建家庭空间“王家”，按用户名把 B 添加为“只看”。
  await switcher(page).click();
  await page.getByRole('menuitem', { name: '新建空间' }).click();
  await page.getByRole('dialog').getByLabel('名称').fill('王家');
  await page.getByRole('dialog').getByRole('button', { name: '创建空间' }).click();
  await expect(switcher(page)).toHaveAccessibleName('当前空间：王家');

  await page.goto('/settings/members');
  await page.getByRole('button', { name: '添加成员' }).click();
  const add = page.getByRole('dialog');
  await add.getByLabel('用户名或邮箱').fill(MEMBER.username);
  await add.getByRole('combobox', { name: /角色/ }).click();
  await page.getByRole('option', { name: '只看' }).click();
  await add.getByRole('button', { name: '添加成员' }).click();
  await expect(page.getByText('家庭 · 2 位成员').first()).toBeVisible();

  // 3. B 在另一个浏览器里登录，切换到“王家”：只看的成员没有管理入口，也不能上传。
  const bContext = await browser.newContext();
  const b = await bContext.newPage();
  await b.goto('/');
  await expect(b.getByRole('heading', { name: '登录' })).toBeVisible();
  await expect(b.getByText(/注册/)).toHaveCount(0); // 7. 登录页没有注册入口
  await b.getByLabel('用户名或邮箱', { exact: true }).fill(MEMBER.email);
  await b.getByLabel('密码', { exact: true }).fill(MEMBER.password);
  await b.getByRole('button', { name: '登录' }).click();
  await switcher(b).click();
  await b.getByRole('menuitemradio', { name: /王家/ }).click();
  await expect(switcher(b)).toHaveAccessibleName('当前空间：王家');
  await expect(b.getByRole('button', { name: '上传' })).toBeDisabled();

  await b.goto('/settings/members');
  await expect(b.getByText(MEMBER.name).first()).toBeVisible();
  await expect(b.getByRole('button', { name: '添加成员' })).toHaveCount(0);
  await expect(b.getByRole('button', { name: '活动记录' })).toHaveCount(0);
  await b.goto('/settings/activity'); // 直接打开也看不到：回到个人资料
  await expect(b).toHaveURL(/\/settings\/profile$/);

  // 4. A 把 B 改为“成员”：B 刷新后可以上传。
  await page.getByRole('combobox', { name: `${MEMBER.name} 的角色` }).click();
  await page.getByRole('option', { name: '成员' }).click();
  await expect(page.getByText(`已将 ${MEMBER.name} 设为成员`)).toBeVisible();
  await b.reload();
  await expect(switcher(b)).toHaveAccessibleName('当前空间：王家');
  await expect(b.getByRole('button', { name: '上传' })).toBeEnabled();

  // 5. A 移除 B：B 刷新后回到个人空间，空间列表里没有“王家”。
  await page.getByRole('button', { name: `${MEMBER.name} 的更多操作` }).click();
  await page.getByRole('menuitem', { name: '移除' }).click();
  await page.getByRole('dialog').getByRole('button', { name: '移除' }).click();
  await expect(page.getByText('家庭 · 1 位成员').first()).toBeVisible();

  await b.reload();
  await expect(switcher(b)).not.toHaveAccessibleName('当前空间：王家');
  await switcher(b).click();
  await expect(b.getByRole('menuitemradio')).toHaveCount(1);
  await expect(b.getByRole('menuitemradio', { name: /王家/ })).toHaveCount(0);
  await bContext.close();

  // 6. A 的活动记录：从新到旧依次是移除、角色变更、添加成员、新建空间。
  await page.goto('/settings/activity');
  await expect(page.getByText(`${ADMIN.name} 移除了成员 ${MEMBER.name}`)).toBeVisible();
  const log = await page.locator('main').innerText();
  const order = [
    `${ADMIN.name} 移除了成员 ${MEMBER.name}`,
    `${ADMIN.name} 将 ${MEMBER.name} 的角色从 只看 改为 成员`,
    `${ADMIN.name} 添加了成员 ${MEMBER.name}（只看）`,
    `${ADMIN.name} 新建了家庭空间“王家”`,
  ].map((text) => log.indexOf(text));
  expect(order.every((at) => at >= 0), `活动记录缺少条目：${log}`).toBe(true);
  expect([...order].sort((x, y) => x - y)).toEqual(order);
});
