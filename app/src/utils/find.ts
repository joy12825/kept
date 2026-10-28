// 预览里查找文字（v1.5.4）：把查找框里的词在文字里找出来。不分大小写，也不管空白——识别出的中文常在字与字之间
// 夹着空格或换行（“电 费”），照样能找到“电费”。多个词用空格分开，任一个都标出。

export interface Match { start: number; end: number }

/** 查找框里的词：按空白分开，去掉引号；搜索语法里的排除词（-词）和字段条件（字段:值）不算。 */
export function findTerms(q: string): string[] {
  const out: string[] = [];
  for (const raw of q.split(/\s+/)) {
    const t = raw.replace(/["“”'‘’]/g, '');
    if (!t || t.startsWith('-') || /^[a-z_]+:/i.test(t)) continue;
    const k = t.toLowerCase();
    if (!out.includes(k)) out.push(k);
  }
  return out;
}

/** text 里所有出现的位置（原文的下标，end 不含），按先后排列、不重叠；长的词优先。 */
export function findMatches(text: string, terms: string[]): Match[] {
  if (!terms.length || !text) return [];
  // 去掉空白后的文字，并记下每个字在原文里的位置
  const pos: number[] = [];
  let flat = '';
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (/\s/.test(c)) continue;
    const l = c.toLowerCase();
    flat += l.length === 1 ? l : c; // 少数字母小写后变成两个字，按原样比，免得位置错开
    pos.push(i);
  }
  const sorted = [...terms].sort((a, b) => b.length - a.length);
  const out: Match[] = [];
  let i = 0;
  while (i < flat.length) {
    const t = sorted.find((w) => flat.startsWith(w, i));
    if (!t) { i++; continue; }
    out.push({ start: pos[i], end: pos[i + t.length - 1] + 1 });
    i += t.length;
  }
  return out;
}
