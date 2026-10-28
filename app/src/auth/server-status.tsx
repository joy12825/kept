import { Box, Stack, Typography } from '@mui/material';
import { Server } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { SW } from '../components/icons';
import { api } from '../api/client';
import { t } from '../i18n';

/** 只读的服务器连接状态：/healthz 返回 200 视为已连接，其他状态码或网络异常视为无法连接。 */
export function useServerHealth() {
  return useQuery<boolean>({
    queryKey: ['healthz'],
    queryFn: async () => {
      try {
        const r = await api.GET('/healthz');
        return r.response.status === 200;
      } catch {
        return false;
      }
    },
    retry: false,
    refetchOnWindowFocus: false,
  });
}

export function ServerStatus() {
  const health = useServerHealth();
  const ok = health.data === true;
  const host = typeof window !== 'undefined' ? window.location.host : '';
  return (
    <Stack
      direction="row"
      spacing={1}
      sx={{
        alignItems: "center",
        height: 56,
        px: 3,
        color: 'text.secondary',
        fontSize: 12
      }}>
      <Server size={14} strokeWidth={SW} />
      <Typography component="span" sx={{ fontSize: 12 }}>{host}</Typography>
      <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, color: ok ? 'success.main' : 'error.main', fontSize: 12 }}>
        <Box component="span" sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: 'currentColor' }} />
        {ok ? t('已连接') : t('无法连接')}
      </Box>
    </Stack>
  );
}
