import { describe, expect, it } from 'vitest';
import { findMatches, findTerms } from './find';

const marked = (text: string, q: string) => findMatches(text, findTerms(q)).map((m) => text.slice(m.start, m.end));

describe('预览里查找', () => {
  it('查找词：按空白分开，去掉引号、排除词和字段条件，不分大小写', () => {
    expect(findTerms(' 电费  "国家电网" -广告 type:账单 PDF pdf ')).toEqual(['电费', '国家电网', 'pdf']);
    expect(findTerms('')).toEqual([]);
  });

  it('不分大小写，跨过字之间的空格和换行', () => {
    expect(marked('本期电 费 326.40 元\n电\n费合计', '电费')).toEqual(['电 费', '电\n费']);
    expect(marked('Invoice INVOICE invoice', 'invoice')).toEqual(['Invoice', 'INVOICE', 'invoice']);
  });

  it('多个词都标出，按先后排列，长的词优先、不重叠', () => {
    expect(marked('国家电网 电费账单 电网', '电网 国家电网 电费')).toEqual(['国家电网', '电费', '电网']);
    expect(findMatches('aaaa', ['aa'])).toEqual([{ start: 0, end: 2 }, { start: 2, end: 4 }]);
  });

  it('没有词或没有文字时没有结果', () => {
    expect(findMatches('电费', [])).toEqual([]);
    expect(findMatches('', ['电费'])).toEqual([]);
  });
});
