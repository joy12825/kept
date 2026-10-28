// 设置 → 连接手机、已连接的设备（UI 设计规范 5.11，设计文档 9.3）。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, Button, Card, Chip, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import { RefreshCw, Smartphone } from 'lucide-react';
import qrcode from 'qrcode-generator';
import { useCreatePairingCode, useDevices, useRevokeDevice, type PairingCode } from '../../api/devices';
import { useMe } from '../../api/session';
import { useApp } from '../../store/app-store';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { fontMono } from '../../theme';
import { dateTime, relativeTime } from '../detail/time';
import { t } from '../../i18n';

/** 二维码的模块矩阵。内容按 UTF-8 编码（空间名称是中文）。 */
export function qrMatrix(text: string): boolean[][] {
  const qr = qrcode(0, 'M');
  qr.addData(String.fromCharCode(...new TextEncoder().encode(text)), 'Byte');
  qr.make();
  const n = qr.getModuleCount();
  return Array.from({ length: n }, (_, r) => Array.from({ length: n }, (_, c) => qr.isDark(r, c)));
}

function QrCode({ text, faded }: { text: string; faded: boolean }) {
  const m = useMemo(() => qrMatrix(text), [text]);
  const n = m.length;
  return (
    <Box component="svg" viewBox={`0 0 ${n} ${n}`} shapeRendering="crispEdges" role="img" aria-label={t('配对二维码')}
      sx={{ width: '100%', height: '100%', display: 'block', opacity: faded ? 0.12 : 1 }}>
      {m.flatMap((row, r) => row.map((on, c) => (on ? <rect key={`${r}-${c}`} x={c} y={r} width={1} height={1} fill="#1F2328" /> : null)))}
    </Box>
  );
}

/** 剩余秒数，每秒更新。 */
function useSecondsLeft(until?: string) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => window.clearInterval(timer); }, []);
  return until ? Math.max(0, Math.round((new Date(until).getTime() - now) / 1000)) : 0;
}

const isLocal = (u: string) => /^https?:\/\/(localhost|127\.|\[::1\])/.test(u);

export function PairPanel() {
  const me = useMe().data;
  const viewer = me?.workspace.role === 'viewer';
  const create = useCreatePairingCode();
  const [pc, setPc] = useState<PairingCode | null>(null);
  const issuedAt = useRef(0);
  const left = useSecondsLeft(pc?.expiresAt);
  const devices = useDevices(pc && left > 0 ? 3000 : undefined);
  const nav = useNavigate();
  const app = useApp();

  const regen = () => create.mutate(undefined, { onSuccess: (r) => { issuedAt.current = Date.now(); setPc(r); } });
  const started = useRef(false);
  useEffect(() => { if (me && !started.current) { started.current = true; regen(); } }, [me]); // eslint-disable-line react-hooks/exhaustive-deps

  // 配对成功：出现一台在配对码生成之后连接的设备
  useEffect(() => {
    if (!pc || !issuedAt.current) return;
    const fresh = devices.data?.find((d) => new Date(d.pairedAt).getTime() >= issuedAt.current - 5000);
    if (fresh) { issuedAt.current = 0; app.toast(t('已连接：{name}', { name: fresh.name })); nav('/settings/devices'); }
  }, [devices.data, pc, app, nav]);

  const code = pc ? `${pc.code.slice(0, 4)} ${pc.code.slice(4)}` : '';
  return (
    <>
      <Typography variant="h4">{t('连接手机')}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5 }}>
        {viewer
          ? t('用手机 App（2.0 及以上）扫描这个二维码。你在这个空间是“只看”，手机上可以查看和搜索文档，不能上传。')
          : t('用手机 App 扫描这个二维码。App 2.0 连接后可以在手机上按你的权限查看、搜索文档；更早的版本只能上传。不能在手机上改密码或管理成员。')}
      </Typography>
      {create.isError && !pc ? (
        <ErrorState onRetry={regen} text={create.error.message} />
      ) : (
        <Card sx={{ p: 2.5, display: 'flex', gap: 3, flexWrap: 'wrap', alignItems: 'flex-start', maxWidth: 720 }}>
          <Box sx={{ position: 'relative', width: 200, height: 200, p: 1.5, bgcolor: '#fff', border: 1, borderColor: 'divider', borderRadius: 2, flex: 'none' }}>
            {pc ? <QrCode text={pc.qr} faded={!left} /> : <Skeleton variant="rectangular" width="100%" height="100%" />}
            {pc && !left && (
              <Stack spacing={1} sx={{ alignItems: 'center', justifyContent: 'center', position: 'absolute', inset: 0, color: '#1F2328' }}>
                <Typography variant="body2">{t('二维码已过期')}</Typography>
                <Button variant="contained" size="small" onClick={regen} disabled={create.isPending}>{t('重新生成')}</Button>
              </Stack>
            )}
          </Box>
          <Stack spacing={2} sx={{ minWidth: 0, flex: 1 }}>
            <Box>
              <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('配对码（手动连接时输入）')}</Typography>
              <Typography sx={{ fontFamily: fontMono, fontSize: 24, letterSpacing: '0.08em', opacity: left ? 1 : 0.4 }} aria-label={t('配对码')}>{code || '—'}</Typography>
            </Box>
            {left > 0 && (
              <Box>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('有效时间')}</Typography>
                <Typography variant="h5">{String(Math.floor(left / 60)).padStart(2, '0')}:{String(left % 60).padStart(2, '0')}</Typography>
              </Box>
            )}
            {pc && (
              <Box>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('连接到')}</Typography>
                <Typography variant="body2">{viewer ? t('“{name}”空间', { name: pc.workspaceName }) : t('“{name}”空间，上传的文档进入这个空间的待整理', { name: pc.workspaceName })}</Typography>
              </Box>
            )}
            {pc && (
              <Box>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('服务器地址（手机按顺序尝试）')}</Typography>
                {pc.urls.map((u) => <Typography key={u} variant="body2" sx={{ fontFamily: fontMono, fontSize: 13, overflowWrap: 'anywhere' }}>{u}</Typography>)}
                {pc.urls.every(isLocal) && (
                  <Typography variant="caption" sx={{ color: 'warning.main', display: 'block', mt: 0.5 }}>
                    {t('这是本机地址，手机无法访问。请用局域网地址打开网页端，或让管理员配置 KEPT_PUBLIC_URLS。')}
                  </Typography>
                )}
              </Box>
            )}
            <Box><Button variant="outlined" startIcon={<RefreshCw size={16} strokeWidth={SW} />} onClick={regen} disabled={create.isPending}>{t('重新生成')}</Button></Box>
          </Stack>
        </Card>
      )}
    </>
  );
}

export function DevicesPanel() {
  const devices = useDevices();
  const revoke = useRevokeDevice();
  const app = useApp();
  const nav = useNavigate();
  const items = devices.data ?? [];
  return (
    <>
      <Typography variant="h4" sx={{ mb: 2 }}>{t('已连接的设备')}</Typography>
      {devices.isPending && <Stack spacing={1} aria-busy>{[100, 92].map((w) => <Skeleton key={w} variant="rounded" height={64} width={`${w}%`} />)}</Stack>}
      {devices.isError && <ErrorState onRetry={() => devices.refetch()} />}
      {devices.isSuccess && !items.length && (
        <EmptyState icon={<Smartphone size={40} strokeWidth={SW} />} title={t('还没有连接任何手机')} text={t('在“连接手机”中用 App 扫码配对。')} action={t('连接手机')} onAction={() => nav('/settings/pair')} />
      )}
      {!!items.length && (
        <Card>
          {items.map((d, i) => (
            <Stack key={d.id} direction="row" spacing={1.5} sx={{ alignItems: 'center', p: 2, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
              <Box sx={{ color: 'text.secondary', display: 'flex' }}><Smartphone size={20} strokeWidth={SW} /></Box>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <Typography variant="h6" sx={{ overflowWrap: 'anywhere' }}>{d.name}</Typography>
                  <Chip size="small" label={d.access === 'full' ? t('可查看文档') : t('仅上传')} />
                </Stack>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  {d.access === 'full' ? t('连接“{name}”', { name: d.workspaceName }) : t('上传到“{name}”', { name: d.workspaceName })} · {t('配对于 {time}', { time: dateTime(d.pairedAt) })} · {d.lastSeenAt
                    ? <Tooltip title={dateTime(d.lastSeenAt)}><span>{t('最后活跃 {time}', { time: relativeTime(d.lastSeenAt) })}</span></Tooltip>
                    : t('还没有使用过')}
                </Typography>
              </Box>
              <Button variant="outlined" size="small" disabled={revoke.isPending} onClick={() => app.confirm({
                title: t('断开“{name}”？', { name: d.name }), body: d.access === 'full' ? t('断开后，这台手机将无法再查看或上传文档。') : t('断开后，这台手机将无法再上传。'), verb: t('断开'),
                run: () => revoke.mutate(d.id, { onSuccess: () => { app.confirm(null); app.toast(t('已断开')); }, onError: (e) => app.toast(e.message) }),
              })}>{t('断开')}</Button>
            </Stack>
          ))}
        </Card>
      )}
    </>
  );
}
