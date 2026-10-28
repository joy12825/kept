// 表格的排序和筛选（v1.11.4）：照表格软件的“自动筛选”，在网页里算，不改原件。
// 标题行和它上面的行固定不动；数字、金额、日期按数值比，其他按中文排序，空白总在最后。

export interface ColumnSort { col: number; dir: 'asc' | 'desc' }

export interface SheetViewState {
  /** 标题行（从 0 数）；-1 表示没有标题行 */
  header: number;
  sort: ColumnSort | null;
  /** 各列只显示这些值（没有的列不筛） */
  filters: Record<number, string[]>;
  query: string;
}

/** 数字：去掉千分位、货币符号和空格；百分数按百分比。不是数字时为 null */
export function cellNumber(v: string): number | null {
  const s = v.replace(/[,，\s¥￥$€£]/g, '').replace(/^RMB/i, '');
  const m = /^([-+]?)(\d+(?:\.\d+)?|\.\d+)(%?)$/.exec(s);
  if (!m) return null;
  const n = Number(m[1] + m[2]);
  return m[3] ? n / 100 : n;
}

/** 日期：2024-03-05、2024/3/5、2024年3月5日、2024.3.5（后面可以带时间）；不是日期时为 null */
export function cellDate(v: string): number | null {
  // i18n-ignore-next：日期里的“年月日”
  const m = /^\s*(\d{4})[-/.年](\d{1,2})[-/.月](\d{1,2})日?(?:[\sT]+(\d{1,2}):(\d{2})(?::(\d{2}))?)?\s*$/.exec(v);
  if (!m) return null;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  if (mo < 1 || mo > 12 || d < 1 || d > 31) return null;
  return Date.UTC(y, mo - 1, d, Number(m[4] ?? 0), Number(m[5] ?? 0), Number(m[6] ?? 0));
}

const collator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' });

/** 两格的先后：都是日期按日期，都是数字按数值，否则按文字；空白总在最后（不管升降序，由调用方处理） */
export function compareCells(a: string, b: string): number {
  const da = cellDate(a), db = cellDate(b);
  if (da !== null && db !== null) return da - db;
  const na = cellNumber(a), nb = cellNumber(b);
  if (na !== null && nb !== null) return na - nb;
  if (na !== null) return -1; // 数字排在文字前
  if (nb !== null) return 1;
  return collator.compare(a, b);
}

const blank = (v: string | undefined) => (v ?? '').trim() === '';

/** 前 5 行里第一行有两格以上有字、又不是横跨大半张表的合并标题的那一行；找不到为 -1 */
export function findHeader(rows: string[][], merges: number[][], cols: number): number {
  for (let r = 0; r < Math.min(5, rows.length - 1); r++) {
    const filled = (rows[r] ?? []).filter((v) => !blank(v)).length;
    const wide = merges.some(([r1, c1, r2, c2]) => r1 <= r && r <= r2 && c2 - c1 + 1 >= Math.max(2, cols / 2));
    if (filled >= 2 && !wide) return r;
  }
  return -1;
}

// i18n-ignore-next：认合计行用的中文
const TOTAL = /^(合计|总计|小计|共计|总和|累计|total|totals|sum|subtotal)[:：]?$/i;

/** 表末尾的合计行（最多 3 行）：有一格是“合计”“总计”之类的 */
export function footerRows(rows: string[][], header: number): number {
  let n = 0;
  for (let r = rows.length - 1; r > header + 1 && n < 3; r--) {
    if (!rows[r].some((c) => TOTAL.test(c.trim()))) break;
    n++;
  }
  return n;
}

/** 要显示的行（原来的行号，从 0 数）：先是固定的（标题行和它上面的），再是排好、筛过的数据行，
 * 最后是表末尾的合计行（不参加排序和筛选） */
export function viewRows(rows: string[][], v: SheetViewState): { fixed: number[]; data: number[]; footer: number[]; total: number } {
  const fixed = Array.from({ length: Math.max(0, v.header + 1) }, (_, i) => i);
  const nFoot = footerRows(rows, v.header);
  const footer = Array.from({ length: nFoot }, (_, i) => rows.length - nFoot + i);
  let data = Array.from({ length: Math.max(0, rows.length - fixed.length - nFoot) }, (_, i) => i + fixed.length);
  const total = data.length;
  const q = v.query.trim().toLowerCase();
  if (q) data = data.filter((r) => rows[r].some((c) => c.toLowerCase().includes(q)));
  for (const [col, allowed] of Object.entries(v.filters)) {
    const set = new Set(allowed);
    data = data.filter((r) => set.has(rows[r][Number(col)] ?? ''));
  }
  if (v.sort) {
    const { col, dir } = v.sort;
    const sign = dir === 'asc' ? 1 : -1;
    data = [...data].sort((x, y) => {
      const a = rows[x][col] ?? '', b = rows[y][col] ?? '';
      if (blank(a) || blank(b)) return blank(a) === blank(b) ? x - y : blank(a) ? 1 : -1;
      return sign * compareCells(a, b) || x - y; // 一样时保持原来的顺序
    });
  }
  return { fixed, data, footer, total };
}

/** 一列里出现过的值和次数（数据行），按排序规则排好 */
export function columnValues(rows: string[][], header: number, col: number): { value: string; count: number }[] {
  const counts = new Map<string, number>();
  for (let r = header + 1; r < rows.length - footerRows(rows, header); r++) {
    const v = rows[r][col] ?? '';
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([value, count]) => ({ value, count }))
    .sort((a, b) => (blank(a.value) ? 1 : blank(b.value) ? -1 : compareCells(a.value, b.value)));
}

export const isActive = (v: SheetViewState) => v.sort !== null || v.query.trim() !== '' || Object.keys(v.filters).length > 0;
