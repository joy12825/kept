import { describe, expect, it } from 'vitest';
import { diff, diffStats, diffText, foldSame } from './diff';

// 由编辑序列还原出 b，检查它是一个正确的编辑脚本
const apply = <T,>(a: T[], b: T[]) => diff(a, b).filter((o) => o.t !== 'del').map((o) => (o.t === 'same' ? a[o.a!] : b[o.b!]));

describe('diff', () => {
  it('编辑序列能把 a 变成 b，而且最短', () => {
    const cases: [string, string][] = [['abcabba', 'cbabac'], ['', 'abc'], ['abc', ''], ['abc', 'abc'], ['kitten', 'sitting'], ['甲方乙方房租三千', '甲方乙方房租三千二']];
    for (const [x, y] of cases) {
      const a = Array.from(x), b = Array.from(y);
      expect(apply(a, b).join('')).toBe(y);
      expect(diff(a, b).filter((o) => o.t === 'same').map((o) => a[o.a!]).join('')).toBe(diff(a, b).filter((o) => o.t === 'same').map((o) => b[o.b!]).join(''));
    }
    // abcabba → cbabac 的最短编辑是 5 步
    expect(diff(Array.from('abcabba'), Array.from('cbabac')).filter((o) => o.t !== 'same')).toHaveLength(5);
  });

  it('逐行对比：改动的行逐字标出', () => {
    const rows = diffText('租房合同\n房租 3000 元\n押一付三\n', '租房合同\n房租 3200 元\n押一付三\n水电自理\n');
    expect(rows.map((r) => r.t)).toEqual(['same', 'del', 'add', 'same', 'add', 'same']);
    const del = rows[1] as Extract<(typeof rows)[number], { t: 'del' }>;
    const add = rows[2] as Extract<(typeof rows)[number], { t: 'add' }>;
    expect(del.segs!.filter((s) => s.changed).map((s) => s.text).join('')).toBe('0');
    expect(add.segs!.filter((s) => s.changed).map((s) => s.text).join('')).toBe('2');
    expect(diffStats(rows)).toEqual({ added: 2, removed: 1 });
  });

  it('多余的空白不算改动', () => {
    expect(diffText('电  费 128 元', '电 费 128 元 ').every((r) => r.t === 'same')).toBe(true);
  });

  it('长段没改的内容折叠，改动前后留几行', () => {
    const a = Array.from({ length: 40 }, (_, i) => `第 ${i} 行`);
    const b = [...a];
    b[20] = '改了';
    const blocks = foldSame(diffText(a.join('\n'), b.join('\n')));
    expect(blocks.map((x) => x.t)).toEqual(['fold', 'rows', 'rows', 'rows', 'fold']);
    expect(blocks[0].rows).toHaveLength(17);
    expect(blocks[1].rows).toHaveLength(3);
  });

  it('很大的差异也能算完', () => {
    const a = Array.from({ length: 3000 }, (_, i) => `a${i}`);
    const b = Array.from({ length: 3000 }, (_, i) => `b${i}`);
    const rows = diffText(a.join('\n'), b.join('\n'));
    expect(diffStats(rows)).toEqual({ added: 3000, removed: 3000 });
  });
});
