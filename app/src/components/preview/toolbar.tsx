// 预览器的底部浮条（v1.9.1 从 PDF 预览里拿出来，图片查看器共用）：圆角胶囊，浮在预览区底部正中，高度 0 不占位置。
import { useEffect, useState } from 'react';
import { Box, IconButton, Stack, Tooltip, Typography } from '@mui/material';
import type { LucideIcon } from 'lucide-react';
import { SW } from '../icons';
import { t } from '../../i18n';

export function FloatingBar({ children }: { children: React.ReactNode }) {
  return (
    <Box sx={{ position: 'sticky', bottom: 16, zIndex: 2, height: 0, display: 'flex', justifyContent: 'center', alignItems: 'flex-end', pointerEvents: 'none' }}>
      <Stack direction="row" role="toolbar" aria-label={t('预览工具')} sx={{ pointerEvents: 'auto', alignItems: 'center', height: 36, px: 0.5, borderRadius: 999, bgcolor: 'background.paper', border: 1, borderColor: 'divider', boxShadow: 2 }}>
        {children}
      </Stack>
    </Box>
  );
}

/** 浮条上的按钮：图标 16px，悬停显示名称和快捷键 */
export function BarButton({ icon: Icon, label, keys, onClick, disabled, active }: {
  icon: LucideIcon; label: string; keys?: string; onClick: () => void; disabled?: boolean; active?: boolean;
}) {
  return (
    <Tooltip title={keys ? `${label}（${keys}）` : label}>
      <span>
        <IconButton size="small" aria-label={label} aria-pressed={active} disabled={disabled} onClick={onClick}
          sx={active ? { color: 'primary.main', bgcolor: 'action.selected' } : undefined}>
          <Icon size={16} strokeWidth={SW} />
        </IconButton>
      </span>
    </Tooltip>
  );
}

export const BarDivider = () => <Box sx={{ width: '1px', height: 18, bgcolor: 'divider', mx: 0.25 }} />;

export const BarText = ({ children, width, live }: { children: React.ReactNode; width?: number; live?: boolean }) => (
  <Typography aria-live={live ? 'polite' : undefined} sx={{ fontSize: 12, px: width ? 0 : 1.25, width, textAlign: 'center', color: 'text.secondary', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
    {children}
  </Typography>
);

/** 全屏：把 el 放大到整个屏幕；返回是否全屏和切换函数。 */
export function useFullscreen(el: React.RefObject<HTMLElement | null>) {
  const [on, setOn] = useState(false);
  useEffect(() => {
    const h = () => setOn(!!el.current && document.fullscreenElement === el.current);
    document.addEventListener('fullscreenchange', h);
    return () => document.removeEventListener('fullscreenchange', h);
  }, [el]);
  const toggle = () => {
    if (document.fullscreenElement) void document.exitFullscreen?.();
    else void el.current?.requestFullscreen?.().catch(() => { /* 浏览器不让全屏时什么也不做 */ });
  };
  return { on, toggle, supported: typeof document !== 'undefined' && !!document.fullscreenEnabled };
}

/** 预览器的快捷键（+ − 0 R F P 等）：输入框里、带修饰键、有对话框时不响应。 */
export function usePreviewKeys(enabled: boolean, map: Record<string, () => void>) {
  useEffect(() => {
    if (!enabled) return;
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey || e.altKey) return;
      if (document.querySelector('.MuiDialog-root, .MuiPopover-root, .MuiMenu-root')) return;
      const k = e.key === '=' ? '+' : e.key === '_' ? '-' : e.key.toLowerCase();
      const run = map[k];
      if (run) { e.preventDefault(); run(); }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  });
}
