// 通过邮件发送文档（阶段 16）：收件人、主题、附言，附件选可搜索版或原件。
import { useEffect, useRef, useState } from 'react';
import { Alert, Autocomplete, Button, Chip, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Radio, RadioGroup, Stack, TextField, Typography } from '@mui/material';
import { useEmailDocuments } from '../api/notify';
import { useApp } from '../store/app-store';
import { t } from '../i18n';

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const MAX_TO = 10;

export function EmailDialog({ docs, open, onClose, onDone }: { docs: { id: number; title: string }[]; open: boolean; onClose: () => void; onDone?: () => void }) {
  const app = useApp();
  const send = useEmailDocuments();
  const [to, setTo] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [subject, setSubject] = useState('');
  const [message, setMessage] = useState('');
  const [file, setFile] = useState<'archive' | 'original'>('archive');
  // 从菜单打开时，菜单关闭会把焦点还给“更多”按钮：等对话框出现后再把焦点放到收件人
  const toRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (open) { setTo([]); setInput(''); setSubject(''); setMessage(''); setFile('archive'); send.reset(); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // 输入逗号、分号或空格时把前面的地址变成一个收件人
  const addFrom = (text: string) => {
    const parts = text.split(/[,，;；\s]+/).map((x) => x.trim()).filter(Boolean);
    if (!parts.length) return;
    setTo((old) => [...old, ...parts.filter((p) => !old.some((o) => o.toLowerCase() === p.toLowerCase()))]);
  };
  const recipients = input.trim() ? [...to, input.trim()] : to;
  const bad = recipients.filter((a) => !EMAIL.test(a));
  const defaultSubject = docs.length > 1 ? t('{title} 等 {n} 份文档', { title: docs[0]?.title ?? '', n: docs.length }) : docs[0]?.title ?? '';

  const submit = () => {
    if (input.trim()) { addFrom(input); setInput(''); }
    send.mutate({ ids: docs.map((d) => d.id), to: recipients, subject: subject.trim() || undefined, message: message.trim() || undefined, file }, {
      onSuccess: () => { app.toast(t('正在发送给 {n} 个收件人，结果可以在“设置 → 任务记录”中查看', { n: recipients.length })); onDone?.(); onClose(); },
    });
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="email-title" slotProps={{ transition: { onEntered: () => toRef.current?.focus() } }}>
      <DialogTitle id="email-title">{t('通过邮件发送')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ pt: 1 }}>
          {send.error && <Alert severity="error">{send.error.message}</Alert>}
          <Autocomplete
            multiple freeSolo options={[]} value={to} inputValue={input}
            onChange={(_, v) => setTo((v as string[]).flatMap((x) => x.split(/[,，;；\s]+/)).filter(Boolean))}
            onInputChange={(_, v, reason) => {
              if (reason === 'reset') return;
              // 输入或粘贴的内容里有分隔符：前面完整的地址变成收件人，最后一段留在输入框
              const parts = v.split(/[,，;；\s]+/);
              if (parts.length > 1) { addFrom(parts.slice(0, -1).join(',')); setInput(parts[parts.length - 1]); } else setInput(v);
            }}
            onBlur={() => { if (input.trim()) { addFrom(input); setInput(''); } }}
            renderValue={(value, getItemProps) => value.map((a, i) => {
              const { key, ...rest } = getItemProps({ index: i });
              return <Chip key={key} {...rest} size="small" label={a} color={EMAIL.test(a) ? 'default' : 'error'} />;
            })}
            renderInput={(params) => (
              <TextField {...params} label={t('收件人')} autoFocus inputRef={toRef}
                error={bad.length > 0 || recipients.length > MAX_TO}
                helperText={bad.length ? t('“{address}”不是有效的邮箱地址', { address: bad[0] }) : recipients.length > MAX_TO ? t('一次最多发给 {n} 个收件人', { n: MAX_TO }) : t('输入邮箱后按回车，可以填多个')} />
            )}
          />
          <TextField label={t('主题')} value={subject} onChange={(e) => setSubject(e.target.value)} placeholder={defaultSubject} slotProps={{ inputLabel: { shrink: true } }} helperText={t('留空时用文档的标题')} />
          <TextField label={t('附言')} value={message} onChange={(e) => setMessage(e.target.value)} multiline minRows={3} />
          <div>
            <Typography variant="body2" sx={{ fontWeight: 500 }}>{t('附件')}</Typography>
            <RadioGroup row value={file} onChange={(e) => setFile(e.target.value as 'archive' | 'original')}>
              <FormControlLabel value="archive" control={<Radio />} label={t('可搜索版（PDF）')} />
              <FormControlLabel value="original" control={<Radio />} label={t('原件')} />
            </RadioGroup>
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', overflowWrap: 'anywhere' }}>
              {docs.length > 5
                ? t('{n} 份：{titles} 等', { n: docs.length, titles: docs.slice(0, 5).map((d) => d.title).join('、') })
                : t('{n} 份：{titles}', { n: docs.length, titles: docs.map((d) => d.title).join('、') })}
            </Typography>
          </div>
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!recipients.length || bad.length > 0 || recipients.length > MAX_TO || send.isPending} onClick={submit}>{t('发送')}</Button>
      </DialogActions>
    </Dialog>
  );
}
