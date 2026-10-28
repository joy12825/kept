// 书单详情（阶段 28）：按顺序的书、推荐语；成员可以添加、调整顺序、移出、改推荐语、改名、删除。
import { useMemo, useState } from 'react';
import { Link as RouterLink, useNavigate, useParams } from 'react-router-dom';
import { Autocomplete, Box, Button, IconButton, Link, Menu, MenuItem, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { ArrowDown, ArrowUp, Ellipsis, Trash2 } from 'lucide-react';
import {
  useAddToBookList, useBookList, useDeleteBookList, useRemoveFromBookList, useReorderBookList, useSetBookListNote, type BookListItem,
} from '../../api/booklists';
import { useDocumentList } from '../../api/documents';
import { STATUS_LABEL } from '../../api/reading';
import { useRole } from '../../api/workspaces';
import { BookCover } from '../../components/book-cover';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { BookListDialog } from './book-list-dialog';
import { t } from '../../i18n';

export default function BookListPage() {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const app = useApp();
  const role = useRole();
  const q = useBookList(id);
  const reorder = useReorderBookList(id);
  const remove = useRemoveFromBookList();
  const del = useDeleteBookList();
  const [menuEl, setMenuEl] = useState<HTMLElement | null>(null);
  const [edit, setEdit] = useState(false);
  if (q.isPending) return <LoadingState />;
  if (q.error?.status === 404) return <EmptyState title={t('找不到这个书单')} text={t('它可能已被删除。')} action={t('回到书单')} onAction={() => nav('/book-lists')} />;
  if (q.isError) return <ErrorState onRetry={() => void q.refetch()} />;
  const l = q.data;
  const ids = l.items.map((it) => it.documentId);
  const move = (i: number, d: number) => {
    const next = [...ids];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    reorder.mutate(next, { onError: (e) => app.toast(e.message) });
  };
  const fail = (e: Error) => app.toast(e.message);
  const removeBook = (it: BookListItem) => remove.mutate({ listId: id, documentId: it.documentId }, { onSuccess: () => app.toast(t('已把“{title}”移出书单', { title: it.title })), onError: fail });
  const deleteList = () => app.confirm({
    title: t('删除书单“{name}”？', { name: l.name }), body: t('书单里的书不会被删除。'), verb: t('删除'),
    run: () => { app.confirm(null); del.mutate(id, { onSuccess: () => { app.toast(t('已删除书单“{name}”', { name: l.name })); nav('/book-lists'); }, onError: fail }); },
  });

  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 4, maxWidth: 880 }}>
      <Link component={RouterLink} to="/book-lists" variant="body2" underline="hover">{t('书单')}</Link>
      <Stack direction="row" sx={{ alignItems: 'center', mt: 0.5 }}>
        <Typography variant="h4" component="h1" sx={{ flex: 1 }}>{l.name}</Typography>
        {role.canWrite && (
          <>
            <Tooltip title={t('更多')}><IconButton aria-label={t('更多')} onClick={(e) => setMenuEl(e.currentTarget)}><Ellipsis size={20} strokeWidth={SW} /></IconButton></Tooltip>
            <Menu anchorEl={menuEl} open={!!menuEl} onClose={() => setMenuEl(null)}>
              <MenuItem sx={{ minWidth: 160 }} onClick={() => { setMenuEl(null); setEdit(true); }}>{t('改名和说明')}</MenuItem>
              <MenuItem sx={{ color: 'error.main' }} onClick={() => { setMenuEl(null); deleteList(); }}>{t('删除书单')}</MenuItem>
            </Menu>
          </>
        )}
      </Stack>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>{t('{n} 本', { n: l.count })}{l.description ? ` · ${l.description}` : ''}</Typography>
      {role.canWrite && <AddBook listId={id} inList={ids} />}
      {l.items.length === 0
        ? <EmptyState title={t('书单里还没有书')} text={role.canWrite ? t('在上面按书名搜索书房里的书，加进来。') : t('成员把书加进来后会出现在这里。')} />
        : (
          <Stack component="ol" aria-label={t('书单里的书')} spacing={2} sx={{ listStyle: 'none', p: 0, mt: 3 }}>
            {l.items.map((it, i) => (
              <Stack component="li" key={it.documentId} aria-label={it.title} direction="row" spacing={2} sx={{ alignItems: 'flex-start' }}>
                <Typography sx={{ width: 24, pt: 0.5, color: 'text.secondary', textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{i + 1}</Typography>
                <Box component={RouterLink} to={`/docs/${it.documentId}`} sx={{ display: 'block' }}>
                  <BookCover id={it.documentId} title={it.title} hasCover={it.hasCover} hasThumbnail={it.hasThumbnail} width={72} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Link component={RouterLink} to={`/docs/${it.documentId}`} underline="hover" sx={{ fontSize: 16, fontWeight: 500, color: 'text.primary' }}>{it.title}</Link>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                    {[it.authors.join(t('、')), it.reading?.status ? STATUS_LABEL[it.reading.status as keyof typeof STATUS_LABEL] : ''].filter(Boolean).join(' · ')}
                  </Typography>
                  <Note listId={id} item={it} readOnly={!role.canWrite} />
                </Box>
                {role.canWrite && (
                  <Stack direction="row">
                    <Tooltip title={t('上移')}><span><IconButton size="small" aria-label={t('上移“{title}”', { title: it.title })} disabled={i === 0} onClick={() => move(i, -1)}><ArrowUp size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                    <Tooltip title={t('下移')}><span><IconButton size="small" aria-label={t('下移“{title}”', { title: it.title })} disabled={i === l.items.length - 1} onClick={() => move(i, 1)}><ArrowDown size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                    <Tooltip title={t('移出书单')}><IconButton size="small" aria-label={t('把“{title}”移出书单', { title: it.title })} onClick={() => removeBook(it)}><Trash2 size={16} strokeWidth={SW} /></IconButton></Tooltip>
                  </Stack>
                )}
              </Stack>
            ))}
          </Stack>
        )}
      <BookListDialog list={l} open={edit} onClose={() => setEdit(false)} />
    </Box>
  );
}

/** 推荐语：点一下就地编辑，失去焦点或回车保存。 */
function Note({ listId, item, readOnly }: { listId: number; item: BookListItem; readOnly: boolean }) {
  const app = useApp();
  const save = useSetBookListNote(listId);
  const [text, setText] = useState<string | null>(null);
  if (readOnly) return item.note ? <Typography variant="body2" sx={{ mt: 0.5 }}>{item.note}</Typography> : null;
  const commit = () => {
    if (text === null) return;
    const v = text.trim();
    setText(null);
    if (v !== item.note) save.mutate({ documentId: item.documentId, note: v }, { onError: (e) => app.toast(e.message) });
  };
  return text === null
    ? <Button size="small" sx={{ mt: 0.25, px: 0.5, mx: -0.5, color: item.note ? 'text.primary' : 'text.secondary', fontWeight: 400, justifyContent: 'flex-start', textAlign: 'left' }}
        onClick={() => setText(item.note)}>{item.note || t('写一句推荐语…')}</Button>
    : <TextField autoFocus size="small" fullWidth label={t('推荐语')} value={text} onChange={(e) => setText(e.target.value)} onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') commit(); if (e.key === 'Escape') setText(null); }} slotProps={{ htmlInput: { maxLength: 200 } }} sx={{ mt: 1 }} />;
}

/** 按书名搜索书房里的书，加到书单最后。 */
function AddBook({ listId, inList }: { listId: number; inList: number[] }) {
  const app = useApp();
  const add = useAddToBookList();
  const [input, setInput] = useState('');
  const isBook = { field: 'book', op: 'eq', value: true };
  const filter = JSON.stringify(input.trim() ? { all: [isBook, { text: input.trim(), prefix: true }] } : isBook);
  const books = useDocumentList({ filter, sort: 'title', dir: 'asc' });
  const options = useMemo(() => books.data?.pages.flatMap((p) => p.items) ?? [], [books.data]);
  return (
    <Autocomplete sx={{ mt: 2.5, maxWidth: 420 }} options={options} value={null} inputValue={input}
      onInputChange={(_, v) => setInput(v)} filterOptions={(o) => o} loading={books.isFetching}
      getOptionLabel={(o) => o.title} getOptionDisabled={(o) => inList.includes(o.id)} noOptionsText={t('书房里没有找到这本书')}
      onChange={(_, o) => { if (o) add.mutate({ listId, documentId: o.id }, { onSuccess: () => { app.toast(t('已把“{title}”加入书单', { title: o.title })); setInput(''); }, onError: (e) => app.toast(e.message) }); }}
      renderOption={(props, o) => <li {...props} key={o.id}>{o.title}{inList.includes(o.id) && <Typography component="span" variant="caption" sx={{ ml: 1, color: 'text.secondary' }}>{t('已在书单里')}</Typography>}</li>}
      renderInput={(params) => <TextField {...params} size="small" label={t('添加书')} placeholder={t('按书名搜索')} />} />
  );
}
