import { describe, expect, it } from 'vitest';
import { pageHits, type TextRun } from './pdf-find';

// 100% 时页面 600 高：PDF 坐标原点在左下，换成屏幕坐标是 [1, 0, 0, -1, 0, 600]
const VP = [1, 0, 0, -1, 0, 600];
const run = (str: string, x: number, y: number, width: number, size = 10, hasEOL = false): TextRun => ({ str, transform: [size, 0, 0, size, x, y], width, hasEOL });

describe('PDF 一页里的查找结果', () => {
  it('按字数比例算出高亮位置（换成屏幕坐标）', () => {
    const hits = pageHits([run('本期电费合计', 100, 500, 60)], VP, ['电费'], 3);
    expect(hits).toHaveLength(1);
    expect(hits[0].page).toBe(3);
    const [x, y, w, h] = hits[0].rects[0];
    expect(x).toBeCloseTo(120); // 第 3 个字起，每个字 10
    expect(w).toBeCloseTo(20);
    expect(y).toBeCloseTo(100 - 8.5); // 基线在屏幕 y=100，往上约一个字高
    expect(h).toBeCloseTo(11);
  });

  it('跨过几段文字时每段一块，段与段之间的空格和换行不影响', () => {
    const hits = pageHits([run('国家', 0, 500, 20, 10, true), run(' ', 20, 500, 5), run('电网', 0, 480, 20)], VP, ['国家电网'], 1);
    expect(hits).toHaveLength(1);
    expect(hits[0].rects.map((r) => Math.round(r[1]))).toEqual([92, 112]);
  });

  it('多处结果按先后排列', () => {
    const hits = pageHits([run('电费 电费', 0, 500, 50)], VP, ['电费'], 1);
    expect(hits.map((h) => Math.round(h.rects[0][0]))).toEqual([0, 30]);
  });
});
