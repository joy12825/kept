// 云端加密（v1.11.0）：文件在收好这边加密后才放进存储，存储服务商和拿到桶、NAS 的人只看到密文。
// 每个存储单独开关；开关后已有的文件在后台转换。恢复码在第一次开启时生成，只显示一次；主密钥丢了用它解开钥匙。
import { useState } from 'react';
import { Alert, Box, Button, Card, Chip, Stack, TextField, Typography } from '@mui/material';
import { useEncryption, useNewRecoveryCode, useRecoverEncryption, type AdminStorage } from '../../api/storage';
import { ErrorState, LoadingState } from '../../components/common';
import { t } from '../../i18n';
import { SectionTitle } from './file-safety';
import { useApp } from '../../store/app-store';
import { dateTime } from '../detail/time';
import { RecoveryCodeDialog } from '../../components/recovery-code-dialog';
import { eeBuild } from '@ee';

export const CODE_BODY = () => t('恢复码只显示这一次，收好不会保存它。主密钥（secret.key 或 KEPT_SECRET_KEY）丢了、或从远程备份恢复到新的服务器时，要用它解开各个存储的钥匙，否则加密的文件打不开。请抄在纸上，或存进密码管理器。');

/** 能不能加密：数据目录就在本机，不需要；没有配置的存储也不行。 */
export function encryptable(st: AdminStorage) {
  return st.kind !== 'local' && st.kind !== 'missing';
}

/** 存储列表里的加密状态。 */
export function EncryptionChip({ st }: { st: AdminStorage }) {
  const e = st.encryption;
  if (!encryptable(st)) return null;
  if (e?.locked) return <Chip size="small" color="error" variant="outlined" label={t('需要恢复码')} />;
  if (e?.converting) {
    const p = e.progress;
    const label = e.enabled ? t('正在加密') : t('正在转回明文');
    return <Chip size="small" color="info" variant="outlined" label={p ? `${label} ${p.converted + p.skipped}/${p.total}` : label} />;
  }
  if (e?.enabled) return <Chip size="small" color="success" variant="outlined" label={t('已加密')} />;
  return <Typography component="span" variant="caption" sx={{ color: 'text.secondary' }}>{t('未加密')}</Typography>;
}


/** 云端加密：恢复码，以及钥匙解不开时输入恢复码。 */
export function EncryptionSection() {
  const app = useApp();
  const status = useEncryption();
  const renew = useNewRecoveryCode();
  const recover = useRecoverEncryption();
  const [code, setCode] = useState('');
  const [shown, setShown] = useState<string | null>(null);
  if (status.isPending) return <LoadingState />;
  if (status.isError) return <ErrorState onRetry={() => status.refetch()} />;
  const s = status.data;
  const regenerate = () => app.confirm({
    title: t('重新生成恢复码？'), body: t('生成后旧的恢复码作废，新的只显示一次。'), verb: t('生成'),
    run: () => renew.mutate(undefined, {
      onSuccess: (c) => { app.confirm(null); setShown(c); },
      onError: (e) => { app.confirm(null); app.toast(e.message); },
    }),
  });
  const unlock = () => recover.mutate(code, {
    onSuccess: (n) => { setCode(''); app.toast(t('已解开 {n} 个存储的钥匙', { n })); },
    onError: (e) => app.toast(e.message),
  });
  return (
    <Box component="section" aria-label={t('加密存放')} sx={{ mt: 4 }}>
      <SectionTitle title={t('加密存放')} tech={t('存储加密：网盘或云服务商只看到密文')} />
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {eeBuild ? t('在上面的列表里给存储开启加密后，文件在收好这边加密才放进去，存储服务商和拿到桶或 NAS 的人只看到密文；备份目标开启后，整份备份都加密。钥匙由主密钥保管，主密钥丢了用恢复码解开。')
          : t('开源的免费版不能开启加密。官方版本里加密过的存储照常能读，钥匙由主密钥保管，主密钥丢了用恢复码解开。')}
      </Typography>
      {s.locked.length > 0 && (
        <Alert severity="error" sx={{ mb: 1.5 }}>
          <Typography variant="body2" sx={{ mb: 1 }}>
            {t('主密钥变了，存储 {names} 的钥匙解不开，里面加密的文件现在打不开。输入恢复码解开：', { names: s.locked.join(t('、')) })}
          </Typography>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} sx={{ alignItems: { sm: 'center' } }}>
            <TextField size="small" label={t('恢复码')} value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" sx={{ minWidth: 280 }} />
            <Button variant="contained" disabled={!code.trim() || recover.isPending} onClick={unlock}>{t('解开')}</Button>
          </Stack>
        </Alert>
      )}
      <Card variant="outlined" sx={{ p: 2 }}>
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} sx={{ alignItems: { sm: 'center' } }}>
          <Typography sx={{ flex: 1 }}>
            {s.recovery && s.recoveryAt
              ? t('恢复码生成于 {date}。', { date: dateTime(s.recoveryAt) })
              : t('还没有恢复码：第一次给存储开启加密时生成。')}
          </Typography>
          {s.recovery && <Button size="small" disabled={s.locked.length > 0 || renew.isPending} onClick={regenerate}>{t('重新生成恢复码')}</Button>}
        </Stack>
      </Card>
      {shown && <RecoveryCodeDialog code={shown} body={CODE_BODY()} onClose={() => setShown(null)} />}
    </Box>
  );
}
