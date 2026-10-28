// OFD 的电子签章（v1.11.3）：每个签章是谁签的、什么时候签的，签完以后文件有没有被改过。
// 证书的签发机构服务器没有核验，这里照实说明。
import { useState } from 'react';
import { Alert, Box, Button, Chip, Skeleton, Stack, Typography } from '@mui/material';
import { sealImageUrl, useSignatures, type OFDSignature } from '../../api/formats';
import { dateTime } from './time';
import { t } from '../../i18n';

const STATUS: Record<OFDSignature['status'], { label: () => string; color: 'success' | 'error' | 'default' }> = {
  valid: { label: () => t('有效'), color: 'success' },
  changed: { label: () => t('被改过'), color: 'error' },
  unknown: { label: () => t('无法验证'), color: 'default' },
};

/** 右栏标题旁的一句话：全部有效、有被改过的…… */
export function signaturesSummary(list: OFDSignature[] | undefined): { text: string; color: 'success' | 'error' | 'default' } | null {
  if (!list || list.length === 0) return null;
  if (list.some((s) => s.status === 'changed')) return { text: t('被改过'), color: 'error' };
  if (list.every((s) => s.status === 'valid')) return { text: t('有效'), color: 'success' };
  return { text: t('无法验证'), color: 'default' };
}

function One({ docId, n, s }: { docId: number; n: number; s: OFDSignature }) {
  const [open, setOpen] = useState(false);
  const st = STATUS[s.status];
  const changed = s.files.filter((f) => !f.ok);
  return (
    <Stack direction="row" spacing={1.5} sx={{ py: 1 }}>
      {s.hasSeal
        ? <Box component="img" src={sealImageUrl(docId, n)} alt={s.sealName || t('印章')} sx={{ width: 56, height: 56, objectFit: 'contain', flexShrink: 0 }} />
        : <Box sx={{ width: 56, height: 56, flexShrink: 0, borderRadius: '50%', border: 2, borderColor: 'error.light', opacity: 0.5 }} aria-hidden />}
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>{s.sealName || (s.type === 'Sign' ? t('数字签名') : t('电子签章'))}</Typography>
          <Chip size="small" color={st.color} variant={s.status === 'valid' ? 'outlined' : 'filled'} label={st.label()} />
        </Stack>
        {(s.signer || s.org) && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{[s.signer, s.org].filter(Boolean).join(' · ')}</Typography>}
        {s.time && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('签于 {time}', { time: dateTime(s.time) })}{s.page ? ` · ${t('第 {n} 页', { n: s.page })}` : ''}</Typography>}
        {s.reason && <Typography variant="caption" sx={{ display: 'block', color: s.status === 'changed' ? 'error.main' : 'text.secondary' }}>{s.reason}</Typography>}
        {s.outsideValidity && <Typography variant="caption" sx={{ display: 'block', color: 'warning.main' }}>{t('签章时间不在证书的有效期内')}</Typography>}
        <Button size="small" onClick={() => setOpen((o) => !o)} sx={{ px: 0, minWidth: 0 }}>{open ? t('收起') : t('详细')}</Button>
        {open && (
          <Box sx={{ fontSize: 12, color: 'text.secondary' }}>
            {s.issuer && <div>{t('证书签发者：{name}', { name: s.issuer })}{s.selfSigned ? t('（自己签发）') : ''}</div>}
            {s.validFrom && s.validTo && <div>{t('证书有效期：{from} 至 {to}', { from: dateTime(s.validFrom), to: dateTime(s.validTo) })}</div>}
            {s.provider && <div>{t('签章软件：{name}', { name: s.provider })}</div>}
            <div>{t('签名覆盖 {n} 个文件', { n: s.files.length })}{changed.length ? t('，其中改过或缺了：{files}', { files: changed.map((f) => f.path).join('、') }) : ''}</div>
            {s.unsigned.length > 0 && <div>{t('不在签名范围内的文件：{files}', { files: s.unsigned.join('、') })}</div>}
          </Box>
        )}
      </Box>
    </Stack>
  );
}

export function Signatures({ docId }: { docId: number }) {
  const list = useSignatures(docId);
  if (list.isPending) return <Skeleton variant="rounded" height={56} />;
  if (list.error) return <Typography variant="body2" sx={{ color: 'error.main' }}>{list.error.message}</Typography>;
  if (list.data.length === 0) return <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('这份 OFD 没有电子签章')}</Typography>;
  return (
    <Box>
      {list.data.map((s, i) => <One key={i} docId={docId} n={i + 1} s={s} />)}
      <Alert severity="info" variant="outlined" sx={{ mt: 1, py: 0, fontSize: 12 }}>
        {t('“有效”说明文件签完以后没有被改过、签名和证书对得上。证书是哪家机构签发的，这里没有核验。')}
      </Alert>
    </Box>
  );
}
