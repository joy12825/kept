// 文字差异（v1.9.2 版本对比，自己写的）：Myers 算法先逐行比，再把相邻的删去和增加配成“改动”，逐字标出改了哪里。

export type Op = { t: 'same' | 'del' | 'add'; a?: number; b?: number };

/** 差异太大时不再细算：中间那段当成全删全加（每一步只留用得到的那段，内存约为它的平方） */
const MAX_EDITS = 1500;

/** 两列内容的最短编辑序列（Myers），返回每一步：same（两边都有）、del（只在 a）、add（只在 b）。 */
export function diff<T>(a: readonly T[], b: readonly T[], eq: (x: T, y: T) => boolean = Object.is): Op[] {
  // 去掉相同的开头和结尾，中间才需要算
  let start = 0;
  while (start < a.length && start < b.length && eq(a[start], b[start])) start++;
  let endA = a.length, endB = b.length;
  while (endA > start && endB > start && eq(a[endA - 1], b[endB - 1])) { endA--; endB--; }
  const head: Op[] = Array.from({ length: start }, (_, i) => ({ t: 'same', a: i, b: i }));
  const tail: Op[] = Array.from({ length: a.length - endA }, (_, i) => ({ t: 'same', a: endA + i, b: endB + i }));
  const n = endA - start, m = endB - start;
  const middle = (): Op[] => {
    const out: Op[] = [];
    for (let i = 0; i < n; i++) out.push({ t: 'del', a: start + i });
    for (let j = 0; j < m; j++) out.push({ t: 'add', b: start + j });
    return out;
  };
  if (n === 0 || m === 0) return [...head, ...middle(), ...tail];

  const max = Math.min(n + m, MAX_EDITS);
  const off = max + 1;
  const v = new Int32Array(2 * max + 3);
  // 第 d 步开始时 v 里 k 在 -d-1…d+1 的那段，倒推时只用到它们
  const trace: Int32Array[] = [];
  let found = -1;
  for (let d = 0; d <= max && found < 0; d++) {
    trace.push(v.slice(off - d - 1, off + d + 2));
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && eq(a[start + x], b[start + y])) { x++; y++; }
      v[off + k] = x;
      if (x >= n && y >= m) { found = d; break; }
    }
  }
  if (found < 0) return [...head, ...middle(), ...tail];

  // 倒着走回去
  const ops: Op[] = [];
  let x = n, y = m;
  for (let d = found; d > 0; d--) {
    const w = trace[d];
    const pv = (k: number) => w[k + d + 1];
    const k = x - y;
    const prevK = k === -d || (k !== d && pv(k - 1) < pv(k + 1)) ? k + 1 : k - 1;
    const px = pv(prevK), py = px - prevK;
    while (x > px && y > py) { x--; y--; ops.push({ t: 'same', a: start + x, b: start + y }); }
    if (x === px) { y--; ops.push({ t: 'add', b: start + y }); } else { x--; ops.push({ t: 'del', a: start + x }); }
  }
  while (x > 0 && y > 0) { x--; y--; ops.push({ t: 'same', a: start + x, b: start + y }); }
  ops.reverse();
  return [...head, ...ops, ...tail];
}

/** 一行里的一段：changed 为这一段是改过的字 */
export interface Seg { text: string; changed: boolean }

export type Row =
  | { t: 'same'; a: number; b: number; text: string }
  | { t: 'del'; a: number; text: string; segs?: Seg[] }
  | { t: 'add'; b: number; text: string; segs?: Seg[] };

/** 逐字比两行，各自标出改过的字 */
function charSegs(x: string, y: string): [Seg[], Seg[]] {
  const a = Array.from(x), b = Array.from(y);
  const ops = diff(a, b);
  const sa: Seg[] = [], sb: Seg[] = [];
  const push = (out: Seg[], ch: string, changed: boolean) => {
    const last = out[out.length - 1];
    if (last && last.changed === changed) last.text += ch; else out.push({ text: ch, changed });
  };
  for (const o of ops) {
    if (o.t === 'same') { push(sa, a[o.a!], false); push(sb, b[o.b!], false); }
    else if (o.t === 'del') push(sa, a[o.a!], true);
    else push(sb, b[o.b!], true);
  }
  return [sa, sb];
}

/** 两段文字逐行对比的结果；相邻的删去和增加一一配对，逐字标出改动（行太长时不细标）。 */
export function diffText(oldText: string, newText: string): Row[] {
  const a = oldText.replace(/\r\n?/g, '\n').split('\n');
  const b = newText.replace(/\r\n?/g, '\n').split('\n');
  const norm = (s: string) => s.replace(/\s+/g, ' ').trim(); // 识别出的文字常有多余的空白
  const ops = diff(a.map(norm), b.map(norm));
  const rows: Row[] = [];
  for (let i = 0; i < ops.length;) {
    const o = ops[i];
    if (o.t === 'same') { rows.push({ t: 'same', a: o.a!, b: o.b!, text: b[o.b!] }); i++; continue; }
    const dels: number[] = [], adds: number[] = [];
    while (i < ops.length && ops[i].t !== 'same') {
      if (ops[i].t === 'del') dels.push(ops[i].a!); else adds.push(ops[i].b!);
      i++;
    }
    const pairs = Math.min(dels.length, adds.length);
    const delRows: Row[] = dels.map((n) => ({ t: 'del', a: n, text: a[n] }));
    const addRows: Row[] = adds.map((n) => ({ t: 'add', b: n, text: b[n] }));
    for (let p = 0; p < pairs; p++) {
      const x = a[dels[p]], y = b[adds[p]];
      if (x.length + y.length <= 2000) {
        const [sa, sb] = charSegs(x, y);
        (delRows[p] as Extract<Row, { t: 'del' }>).segs = sa;
        (addRows[p] as Extract<Row, { t: 'add' }>).segs = sb;
      }
    }
    rows.push(...delRows, ...addRows);
  }
  return rows;
}

/** 统计：增加、删去的行数 */
export function diffStats(rows: Row[]) {
  return { added: rows.filter((r) => r.t === 'add').length, removed: rows.filter((r) => r.t === 'del').length };
}

export type Block = { t: 'rows'; rows: Row[] } | { t: 'fold'; rows: Row[] };

/** 把连续很长的没改动的行折叠起来，改动前后各留 context 行。 */
export function foldSame(rows: Row[], context = 3): Block[] {
  const out: Block[] = [];
  let i = 0;
  while (i < rows.length) {
    if (rows[i].t !== 'same') {
      const start = i;
      while (i < rows.length && rows[i].t !== 'same') i++;
      out.push({ t: 'rows', rows: rows.slice(start, i) });
      continue;
    }
    const start = i;
    while (i < rows.length && rows[i].t === 'same') i++;
    const run = rows.slice(start, i);
    const keepHead = start === 0 ? 0 : context;
    const keepTail = i === rows.length ? 0 : context;
    if (run.length <= keepHead + keepTail + 2) { out.push({ t: 'rows', rows: run }); continue; }
    if (keepHead) out.push({ t: 'rows', rows: run.slice(0, keepHead) });
    out.push({ t: 'fold', rows: run.slice(keepHead, run.length - keepTail) });
    if (keepTail) out.push({ t: 'rows', rows: run.slice(run.length - keepTail) });
  }
  return out;
}
