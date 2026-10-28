// 对照表的检查（v1.10.0）：源代码里每个 t('…') 都有英文、对照表里没有用不到的条目、占位符一致，
// 以及注释以外没有不经 t() 的中文（确实要中文的地方标 i18n-ignore）。
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { describe, expect, it } from 'vitest';
import { eeBuild } from '@ee';
import en from './en';
import { placeholders, scan } from './source';

const root = join(__dirname, '..');

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    // 对照表本身不查；开源构建（v1.13）不查收费模块
    if (statSync(p).isDirectory()) return name === 'test' || p === join(root, 'i18n', 'en') || p === join(root, 'ee', 'i18n') || (!eeBuild && p === join(root, 'ee')) ? [] : sources(p);
    if (!/\.(ts|tsx)$/.test(name) || /\.test\.tsx?$/.test(name) || name === 'schema.d.ts' || name.endsWith('.d.ts')) return [];
    return [p];
  });
}

// I18N_ONLY=pages/settings/storage,components：只查这些路径开头的文件（分头翻译时自查用），不查用不到的条目
const only = process.env.I18N_ONLY?.split(',').map((s) => s.trim()).filter(Boolean);
const files = sources(root).map((p) => ({ path: relative(root, p), found: scan(readFileSync(p, 'utf8')) }))
  .filter((f) => !only || only.some((o) => f.path.startsWith(o)));

describe('界面文字的英文对照表', () => {
  it('每个 t() 都有英文', () => {
    const missing = files.flatMap((f) => f.found.keys.filter((k) => !(k.key in en)).map((k) => `${f.path}:${k.line} ${k.key}`));
    expect(missing).toEqual([]);
  });

  it('t() 的键不拼接（用占位符）', () => {
    expect(files.flatMap((f) => f.found.dynamic.map((l) => `${f.path}:${l}`))).toEqual([]);
  });

  it.skipIf(!!only)('对照表里没有用不到的条目', () => {
    const used = new Set(files.flatMap((f) => f.found.keys.map((k) => k.key)));
    expect(Object.keys(en).filter((k) => !used.has(k))).toEqual([]);
  });

  it('英文和中文的占位符一致', () => {
    const wrong = Object.entries(en).filter(([zh, e]) => {
      const want = placeholders(zh.split('|')[0]).filter((p) => !(typeof e !== 'string' && p === 'n')).join(',');
      const forms = typeof e === 'string' ? [e] : [e.one, e.other];
      return forms.some((f) => placeholders(f).filter((p) => !(typeof e !== 'string' && p === 'n')).join(',') !== want);
    }).map(([zh]) => zh);
    expect(wrong).toEqual([]);
  });

  it('注释以外没有不经 t() 的中文', () => {
    const bare = files.flatMap((f) => f.found.bare.map((b) => `${f.path}:${b.line} ${b.text}`));
    expect(bare).toEqual([]);
  });
});
