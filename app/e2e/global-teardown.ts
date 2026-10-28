import { rm } from 'node:fs/promises';
import { E2E_TMP } from '../playwright.config';

/** 测试结束后删除各套后端的程序和数据目录（见 playwright.config.ts）。中途打断时留下的，下次启动前会清空。 */
export default async function globalTeardown() {
  await rm(E2E_TMP, { recursive: true, force: true });
}
