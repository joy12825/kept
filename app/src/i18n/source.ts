// 读源代码找界面文字（v1.10.0，只给 catalog.test.ts 用）：t('…') 的键，以及没有经过 t() 的中文。
// 一个够用的小词法器：分出注释、字符串、模板字符串和正则，不求完整。

export interface Found {
  /** t('…') 的第一个参数 */
  keys: { key: string; line: number }[];
  /** 注释以外、不在 t() 里的中文 */
  bare: { text: string; line: number }[];
  /** t(`…${x}…`)：键不能拼接，要用占位符 */
  dynamic: number[];
}

const CJK = /[㐀-鿿豈-﫿]/;

export function scan(src: string): Found {
  const out: Found = { keys: [], bare: [], dynamic: [] };
  if (/i18n-ignore-file/.test(src)) return out;
  const lines = src.split('\n');
  const ignored = (line: number) => /i18n-ignore/.test(lines[line - 1] ?? '') || /i18n-ignore-next/.test(lines[line - 2] ?? '');
  let i = 0;
  let line = 1;
  let prevSig = ''; // 上一个有意义的字符，判断 / 是除号还是正则
  let prevWord = '';
  // t( 之后紧跟的字符串是键
  const isTCall = () => /(^|[^\w$.])t\s*\($/.test(src.slice(Math.max(0, i - 8), i));
  // tc('语境', '中文')：先记下语境，紧跟着的字符串是中文，键是“中文|语境”
  const isTcCall = () => /(^|[^\w$.])tc\s*\($/.test(src.slice(Math.max(0, i - 8), i));
  let context: { text: string; end: number } | null = null;
  const add = (text: string, at: number) => {
    if (CJK.test(text) && !ignored(at)) out.bare.push({ text: text.trim().slice(0, 40), line: at });
  };
  let code = ''; // 当前一段代码（JSX 文本也在这里）
  let codeLine = 1; // 这段代码开始的行
  const flushCode = () => {
    // 代码里的中文只会是 JSX 文本；按标签和花括号分段，逐段记行号
    let at = codeLine;
    for (const part of code.split(/([<>{}])/)) {
      add(part, at + (part.match(/^\n*/)?.[0].length ?? 0));
      at += (part.match(/\n/g) ?? []).length;
    }
    code = '';
    codeLine = line;
  };
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (c === '\n') { line++; code += c; i++; continue; }
    if (c === '/' && n === '/') { // 行注释
      flushCode();
      while (i < src.length && src[i] !== '\n') i++;
      continue;
    }
    if (c === '/' && n === '*') { // 块注释
      flushCode();
      i += 2;
      while (i < src.length && !(src[i] === '*' && src[i + 1] === '/')) { if (src[i] === '\n') line++; i++; }
      i += 2;
      continue;
    }
    if (c === '/' && (/[(,=:[!&|?{;+\-*%>~^]/.test(prevSig) || prevSig === '' || /^(return|typeof|case|in|of)$/.test(prevWord))) { // 正则
      flushCode();
      const start = line;
      let j = i + 1;
      let cls = false;
      while (j < src.length && src[j] !== '\n') {
        if (src[j] === '\\') { j += 2; continue; }
        if (src[j] === '[') cls = true;
        else if (src[j] === ']') cls = false;
        else if (src[j] === '/' && !cls) break;
        j++;
      }
      if (src[j] !== '/') { // 不是正则（行末也没有结束）：当作代码
        code += c;
        i++;
        continue;
      }
      add(src.slice(i, j + 1), start);
      i = j + 1;
      while (/[a-z]/.test(src[i] ?? '')) i++;
      prevSig = 'x';
      continue;
    }
    if (c === "'" || c === '"' || c === '`') {
      const ctxCall = isTcCall();
      const afterCtx = context !== null && /^\s*,\s*$/.test(src.slice(context.end, i)) ? context.text : null;
      context = null;
      const call = isTCall() || afterCtx !== null;
      flushCode();
      const start = line;
      let j = i + 1;
      let text = '';
      let hasExpr = false;
      while (j < src.length && src[j] !== c) {
        if (src[j] === '\\') { text += unescape(src[j + 1]); j += 2; continue; }
        if (c === '`' && src[j] === '$' && src[j + 1] === '{') {
          // ${...}：跳过表达式（里面可以再有字符串，简单地按括号配对）
          hasExpr = true;
          let depth = 1;
          j += 2;
          while (j < src.length && depth > 0) {
            if (src[j] === '{') depth++;
            else if (src[j] === '}') depth--;
            else if (src[j] === '\n') line++;
            j++;
          }
          text += '\u0000';
          continue;
        }
        if (src[j] === '\n') line++;
        text += src[j];
        j++;
      }
      if ((call || ctxCall) && c === '`' && hasExpr) out.dynamic.push(start);
      else if (ctxCall) context = { text, end: j + 1 };
      else if (call) out.keys.push({ key: afterCtx !== null ? `${text}|${afterCtx}` : text, line: start });
      else add(text.replace(/\u0000/g, ' '), start);
      i = j + 1;
      prevSig = 'x';
      prevWord = '';
      continue;
    }
    if (!/\s/.test(c)) {
      prevSig = c;
      if (/[\w$]/.test(c)) {
        let j = i;
        while (/[\w$]/.test(src[j] ?? '')) j++;
        prevWord = src.slice(i, j);
        code += prevWord;
        i = j;
        prevSig = 'x';
        continue;
      }
      prevWord = '';
    }
    code += c;
    i++;
  }
  flushCode();
  return out;
}

function unescape(ch: string | undefined) {
  switch (ch) {
    case 'n': return '\n';
    case 't': return '\t';
    case undefined: return '';
    default: return ch;
  }
}

/** 文字里的占位符 {name}，排好序。 */
export function placeholders(s: string): string[] {
  return [...new Set([...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort();
}
