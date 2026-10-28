import { Box, Button, Skeleton, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { Inbox, RefreshCw, TriangleAlert } from 'lucide-react';
import React from 'react';
import { SW } from './icons';
import { t } from '../i18n';

export function EmptyState({ icon, title, text, action, onAction }: { icon?: React.ReactNode; title: string; text?: string; action?: string; onAction?: () => void }) {
  return (
    <Stack
      spacing={1}
      sx={{
        alignItems: "center",
        py: 12,
        px: 3,
        textAlign: 'center',
        maxWidth: 440,
        mx: 'auto'
      }}>
      <Box sx={{ color: 'text.secondary', mb: 1 }}>{icon || <Inbox size={40} strokeWidth={SW} />}</Box>
      <Typography variant="h5">{title}</Typography>
      {text && <Typography sx={{
        color: "text.secondary"
      }}>{text}</Typography>}
      {action && <Box sx={{
        pt: 1.5
      }}><Button variant="contained" onClick={onAction}>{action}</Button></Box>}
    </Stack>
  );
}

/** 出错：没有 text 时按连不上服务器处理；有 text 时显示服务器给出的原因。 */
export function ErrorState({ onRetry, text }: { onRetry: () => void; text?: string }) {
  const icon = <TriangleAlert size={40} strokeWidth={SW} />;
  return text
    ? <EmptyState icon={icon} title={t('加载失败')} text={text} action={t('重新加载')} onAction={onRetry} />
    : <EmptyState icon={icon} title={t('无法连接到服务器')} text={t('请检查网络连接，或确认服务器正在运行，然后重新加载。')} action={t('重新加载')} onAction={onRetry} />;
}

export function LoadingState() {
  return (
    <Stack spacing={2} sx={{ px: 3, py: 1 }} aria-busy>
      <Skeleton variant="rounded" width={260} height={32} />
      <Skeleton variant="rounded" width={380} height={20} />
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(240px,1fr))', gap: 2, mt: 1 }}>
        {[0, 1, 2, 3].map((i) => <Skeleton key={i} variant="rounded" height={140} sx={{ borderRadius: 3 }} />)}
      </Box>
      {[100, 85, 70].map((w) => <Skeleton key={w} variant="rounded" height={56} width={`${w}%`} />)}
    </Stack>
  );
}

export function RetryIcon() { return <RefreshCw size={16} strokeWidth={SW} />; }
