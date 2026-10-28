// 预览里查找文字（v1.5.4）的查找框：⌘F 打开，回车下一处、Shift+回车上一处，Esc 关闭。
// 状态放在详情页（useFind），预览（PDF 或文字）按 terms 标出、报回总数，按 current 跳到当前那一处。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { IconButton, InputAdornment, Paper, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { ChevronDown, ChevronUp, Search, X } from 'lucide-react';
import { findTerms } from '../utils/find';
import { SW } from './icons';
import { t } from '../i18n';

/** 交给预览的查找条件：terms 为空时不标。current 从 0 数；seq 每次跳转加 1，同一处也重新滚到。 */
export interface FindState { terms: string[]; current: number; seq: number; onCount: (n: number) => void }

export interface Find {
  open: boolean; q: string; current: number; count: number | null;
  /** 打开查找框；从搜索结果点进来时带上关键词，不抢焦点（focus=false） */
  show: (q?: string, focus?: boolean) => void; close: () => void; setQ: (q: string) => void; step: (d: 1 | -1) => void;
  focusRef: React.RefObject<HTMLInputElement | null>;
  /** 预览用；查找框关着时为 undefined */
  state: FindState | undefined;
}

export function useFind(): Find {
  const [open, setOpen] = useState(false);
  const [q, setQRaw] = useState('');
  const [debounced, setDebounced] = useState('');
  const [current, setCurrent] = useState(0);
  const [seq, setSeq] = useState(0);
  // 预览报回的总数，记着是哪组词的：预览的 effect 比这里的先跑，不能在词变了之后再清空
  const [counted, setCounted] = useState<{ key: string; n: number } | null>(null);
  const focusRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 200);
    return () => clearTimeout(t);
  }, [q]);
  const terms = useMemo(() => findTerms(debounced), [debounced]);
  const key = terms.join(' ');
  useEffect(() => { setCurrent(0); setSeq((s) => s + 1); }, [key]);
  const onCount = useCallback((n: number) => setCounted({ key, n }), [key]);
  const count = open && terms.length && counted?.key === key ? counted.n : null;
  const state = useMemo<FindState | undefined>(() => (open ? { terms, current, seq, onCount } : undefined), [open, terms, current, seq, onCount]);
  return {
    open, q, current, count, focusRef, state,
    show: (text, focus = true) => {
      setOpen(true);
      if (text !== undefined) { setQRaw(text); setDebounced(text); }
      if (focus) requestAnimationFrame(() => { focusRef.current?.focus(); focusRef.current?.select(); });
    },
    close: () => { setOpen(false); setCounted(null); },
    setQ: setQRaw,
    step: (d) => {
      if (!count) return;
      setCurrent((c) => (c + d + count) % count);
      setSeq((s) => s + 1);
    },
  };
}

export function FindBar({ find }: { find: Find }) {
  if (!find.open) {
    return (
      <Tooltip title={t('查找（⌘F）')}>
        <IconButton aria-label={t('在预览里查找')} onClick={() => find.show()} sx={{ bgcolor: 'background.paper', border: 1, borderColor: 'divider', boxShadow: 1, '&:hover': { bgcolor: 'background.paper' } }}>
          <Search size={18} strokeWidth={SW} />
        </IconButton>
      </Tooltip>
    );
  }
  const { count, current } = find;
  const status = count === null ? '' : count === 0 ? t('没有找到') : t('第 {current} / {total} 处', { current: current + 1, total: count });
  return (
    <Paper elevation={3} sx={{ p: 1, pl: 1.5, borderRadius: 2, width: 340, maxWidth: 'calc(100vw - 32px)' }}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
        <TextField size="small" label={t('查找')} fullWidth value={find.q} inputRef={find.focusRef}
          onChange={(e) => find.setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return;
            if (e.key === 'Enter') { e.preventDefault(); find.step(e.shiftKey ? -1 : 1); }
            if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); find.close(); }
          }}
          slotProps={{
            htmlInput: { 'aria-label': t('查找'), spellCheck: false },
            input: { endAdornment: status ? <InputAdornment position="end"><Typography aria-live="polite" sx={{ fontSize: 12, color: count === 0 ? 'error.main' : 'text.secondary', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>{status}</Typography></InputAdornment> : undefined },
          }} />
        <Tooltip title={t('上一处（Shift+回车）')}><span><IconButton size="small" aria-label={t('上一处')} disabled={!count} onClick={() => find.step(-1)}><ChevronUp size={18} strokeWidth={SW} /></IconButton></span></Tooltip>
        <Tooltip title={t('下一处（回车）')}><span><IconButton size="small" aria-label={t('下一处')} disabled={!count} onClick={() => find.step(1)}><ChevronDown size={18} strokeWidth={SW} /></IconButton></span></Tooltip>
        <Tooltip title={t('关闭（Esc）')}><IconButton size="small" aria-label={t('关闭查找')} onClick={find.close}><X size={18} strokeWidth={SW} /></IconButton></Tooltip>
      </Stack>
    </Paper>
  );
}
