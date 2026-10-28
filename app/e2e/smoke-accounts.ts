// 冒烟测试（v1.9.11）的两个账号：只在测试的临时实例里，由 playwright.config.ts 用 kept user create 建好。
// 爸爸（dad）是实例管理员，演示数据装在他名下；妈妈（mum）由测试加进“王家”，用来确认看不到爸爸的私密文档。
export const SMOKE_PASSWORD = 'smoke-test-only-Pw1';
export const DAD = { login: 'dad', email: 'dad@example.test' };
export const MUM = { login: 'mum', email: 'mum@example.test' };
