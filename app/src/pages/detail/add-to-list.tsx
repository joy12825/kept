// 详情页的“加入书单”（阶段 28）：每个书单一个勾选项，最后可以新建一个书单并放进去。
import { useState } from 'react';
import { Button, Checkbox, Divider, ListItemIcon, ListItemText, Menu, MenuItem } from '@mui/material';
import { Plus } from 'lucide-react';
import { useAddToBookList, useBookLists, useDocumentBookLists, useRemoveFromBookList, type BookList } from '../../api/booklists';
import { SW } from '../../components/icons';
import { t } from '../../i18n';
import { useApp } from '../../store/app-store';
import { BookListDialog } from '../booklists/book-list-dialog';

export function AddToList({ docId, title }: { docId: number; title: string }) {
  const app = useApp();
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [create, setCreate] = useState(false);
  const lists = useBookLists();
  const mine = useDocumentBookLists(docId, !!el);
  const add = useAddToBookList();
  const remove = useRemoveFromBookList();
  const fail = (e: Error) => app.toast(e.message);
  const toggle = (l: BookList, on: boolean) => on
    ? add.mutate({ listId: l.id, documentId: docId }, { onSuccess: () => app.toast(t('已把“{title}”加入“{list}”', { title, list: l.name })), onError: fail })
    : remove.mutate({ listId: l.id, documentId: docId }, { onSuccess: () => app.toast(t('已把“{title}”移出“{list}”', { title, list: l.name })), onError: fail });
  return (
    <>
      <Button size="small" onClick={(e) => setEl(e.currentTarget)}>{t('加入书单')}</Button>
      <Menu anchorEl={el} open={!!el} onClose={() => setEl(null)} slotProps={{ list: { 'aria-label': t('加入书单') } }}>
        {(lists.data ?? []).map((l) => {
          const on = mine.data?.includes(l.id) ?? false;
          return (
            <MenuItem key={l.id} sx={{ minWidth: 220 }} disabled={!mine.data || add.isPending || remove.isPending} onClick={() => toggle(l, !on)}>
              <ListItemIcon><Checkbox edge="start" size="small" checked={on} tabIndex={-1} disableRipple slotProps={{ input: { 'aria-label': l.name } }} /></ListItemIcon>
              <ListItemText primary={l.name} secondary={t('{n} 本', { n: l.count })} />
            </MenuItem>
          );
        })}
        {(lists.data?.length ?? 0) > 0 && <Divider />}
        <MenuItem onClick={() => { setEl(null); setCreate(true); }}>
          <ListItemIcon><Plus size={16} strokeWidth={SW} /></ListItemIcon>{t('新建书单…')}
        </MenuItem>
      </Menu>
      <BookListDialog open={create} onClose={() => setCreate(false)} onSaved={(l) => toggle(l, true)} />
    </>
  );
}
