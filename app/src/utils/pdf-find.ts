// 预览里查找文字（v1.5.4）：在 PDF 一页的文字里找，并算出高亮的位置。不依赖 pdf.js，便于测试。
import { findMatches } from './find';

/** 两个 PDF 变换矩阵 [a b c d e f] 相乘（先 n 后 m），同 pdf.js 的 Util.transform。 */
function mul(m: number[], n: number[]): number[] {
  return [
    m[0] * n[0] + m[2] * n[1], m[1] * n[0] + m[3] * n[1],
    m[0] * n[2] + m[2] * n[3], m[1] * n[2] + m[3] * n[3],
    m[0] * n[4] + m[2] * n[5] + m[4], m[1] * n[4] + m[3] * n[5] + m[5],
  ];
}

/** 页面上的一块高亮：按 100% 的页面尺寸，x、y、宽、高。 */
export type Rect = [number, number, number, number];
/** 一处查找结果：所在页和它的高亮块（跨过几段文字时有几块）。 */
export interface Hit { page: number; rects: Rect[] }
export interface TextRun { str: string; transform: number[]; width: number; hasEOL: boolean }

/**
 * 一页里的查找结果。pdf.js 给出的文字是一段段的（识别出的字常一个词一段），连起来找，
 * 再按每段的位置和宽度、按字数比例算出高亮的范围；字宽不一时略有偏差，够看清是哪里。
 */
export function pageHits(runs: TextRun[], vp: number[], terms: string[], page: number): Hit[] {
  let text = '';
  const owner: number[] = [];
  const off: number[] = [];
  runs.forEach((it, j) => {
    for (let k = 0; k < it.str.length; k++) { text += it.str[k]; owner.push(j); off.push(k); }
    if (it.hasEOL) { text += '\n'; owner.push(-1); off.push(0); }
  });
  const scale = Math.hypot(vp[0], vp[1]);
  const rect = (j: number, a: number, b: number): Rect => {
    const it = runs[j];
    const tx = mul(vp, it.transform);
    const h = Math.hypot(tx[2], tx[3]);
    const w = it.width * scale;
    const len = it.str.length || 1;
    return [tx[4] + (w * a) / len, tx[5] - h * 0.85, Math.max((w * (b - a)) / len, 2), h * 1.1];
  };
  return findMatches(text, terms).map((m) => {
    const rects: Rect[] = [];
    let j = -1, a = 0, b = 0;
    for (let i = m.start; i < m.end; i++) {
      if (owner[i] < 0 || /\s/.test(text[i])) continue; // 段与段之间的空白不标
      if (owner[i] !== j) {
        if (j >= 0) rects.push(rect(j, a, b));
        j = owner[i]; a = off[i];
      }
      b = off[i] + 1;
    }
    if (j >= 0) rects.push(rect(j, a, b));
    return { page, rects };
  });
}
