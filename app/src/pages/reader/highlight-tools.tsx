// 阅读器里的高亮与批注（阶段 27）：选中文字后的颜色条、编辑高亮、写批注的对话框、批注面板。
import { useEffect, useState } from 'react';
import { Box, Button, ButtonBase, Dialog, DialogActions, DialogContent, DialogTitle, Drawer, IconButton, List, ListItemButton, Popover, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { MessageSquareText, Trash2, X } from 'lucide-react';
import { colorCss, HIGHLIGHT_COLORS, type Highlight, type HighlightColor } from '../../api/reading';
import { SW } from '../../components/icons';
import type { Point } from './epub-view';
import { t } from '../../i18n';

export function ColorRow({ value, onPick }: { value?: HighlightColor; onPick: (c: HighlightColor) => void }) {
  return (
    <Stack direction="row" spacing={0.75} role="radiogroup" aria-label={t('高亮颜色')}>
      {HIGHLIGHT_COLORS.map((c) => (
        <ButtonBase key={c.value} role="radio" aria-checked={value === c.value} aria-label={t('{color}色高亮', { color: c.label })} onClick={() => onPick(c.value)}
          sx={{ width: 28, height: 28, borderRadius: '50%', bgcolor: c.css, border: 2, borderColor: value === c.value ? 'text.primary' : 'transparent' }} />
      ))}
    </Stack>
  );
}

/** 选中一段文字后：选颜色就高亮，或者写批注。 */
export function SelectionPopover({ at, onClose, onColor, onNote }: { at: Point | null; onClose: () => void; onColor: (c: HighlightColor) => void; onNote: () => void }) {
  return (
    <Popover open={!!at} onClose={onClose} anchorReference="anchorPosition" anchorPosition={at ? { left: at.x, top: at.y + 6 } : undefined}
      transformOrigin={{ vertical: 'top', horizontal: 'center' }} aria-label={t('高亮')}>
      <Stack direction="row" spacing={1.5} sx={{ p: 1, alignItems: 'center' }}>
        <ColorRow onPick={onColor} />
        <Button size="small" startIcon={<MessageSquareText size={16} strokeWidth={SW} />} onClick={onNote}>{t('批注')}</Button>
      </Stack>
    </Popover>
  );
}

/** 点了已有的高亮：换颜色、改批注、删除。 */
export function HighlightPopover({ h, at, onClose, onColor, onNote, onDelete }: {
  h: Highlight | null; at: Point | null; onClose: () => void; onColor: (c: HighlightColor) => void; onNote: () => void; onDelete: () => void;
}) {
  return (
    <Popover open={!!h && !!at} onClose={onClose} anchorReference="anchorPosition" anchorPosition={at ? { left: at.x, top: at.y + 6 } : undefined}
      transformOrigin={{ vertical: 'top', horizontal: 'center' }} aria-label={t('编辑高亮')}>
      <Box sx={{ p: 1.5, maxWidth: 320 }}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
          <ColorRow value={h?.color} onPick={onColor} />
          <Button size="small" startIcon={<MessageSquareText size={16} strokeWidth={SW} />} onClick={onNote}>{h?.note ? t('改批注') : t('批注')}</Button>
          <Tooltip title={t('删除高亮')}><IconButton size="small" aria-label={t('删除高亮')} onClick={onDelete}><Trash2 size={16} strokeWidth={SW} /></IconButton></Tooltip>
        </Stack>
        {h?.note && <Typography variant="body2" sx={{ mt: 1, whiteSpace: 'pre-wrap' }}>{h.note}</Typography>}
      </Box>
    </Popover>
  );
}

/** 写批注：quote 是摘录（给一页写批注时为空）。 */
export function NoteDialog({ open, title, quote, initial, onClose, onSave, busy }: {
  open: boolean; title: string; quote?: string; initial?: string; onClose: () => void; onSave: (note: string) => void; busy?: boolean;
}) {
  const [note, setNote] = useState('');
  useEffect(() => { if (open) setNote(initial ?? ''); }, [open, initial]);
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="note-title">
      <DialogTitle id="note-title">{title}</DialogTitle>
      <DialogContent>
        {quote && <Typography variant="body2" sx={{ color: 'text.secondary', borderLeft: 3, borderColor: 'divider', pl: 1.5, mb: 2, whiteSpace: 'pre-wrap' }}>{quote}</Typography>}
        <TextField autoFocus fullWidth multiline minRows={3} label={t('批注')} value={note} onChange={(e) => setNote(e.target.value)} slotProps={{ htmlInput: { maxLength: 5000 } }} sx={{ mt: quote ? 0 : 1 }} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={busy || (!quote && !note.trim())} onClick={() => onSave(note.trim())}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 页码批注写成 page:12，显示为“第 12 页”。 */
export const locatorLabel = (locator: string) => {
  const m = /^page:(\d+)$/.exec(locator);
  return m ? t('第 {n} 页', { n: Number(m[1]) }) : '';
};

/** 右侧的批注面板：点一条跳过去。 */
export function HighlightsPanel({ open, items, onClose, onPick }: { open: boolean; items: Highlight[]; onClose: () => void; onPick: (h: Highlight) => void }) {
  return (
    <Drawer anchor="right" open={open} onClose={onClose} slotProps={{ paper: { sx: { width: { xs: '100%', sm: 360 } } } }}>
      <Stack direction="row" sx={{ alignItems: 'center', px: 2, height: 56, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="h6" sx={{ flex: 1 }}>{t('批注 · {n}', { n: items.length })}</Typography>
        <IconButton aria-label={t('关闭批注')} onClick={onClose}><X size={20} strokeWidth={SW} /></IconButton>
      </Stack>
      {items.length === 0
        ? <Typography variant="body2" sx={{ color: 'text.secondary', p: 2 }}>{t('还没有批注。选中文字后可以高亮、写批注。')}</Typography>
        : (
          <List aria-label={t('这本书的批注')} sx={{ overflow: 'auto' }}>
            {items.map((h) => (
              <ListItemButton key={h.id} onClick={() => onPick(h)} sx={{ display: 'block', borderLeft: 4, borderColor: colorCss(h.color), my: 0.5 }}>
                {locatorLabel(h.locator) && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{locatorLabel(h.locator)}</Typography>}
                {h.quote && <Typography variant="body2" sx={{ display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{h.quote}</Typography>}
                {h.note && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5, whiteSpace: 'pre-wrap' }}>{h.note}</Typography>}
              </ListItemButton>
            ))}
          </List>
        )}
    </Drawer>
  );
}
