// 识别文字（v1.3.7）：预览区切到“识别文字”时显示；成员可以改正识别错的字，保存后搜索和自动分类按新文字。
// 改过的文字标为“已人工修改”；重新处理会用新识别的文字覆盖（详情页在重新处理前确认）。
import { useEffect, useState } from 'react';
import { Alert, Box, Button, Stack, TextField, Typography } from '@mui/material';
import { Pencil } from 'lucide-react';
import { ApiError } from '../../api/client';
import { useUpdateDocument, type DocumentDetail } from '../../api/documents';
import type { FindState } from '../../components/find-bar';
import { FindText } from '../../components/find-text';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { locale, t } from '../../i18n';
import { relativeTime } from './time';

export function ContentEditor({ d, readOnly, blocked, find }: { d: DocumentDetail; readOnly: boolean; blocked: boolean; find?: FindState }) {
  const app = useApp();
  const update = useUpdateDocument();
  const [text, setText] = useState<string | null>(null); // 正在编辑时的文字
  const [conflict, setConflict] = useState(false);
  const editing = text !== null;
  const changed = editing && text !== d.content;
  useEffect(() => { setText(null); setConflict(false); }, [d.id]);
  // 编辑时文字在输入框里，不标查找结果
  useEffect(() => { if (editing) find?.onCount(0); }, [editing, find?.onCount]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!changed) return;
    const h = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener('beforeunload', h);
    return () => window.removeEventListener('beforeunload', h);
  }, [changed]);

  const start = () => (blocked ? app.toast(t('请先保存或放弃右侧的修改，再改识别文字')) : setText(d.content));
  const save = () => {
    if (!changed) { setText(null); return; }
    update.mutate({ id: d.id, patch: { content: text, modifiedAt: d.modifiedAt } }, {
      onSuccess: () => { setText(null); app.toast(t('已保存识别文字，搜索会按新文字查找')); },
      onError: (e) => { if (e instanceof ApiError && e.code === 'modified') setConflict(true); else app.toast(e.message); },
    });
  };

  if (editing) {
    return (
      <Stack spacing={1.5} sx={{ p: 3 }}>
        <TextField label={t('识别文字')} multiline fullWidth minRows={14} autoFocus value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Escape' && !changed) setText(null); if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) save(); }}
          helperText={t('改正识别错的字，保存后搜索和自动分类按新文字。重新处理会用新识别的文字覆盖这里的修改。')}
          slotProps={{ htmlInput: { 'aria-label': t('识别文字'), spellCheck: false }, input: { sx: { fontSize: 14, lineHeight: '24px', bgcolor: 'background.paper' } } }} />
        {conflict && <Alert severity="warning">{t('这份文档刚被其他人修改过。复制好你的修改，刷新页面后再改。')}</Alert>}
        <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
          <Button onClick={() => { setText(null); setConflict(false); }}>{t('取消')}</Button>
          <Button variant="contained" disabled={update.isPending || conflict} onClick={save}>{t('保存')}</Button>
        </Stack>
      </Stack>
    );
  }
  return (
    <Box sx={{ p: 3 }}>
      {(d.contentEditedAt || !readOnly) && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1.5, minHeight: 32 }}>
          <Typography variant="caption" sx={{ color: 'text.secondary', flex: 1 }} title={d.contentEditedAt ? new Date(d.contentEditedAt).toLocaleString(locale()) : undefined}>
            {d.contentEditedAt ? t('已人工修改 · {time}', { time: relativeTime(d.contentEditedAt) }) : ''}
          </Typography>
          {!readOnly && <Button size="small" startIcon={<Pencil size={16} strokeWidth={SW} />} onClick={start}>{t('编辑')}</Button>}
        </Stack>
      )}
      <Box component="pre" sx={{ m: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: 'inherit', fontSize: 14, lineHeight: '24px' }}>
        {d.content ? <FindText text={d.content} find={find} /> : t('没有识别出文字。')}
      </Box>
    </Box>
  );
}
