import React from 'react';
import { Box, Avatar } from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
  Home, Car, HeartPulse, Briefcase, Baby, IdCard, PawPrint, Wallet, Plane, BookOpen, Users, FileText, FolderKanban, Building2, CircleDashed, User, SlidersHorizontal, Sparkles,
} from 'lucide-react';
import type { DrawerColorName, DrawerIconName } from '../api/drawers';
import type { DrawerColor } from '../theme';
import { BRAND_MARK, useBranding } from '../api/branding';
import { t } from '../i18n';

export const SW = 1.75; // lucide 线宽

type IconName = DrawerIconName | 'dashed' | 'user' | 'sliders';
const ICONS: Record<IconName, React.ComponentType<any>> = {
  home: Home, car: Car, heart: HeartPulse, briefcase: Briefcase, baby: Baby, idcard: IdCard, paw: PawPrint, wallet: Wallet, plane: Plane,
  book: BookOpen, users: Users, file: FileText, kanban: FolderKanban, building: Building2, dashed: CircleDashed, user: User, sliders: SlidersHorizontal,
};
export function NamedIcon({ name, size = 18 }: { name: keyof typeof ICONS; size?: number }) {
  const C = ICONS[name] || FileText;
  return <C size={size} strokeWidth={SW} />;
}

// 抽屉图标：抽屉颜色 12% 浅底 + 同色图标；共享抽屉右下角带标记
export function DrawerIcon({ drawer, size = 20, shared }: { drawer?: { icon: DrawerIconName; color: DrawerColorName } | null; size?: number; shared?: boolean }) {
  const theme = useTheme();
  const c = drawer ? theme.palette.drawer[drawer.color] : theme.palette.drawer.gray;
  return (
    <Box component="span" sx={{ position: 'relative', width: size, height: size, flex: 'none', borderRadius: `${Math.round(size * 0.3)}px`, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', bgcolor: alpha(c, 0.12), color: c }}>
      <NamedIcon name={drawer ? drawer.icon : 'dashed'} size={Math.round(size * 0.58)} />
      {shared && (
        <Box component="span" title={t('共享')} sx={{ position: 'absolute', right: -3, bottom: -3, width: Math.max(10, size * 0.45), height: Math.max(10, size * 0.45), borderRadius: '50%', bgcolor: 'background.paper', color: 'text.secondary', boxShadow: `0 0 0 1px ${theme.palette.divider}`, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Users size={Math.max(7, Math.round(size * 0.3))} strokeWidth={2} />
        </Box>
      )}
    </Box>
  );
}

/** 产品 Logo：有自定义 Logo 时显示图片；没改过名时是收好的标志（v1.13.6）；改过名、没有 Logo 时是名称的第一个字。 */
export function BrandMark({ size = 28 }: { size?: number }) {
  const { name, logoUrl, isDefault } = useBranding();
  const r = Math.round(size * 0.28);
  const src = logoUrl ?? (isDefault ? BRAND_MARK : undefined);
  if (src) return <Box component="img" src={src} alt={name} sx={{ width: size, height: size, borderRadius: `${r}px`, objectFit: 'cover', flex: 'none' }} />;
  return (
    <Box sx={{ width: size, height: size, flex: 'none', borderRadius: `${r}px`, bgcolor: 'primary.main', color: 'primary.contrastText', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: size * 0.5, fontWeight: 600, lineHeight: 1 }}>
      {name.trim().slice(0, 1) || t('收好').slice(0, 1)}
    </Box>
  );
}

/** 空间头像：名称的第一个字，底色取空间的颜色。 */
export function SpaceAvatar({ name, color, size = 28 }: { name: string; color: DrawerColor; size?: number }) {
  const theme = useTheme();
  return <Avatar variant="rounded" sx={{ width: size, height: size, fontSize: size * 0.46, fontWeight: 600, bgcolor: theme.palette.drawer[color], color: '#fff', borderRadius: `${Math.round(size * 0.25)}px` }}>{name.trim().slice(0, 1)}</Avatar>;
}

/** AI 标记（UI 设计规范 5.12）：12px 强调色 sparkles，只标识 AI 产生的内容。 */
export function AiMark({ label = t('AI 生成') }: { label?: string }) {
  return (
    <Box component="span" role="img" aria-label={label} title={label} sx={{ display: 'inline-flex', verticalAlign: 'middle', color: 'primary.main', ml: 0.5 }}>
      <Sparkles size={12} strokeWidth={SW} />
    </Box>
  );
}

export function TagDot({ name, color }: { name: string; color: string }) {
  return (
    <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: '5px', fontSize: 12, whiteSpace: 'nowrap' }}>
      <Box component="span" sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: color }} />{name}
    </Box>
  );
}

export function Thumb({ w = 32, h = 42 }: { w?: number; h?: number }) {
  return (
    <Box sx={{ width: w, height: h, flex: 'none', display: 'flex', flexDirection: 'column', gap: '3px', p: '7px 5px', bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: '4px' }}>
      {[70, 100, 100, 55].map((x, i) => <Box key={i} sx={{ height: 2, width: `${x}%`, bgcolor: 'divider' }} />)}
    </Box>
  );
}
