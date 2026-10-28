// 详情页的书目（阶段 23）：书籍才显示；“设为书籍”和“编辑书目”用同一个对话框。
import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Stack, TextField, Typography } from '@mui/material';
import { Search } from 'lucide-react';
import {
  coverUrl, formatIsbn, isbnValid, useDeleteCover, useFetchCover, useLookupBook, useSetBook, useUploadCover,
  type Book, type BookInput, type BookLookupResult,
} from '../../api/books';
import { useUpdateDocument } from '../../api/documents';
import { useMe } from '../../api/session';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { t } from '../../i18n';
import { AddToList } from './add-to-list';
import { AssistCatalogButton, BookSummarySection } from '@ee';

interface Form { isbn: string; authors: string; publisher: string; series: string; edition: string; published: string; pages: string; language: string; description: string }

const empty: Form = { isbn: '', authors: '', publisher: '', series: '', edition: '', published: '', pages: '', language: '', description: '' };

const toForm = (b?: Book): Form => (b
  ? { isbn: b.isbn, authors: b.authors.join('、'), publisher: b.publisher, series: b.series, edition: b.edition, published: b.published,
    pages: b.pages ? String(b.pages) : '', language: b.language, description: b.description }
  : empty);

/** 作者之间用顿号、逗号或分号隔开。 */
export const splitAuthors = (s: string) => s.split(/[、,，;；]/).map((a) => a.trim()).filter(Boolean);

type Filled = Partial<Record<keyof Form, true>>;

/** 查找结果里能填进表单的栏目（书名另算：改的是文档标题）。 */
const fromResult = (r: BookLookupResult): Partial<Form> => ({
  authors: r.authors.join('、'), publisher: r.publisher, published: r.published, pages: r.pages ? String(r.pages) : '',
  language: r.language, description: r.description, series: r.series,
});

export function BookDialog({ docId, docTitle, book, open, onClose }: { docId: number; docTitle: string; book?: Book; open: boolean; onClose: () => void }) {
  const app = useApp();
  const save = useSetBook(docId);
  const lookup = useLookupBook();
  const fetchCover = useFetchCover(docId);
  const update = useUpdateDocument();
  const canLookup = useMe().data?.features?.bookLookup ?? false;
  const [f, setF] = useState<Form>(empty);
  const [filled, setFilled] = useState<Filled>({});
  const [used, setUsed] = useState<BookLookupResult | null>(null);
  const [rename, setRename] = useState(false);
  const [withCover, setWithCover] = useState(false);
  useEffect(() => {
    if (!open) return;
    setF(toForm(book)); setFilled({}); setUsed(null); setRename(false); setWithCover(false);
    save.reset(); lookup.reset();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const set = (p: Partial<Form>) => {
    setF((x) => ({ ...x, ...p }));
    setFilled((x) => { const n = { ...x }; for (const k of Object.keys(p)) delete n[k as keyof Form]; return n; });
  };
  const isbnBad = !isbnValid(f.isbn);
  const pagesBad = f.pages !== '' && !/^\d{1,6}$/.test(f.pages);

  // 只填空着的栏目，或上一次查找填入、用户没有改过的栏目
  const apply = (r: BookLookupResult) => {
    const next = { ...f };
    const nf: Filled = {};
    for (const [k, v] of Object.entries(fromResult(r)) as [keyof Form, string][]) {
      if (!v) continue;
      if (f[k] === '' || filled[k]) { next[k] = v; nf[k] = true; }
    }
    // 上一次查找填入、这一次没有值的栏目清空
    for (const k of Object.keys(filled) as (keyof Form)[]) if (!nf[k]) next[k] = '';
    setF(next); setFilled(nf); setUsed(r);
    setRename(!book && !!r.title && r.title !== docTitle);
    setWithCover(r.hasCover && !book?.hasCover);
  };
  const find = () => lookup.mutate(f.isbn.trim(), { onSuccess: (res) => { if (res.results.length) apply(res.results[0]); else setUsed(null); } });
  const count = Object.keys(filled).length;
  const others = (lookup.data?.results ?? []).filter((r) => r.source !== used?.source);

  const submit = async () => {
    const body: BookInput = {
      isbn: f.isbn.trim(), authors: splitAuthors(f.authors), publisher: f.publisher, series: f.series, edition: f.edition,
      published: f.published, pages: f.pages ? Number(f.pages) : 0, language: f.language, description: f.description,
    };
    let saved: Book;
    try { saved = await save.mutateAsync(body); } catch { return; }
    const problems: string[] = [];
    if (used && withCover) {
      try { await fetchCover.mutateAsync({ isbn: saved.isbn, source: used.source }); } catch (e) { problems.push((e as Error).message); }
    }
    if (used && rename) {
      try { await update.mutateAsync({ id: docId, patch: { title: used.title } }); } catch (e) { problems.push((e as Error).message); }
    }
    app.toast(problems.length ? t('书目已保存，但{problems}', { problems: problems.join(t('；')) }) : book ? t('已保存书目') : t('已设为书籍'));
    onClose();
  };
  const busy = save.isPending || fetchCover.isPending || update.isPending;
  const field = (k: keyof Form, label: string, extra: object = {}) => (
    <TextField label={label} value={f[k]} onChange={(e) => set({ [k]: e.target.value })} fullWidth {...extra} />
  );
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="book-title">
      <DialogTitle id="book-title">{book ? t('编辑书目') : t('设为书籍')}</DialogTitle>
      <DialogContent>
        {save.error && <Alert severity="error" sx={{ mb: 2 }}>{save.error.message}</Alert>}
        <Stack spacing={2} sx={{ pt: 1 }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
            {field('isbn', 'ISBN', { error: isbnBad, helperText: isbnBad ? t('应为 10 位或 13 位，校验位要对得上') : t('可以带连字符，10 位的会换成 13 位'), slotProps: { htmlInput: { maxLength: 20 } },
              onKeyDown: (e: React.KeyboardEvent) => { if (e.key === 'Enter' && canLookup && f.isbn.trim() && !isbnBad) { e.preventDefault(); find(); } } })}
            {canLookup && (
              <Button variant="outlined" startIcon={lookup.isPending ? <CircularProgress size={14} /> : <Search size={16} strokeWidth={SW} />}
                disabled={!f.isbn.trim() || isbnBad || lookup.isPending} onClick={find} sx={{ flex: 'none', height: 56 }}>{t('查找')}</Button>
            )}
          </Stack>
          {lookup.error && <Alert severity="error">{lookup.error.message}</Alert>}
          {lookup.data && (
            <Alert severity={lookup.data.results.length ? 'success' : 'info'} aria-label={t('查找结果')}>
              {used
                ? <>{count ? t('已从 {source} 填入 {n} 项', { source: used.sourceName, n: count }) : t('{source} 找到了这本书，空着的栏目没有可以填的', { source: used.sourceName })}{used.title && t('：《{title}》', { title: used.title })}</>
                : t('没有找到这本书，可以手动填写。')}
              {others.map((r) => <Button key={r.source} size="small" onClick={() => apply(r)} sx={{ ml: 1, py: 0 }}>{t('改用 {source}', { source: r.sourceName })}</Button>)}
              {lookup.data.failures.map((x) => <Typography key={x.source} variant="caption" component="div" sx={{ color: 'text.secondary' }}>{t('{source}：{message}', { source: x.sourceName, message: x.message })}</Typography>)}
            </Alert>
          )}
          {used && (used.title && used.title !== docTitle || used.hasCover) && (
            <Stack>
              {used.title && used.title !== docTitle && <FormControlLabel control={<Checkbox checked={rename} onChange={(e) => setRename(e.target.checked)} />} label={t('文档标题改为《{title}》', { title: used.title })} />}
              {used.hasCover && <FormControlLabel control={<Checkbox checked={withCover} onChange={(e) => setWithCover(e.target.checked)} />} label={book?.hasCover ? t('用找到的封面替换现在的') : t('同时保存封面')} />}
            </Stack>
          )}
          {field('authors', t('作者'), { helperText: t('多位作者用顿号或逗号隔开') })}
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            {field('publisher', t('出版社'), { slotProps: { htmlInput: { maxLength: 200 } } })}
            {field('published', t('出版时间'), { placeholder: t('2019 或 2019-05'), slotProps: { htmlInput: { maxLength: 200 } } })}
          </Stack>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            {field('series', t('丛书'), { slotProps: { htmlInput: { maxLength: 200 } } })}
            {field('edition', t('版次'), { slotProps: { htmlInput: { maxLength: 200 } } })}
          </Stack>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            {field('pages', t('页数'), { error: pagesBad, slotProps: { htmlInput: { inputMode: 'numeric', maxLength: 6 } } })}
            {field('language', t('语言'), { slotProps: { htmlInput: { maxLength: 200 } } })}
          </Stack>
          {field('description', t('简介'), { multiline: true, minRows: 3, slotProps: { htmlInput: { maxLength: 5000 } } })}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={busy || isbnBad || pagesBad} onClick={() => void submit()}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 信息栏里的书目：封面、有值的项和简介。 */
export function BookSection({ docId, docTitle, book, readOnly }: { docId: number; docTitle: string; book: Book; readOnly: boolean }) {
  const app = useApp();
  const [open, setOpen] = useState(false);
  const upload = useUploadCover(docId);
  const delCover = useDeleteCover(docId);
  const input = useRef<HTMLInputElement>(null);
  const rows: [string, string][] = ([
    [t('作者'), book.authors.join(t('、'))], ['ISBN', book.isbn ? formatIsbn(book.isbn) : ''], [t('出版社'), book.publisher],
    [t('出版时间'), book.published], [t('丛书'), book.series], [t('版次'), book.edition], [t('页数'), book.pages ? t('{n} 页', { n: book.pages }) : ''], [t('语言'), book.language],
  ] as [string, string][]).filter(([, v]) => v);
  const pick = (file?: File) => {
    if (!file) return;
    upload.mutate(file, { onSuccess: () => app.toast(t('已更换封面')), onError: (e) => app.toast(e.message) });
  };
  return (
    <Box component="section" aria-label={t('书目')}>
      <Stack direction="row" sx={{ alignItems: 'center', mb: 0.5 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary', flex: 1 }}>{t('书目')}</Typography>
        {!readOnly && <AssistCatalogButton docId={docId} />}
        {!readOnly && <AddToList docId={docId} title={docTitle} />}
        {!readOnly && <Button size="small" onClick={() => setOpen(true)}>{t('编辑')}</Button>}
      </Stack>
      <Stack direction="row" spacing={2} sx={{ alignItems: 'flex-start' }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          {rows.length === 0 && !book.description
            ? <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('还没有填写书目。')}</Typography>
            : rows.map(([k, v]) => (
              <Stack key={k} direction="row" spacing={1} sx={{ minHeight: 28, alignItems: 'baseline' }}>
                <Typography variant="body2" sx={{ width: 64, flex: 'none', color: 'text.secondary' }}>{k}</Typography>
                <Typography variant="body2" sx={k === 'ISBN' ? { whiteSpace: 'nowrap' } : { overflowWrap: 'anywhere' }}>{v}</Typography>
              </Stack>
            ))}
        </Box>
        {(book.hasCover || !readOnly) && (
          <Stack spacing={0.5} sx={{ width: 88, flex: 'none', alignItems: 'center' }}>
            {book.hasCover
              ? <Box component="img" src={coverUrl(docId, book.updatedAt)} alt={t('封面')} sx={{ width: 88, borderRadius: 1, boxShadow: 1, display: 'block' }} />
              : <Box sx={{ width: 88, aspectRatio: '2 / 3', borderRadius: 1, border: '1px dashed', borderColor: 'divider', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('没有封面')}</Typography>
              </Box>}
            {!readOnly && (
              <>
                <input ref={input} type="file" hidden accept="image/jpeg,image/png" aria-label={t('选择封面图')} onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ''; }} />
                <Button size="small" disabled={upload.isPending} onClick={() => input.current?.click()}>{book.hasCover ? t('更换封面') : t('上传封面')}</Button>
                {book.hasCover && <Button size="small" color="inherit" disabled={delCover.isPending}
                  onClick={() => delCover.mutate(undefined, { onSuccess: () => app.toast(t('已删除封面')), onError: (e) => app.toast(e.message) })}>{t('删除封面')}</Button>}
              </>
            )}
          </Stack>
        )}
      </Stack>
      {book.description && <Typography variant="body2" sx={{ mt: 1, whiteSpace: 'pre-wrap', color: 'text.secondary' }}>{book.description}</Typography>}
      <BookSummarySection docId={docId} readOnly={readOnly} />
      <BookDialog docId={docId} docTitle={docTitle} book={book} open={open} onClose={() => setOpen(false)} />
    </Box>
  );
}
