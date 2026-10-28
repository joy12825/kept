// Markdown 解析（v1.9.1，自己写的）：只产出一棵语法树，由 markdown-view 画成 React 元素，从不生成 HTML。
// 支持：标题、段落、引用、有序 / 无序 / 任务列表（可嵌套）、代码块、表格、分隔线；
// 行内的代码、粗体、斜体、删除线、链接、图片（只显示为链接，不加载）、自动链接、换行。
// 原文里的 HTML 照字面显示。文字节点按出现顺序编号（id），查找时逐个标出。

export type Inline =
  | { t: 'text'; id: number; text: string }
  | { t: 'code'; id: number; text: string }
  | { t: 'strong' | 'em' | 'del'; children: Inline[] }
  | { t: 'link'; href: string | null; children: Inline[] }
  | { t: 'image'; href: string | null; alt: string; id: number }
  | { t: 'br' };

export type Align = 'left' | 'center' | 'right' | null;

export type Block =
  | { t: 'heading'; level: number; children: Inline[] }
  | { t: 'paragraph'; children: Inline[] }
  | { t: 'code'; lang: string; id: number; text: string }
  | { t: 'quote'; children: Block[] }
  | { t: 'list'; ordered: boolean; start: number; items: { checked: boolean | null; children: Block[] }[] }
  | { t: 'table'; align: Align[]; head: Inline[][]; rows: Inline[][][] }
  | { t: 'hr' };

export interface MarkdownDoc { blocks: Block[]; texts: string[] }

/** 链接只认 http、https、mailto；其余（javascript: 之类、相对地址）不做成链接。 */
export function safeHref(url: string): string | null {
  const u = url.trim();
  return /^(https?:\/\/|mailto:)/i.test(u) ? u : null;
}

export function parseMarkdown(src: string): MarkdownDoc {
  const texts: string[] = [];
  const text = (s: string): Inline => { texts.push(s); return { t: 'text', id: texts.length - 1, text: s }; };
  const lines = src.replace(/\r\n?/g, '\n').replace(/\t/g, '    ').split('\n');

  // ---------- 行内 ----------
  const inline = (s: string): Inline[] => {
    const out: Inline[] = [];
    let buf = '';
    const flush = () => { if (buf) { out.push(text(buf)); buf = ''; } };
    let i = 0;
    while (i < s.length) {
      const c = s[i];
      const rest = s.slice(i);
      if (c === '\\' && i + 1 < s.length && /[!-/:-@[-`{-~]/.test(s[i + 1])) { buf += s[i + 1]; i += 2; continue; }
      if (c === '\\' && i === s.length - 1) { flush(); out.push({ t: 'br' }); i++; continue; }
      if (c === '`') {
        const ticks = /^`+/.exec(rest)![0];
        const end = s.indexOf(ticks, i + ticks.length);
        if (end > 0) {
          flush();
          let code = s.slice(i + ticks.length, end);
          if (/^ .* $/.test(code)) code = code.slice(1, -1);
          texts.push(code);
          out.push({ t: 'code', id: texts.length - 1, text: code });
          i = end + ticks.length;
          continue;
        }
        buf += ticks; i += ticks.length; continue;
      }
      if (c === '!' && s[i + 1] === '[') {
        const m = linkAt(s, i + 1);
        if (m) {
          flush();
          texts.push(m.label);
          out.push({ t: 'image', href: safeHref(m.url), alt: m.label, id: texts.length - 1 });
          i = m.end; continue;
        }
      }
      if (c === '[') {
        const m = linkAt(s, i);
        if (m) { flush(); out.push({ t: 'link', href: safeHref(m.url), children: inline(m.label) }); i = m.end; continue; }
      }
      if (c === '<') {
        const m = /^<((?:https?:\/\/|mailto:)[^\s<>]+)>/i.exec(rest);
        if (m) { flush(); out.push({ t: 'link', href: safeHref(m[1]), children: [text(m[1])] }); i += m[0].length; continue; }
      }
      if ((c === 'h' || c === 'H') && /^https?:\/\//i.test(rest) && !/[\w/]/.test(s[i - 1] ?? '')) {
        let url = /^https?:\/\/[^\s<>]+/i.exec(rest)![0];
        url = url.replace(/[.,;:!?。，；：！？、）)\]'"”’]+$/, '');
        flush(); out.push({ t: 'link', href: safeHref(url), children: [text(url)] }); i += url.length; continue;
      }
      if (c === '~' && s[i + 1] === '~') {
        const end = closing(s, '~~', i + 2);
        if (end > 0) { flush(); out.push({ t: 'del', children: inline(s.slice(i + 2, end)) }); i = end + 2; continue; }
      }
      if ((c === '*' || c === '_') && s[i + 1] === c) {
        const d = c + c;
        const end = closing(s, d, i + 2);
        if (end > 0 && (c === '*' || !/\w/.test(s[i - 1] ?? ''))) { flush(); out.push({ t: 'strong', children: inline(s.slice(i + 2, end)) }); i = end + 2; continue; }
      }
      if (c === '*' || c === '_') {
        const end = closing(s, c, i + 1);
        // 下划线在词中间（snake_case）不算强调
        if (end > 0 && (c === '*' || (!/\w/.test(s[i - 1] ?? '') && !/\w/.test(s[end + 1] ?? '')))) {
          flush(); out.push({ t: 'em', children: inline(s.slice(i + 1, end)) }); i = end + 1; continue;
        }
      }
      buf += c; i++;
    }
    flush();
    return out;
  };

  // ---------- 块 ----------
  const blocks = (ls: string[]): Block[] => {
    const out: Block[] = [];
    let i = 0;
    let para: string[] = [];
    const endPara = () => {
      if (!para.length) return;
      const kids: Inline[] = [];
      para.forEach((l, k) => {
        const hard = / {2,}$/.test(l) || /\\$/.test(l);
        kids.push(...inline(l.replace(/ {2,}$/, '').trim()));
        if (k < para.length - 1) kids.push(hard ? { t: 'br' } : text('\n'));
      });
      out.push({ t: 'paragraph', children: kids });
      para = [];
    };
    while (i < ls.length) {
      const l = ls[i];
      if (!l.trim()) { endPara(); i++; continue; }
      const fence = /^ {0,3}(`{3,}|~{3,})\s*([\w+#.-]*)/.exec(l);
      if (fence) {
        endPara();
        const body: string[] = [];
        i++;
        while (i < ls.length && !ls[i].trim().startsWith(fence[1])) body.push(ls[i++]);
        i++;
        texts.push(body.join('\n'));
        out.push({ t: 'code', lang: fence[2], id: texts.length - 1, text: body.join('\n') });
        continue;
      }
      const h = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(l);
      if (h) { endPara(); out.push({ t: 'heading', level: h[1].length, children: inline(h[2]) }); i++; continue; }
      // 段落下一行是 === 或 ---：上一段是标题
      if (para.length && /^ {0,3}(=+|-+)\s*$/.test(l)) {
        const level = l.trim()[0] === '=' ? 1 : 2;
        const title = para.join(' ');
        para = [];
        out.push({ t: 'heading', level, children: inline(title.trim()) });
        i++; continue;
      }
      if (/^ {0,3}([-*_])(\s*\1){2,}\s*$/.test(l)) { endPara(); out.push({ t: 'hr' }); i++; continue; }
      if (/^ {0,3}>/.test(l)) {
        endPara();
        const body: string[] = [];
        while (i < ls.length && ls[i].trim() && /^ {0,3}>/.test(ls[i])) body.push(ls[i++].replace(/^ {0,3}> ?/, ''));
        out.push({ t: 'quote', children: blocks(body) });
        continue;
      }
      const item = listMarker(l);
      if (item) {
        endPara();
        const items: { checked: boolean | null; children: Block[] }[] = [];
        const ordered = item.ordered;
        while (i < ls.length) {
          const m = listMarker(ls[i]);
          if (!m || m.ordered !== ordered) break;
          const body = [ls[i].slice(m.width)];
          i++;
          // 续行：缩进到内容的位置，或空行后仍缩进
          while (i < ls.length) {
            const n = ls[i];
            if (!n.trim()) {
              if (i + 1 < ls.length && /^\s{2,}\S/.test(ls[i + 1]) && !listMarker(ls[i + 1])?.top) { body.push(''); i++; continue; }
              break;
            }
            if (listMarker(n) && indent(n) < m.width) break;
            if (indent(n) >= 2) { body.push(n.slice(Math.min(indent(n), m.width))); i++; continue; }
            if (!listMarker(n) && !/^ {0,3}(#|>|```|~~~)/.test(n)) { body.push(n); i++; continue; } // 懒续行
            break;
          }
          let checked: boolean | null = null;
          const task = /^\[([ xX])\]\s+/.exec(body[0]);
          if (task) { checked = task[1] !== ' '; body[0] = body[0].slice(task[0].length); }
          items.push({ checked, children: blocks(body) });
          while (i < ls.length && !ls[i].trim() && listMarker(ls[i + 1] ?? '')?.ordered === ordered) i++;
        }
        out.push({ t: 'list', ordered, start: item.start, items });
        continue;
      }
      // 表格：一行竖线，下一行是 --- 分隔
      if (l.includes('|') && i + 1 < ls.length && /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/.test(ls[i + 1])) {
        endPara();
        const head = cells(l);
        const align = cells(ls[i + 1]).map((c): Align => (c.startsWith(':') && c.endsWith(':') ? 'center' : c.endsWith(':') ? 'right' : c.startsWith(':') ? 'left' : null));
        i += 2;
        const rows: Inline[][][] = [];
        while (i < ls.length && ls[i].trim() && ls[i].includes('|')) {
          const r = cells(ls[i++]);
          rows.push(head.map((_, k) => inline(r[k] ?? '')));
        }
        out.push({ t: 'table', align, head: head.map(inline), rows });
        continue;
      }
      para.push(l);
      i++;
    }
    endPara();
    return out;
  };

  return { blocks: blocks(lines), texts };
}

const indent = (l: string) => /^ */.exec(l)![0].length;

/** 列表项的标记：- * + 或 1. 1)；width 是内容开始的列 */
function listMarker(l: string): { ordered: boolean; start: number; width: number; top: boolean } | null {
  const m = /^( {0,3})([-*+]|(\d{1,9})[.)])( +|$)/.exec(l);
  if (!m) return null;
  return { ordered: !!m[3], start: m[3] ? Number(m[3]) : 1, width: m[0].length, top: m[1].length === 0 };
}

/** 表格一行的各格（去掉两头的竖线，\| 是格子里的竖线） */
function cells(l: string): string[] {
  let s = l.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);
  return s.split(/(?<!\\)\|/).map((c) => c.trim().replace(/\\\|/g, '|'));
}

/** s 里从 from 起，不紧跟空白的收尾记号的位置 */
function closing(s: string, d: string, from: number): number {
  if (/\s/.test(s[from] ?? ' ')) return -1;
  for (let j = from + 1; j <= s.length - d.length; j++) {
    if (s.startsWith(d, j) && !/\s/.test(s[j - 1]) && !(d.length === 1 && s[j + 1] === d)) return j;
    if (s[j] === '`') { const e = s.indexOf('`', j + 1); if (e > 0) j = e; }
  }
  return -1;
}

/** [文字](地址 "标题")，返回文字、地址和结束位置 */
function linkAt(s: string, at: number): { label: string; url: string; end: number } | null {
  let depth = 0;
  let j = at;
  for (; j < s.length; j++) {
    if (s[j] === '\\') { j++; continue; }
    if (s[j] === '[') depth++;
    else if (s[j] === ']' && --depth === 0) break;
  }
  if (j >= s.length || s[j + 1] !== '(') return null;
  const m = /^\(\s*<?([^\s()<>]*(?:\([^\s()]*\)[^\s()<>]*)*)>?(?:\s+(?:"[^"]*"|'[^']*'))?\s*\)/.exec(s.slice(j + 1));
  if (!m) return null;
  return { label: s.slice(at + 1, j), url: m[1], end: j + 1 + m[0].length };
}
