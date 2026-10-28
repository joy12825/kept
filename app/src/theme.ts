// MUI 主题：Material UI v9，视觉按《文档管理系统UI设计规范》的“清爽现代”：白底、中性灰、青蓝强调色 #2563EB、系统字体、6–16px 圆角、细边框
import { createTheme, alpha, type Theme } from '@mui/material/styles';
import { enUS, zhCN } from '@mui/material/locale';
import { currentLang } from './i18n';

export type DrawerColor = 'blue' | 'cyan' | 'green' | 'orange' | 'red' | 'purple' | 'pink' | 'gray';

export interface SurfacePalette {
  main: string;
  containerLowest: string;
  containerLow: string;
  container: string;
  containerHigh: string;
  onSurface: string;
  onSurfaceVariant: string;
  outline: string;
  outlineVariant: string;
  secondaryContainer: string;
  onSecondaryContainer: string;
  inverseSurface: string;
  inverseOnSurface: string;
  inversePrimary: string;
}

declare module '@mui/material/styles' {
  interface Palette { surface: SurfacePalette; drawer: Record<DrawerColor, string> }
  interface PaletteOptions { surface?: SurfacePalette; drawer?: Record<DrawerColor, string> }
}
declare module '@mui/material/Button' {
  interface ButtonPropsVariantOverrides { tonal: true }
}

// 取值见《文档管理系统UI设计规范》第三部分“设计令牌”。surface.* 沿用原型的命名，页面代码不用改：
// main 内容区背景、containerLow 侧边栏与浅底区块、container 卡片与浮层、containerHigh 悬停与次要按钮底色、
// outline 输入框边框、outlineVariant 分割线与卡片边框、secondaryContainer 强调浅底（选中项）。
const TOKENS = {
  light: {
    primary: '#2563EB', onPrimary: '#FFFFFF',
    surface: {
      main: '#FFFFFF', containerLowest: '#FFFFFF', containerLow: '#F7F8FA', container: '#FFFFFF', containerHigh: '#F2F3F5',
      onSurface: '#1F2328', onSurfaceVariant: '#636B78', outline: '#D0D5DD', outlineVariant: '#E5E7EB',
      secondaryContainer: '#EEF3FE', onSecondaryContainer: '#1D4ED8',
      inverseSurface: '#1F2328', inverseOnSurface: '#FFFFFF', inversePrimary: '#93B4FF',
    } as SurfacePalette,
    success: '#1A7F37', warning: '#9A6700', error: '#CF222E',
    drawer: { blue: '#2563EB', cyan: '#0E7490', green: '#15803D', orange: '#C2410C', red: '#B91C1C', purple: '#7C3AED', pink: '#BE185D', gray: '#4B5563' },
    shadowHover: '0 1px 3px rgb(0 0 0 / 0.08)',
    shadowPopover: '0 8px 24px rgb(0 0 0 / 0.12)',
  },
  dark: {
    primary: '#5B9BFF', onPrimary: '#0B1220',
    surface: {
      main: '#0F1115', containerLowest: '#0F1115', containerLow: '#15181D', container: '#1B1F24', containerHigh: '#22272E',
      onSurface: '#E6E8EB', onSurfaceVariant: '#9BA3AE', outline: '#3A404A', outlineVariant: '#2A2F36',
      secondaryContainer: '#17243A', onSecondaryContainer: '#A8C7FF',
      inverseSurface: '#E6E8EB', inverseOnSurface: '#0F1115', inversePrimary: '#2563EB',
    } as SurfacePalette,
    success: '#3FB950', warning: '#D29922', error: '#F85149',
    // 深色下提高亮度，使其与卡片底色（#1B1F24）的对比度不低于 3:1
    drawer: { blue: '#7AA7FF', cyan: '#3BC3D6', green: '#56C77A', orange: '#FF9A5C', red: '#FF7B7B', purple: '#B794F6', pink: '#F27CB3', gray: '#A0AAB8' },
    shadowHover: '0 1px 3px rgb(0 0 0 / 0.5)',
    shadowPopover: '0 8px 24px rgb(0 0 0 / 0.5)',
  },
};

// 使用系统字体，不打包字体文件（规范 3.2）
export const fontSans = ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', '"PingFang SC"', '"HarmonyOS Sans SC"', '"Microsoft YaHei"', '"Noto Sans SC"', 'sans-serif'].join(',');
export const fontMono = ['ui-monospace', '"SF Mono"', '"JetBrains Mono"', 'Consolas', 'monospace'].join(',');

export function buildTheme(mode: 'light' | 'dark') {
  const t = TOKENS[mode];
  const S = t.surface;
  const focusRing = `0 0 0 3px ${alpha(t.primary, mode === 'light' ? 0.18 : 0.35)}`;
  return createTheme({
    palette: {
      mode,
      primary: { main: t.primary, contrastText: t.onPrimary },
      secondary: { main: S.onSecondaryContainer, light: S.secondaryContainer, contrastText: S.secondaryContainer },
      success: { main: t.success }, warning: { main: t.warning }, error: { main: t.error },
      background: { default: S.main, paper: S.container },
      text: { primary: S.onSurface, secondary: S.onSurfaceVariant },
      divider: S.outlineVariant,
      action: { hover: alpha(S.onSurface, 0.05), selected: S.secondaryContainer, focus: alpha(S.onSurface, 0.1) },
      surface: S,
      drawer: t.drawer,
    },
    shape: { borderRadius: 6 },
    shadows: ['none', t.shadowHover, t.shadowPopover, ...Array(22).fill(t.shadowPopover)] as any,
    typography: {
      fontFamily: fontSans,
      // 规范 3.2 的字号阶梯：display 48/56、2xl 28/36、xl 22/30、lg 18/26、md 16/24、base 14/22、sm 13/20、xs 12/18
      h1: { fontSize: 48, lineHeight: '56px', fontWeight: 700 },
      h2: { fontSize: 28, lineHeight: '36px', fontWeight: 700 },
      h3: { fontSize: 22, lineHeight: '30px', fontWeight: 600 },
      h4: { fontSize: 22, lineHeight: '30px', fontWeight: 600 },
      h5: { fontSize: 16, lineHeight: '24px', fontWeight: 500 },
      h6: { fontSize: 14, lineHeight: '22px', fontWeight: 600 },
      subtitle1: { fontSize: 16, lineHeight: '24px', fontWeight: 500 },
      subtitle2: { fontSize: 14, lineHeight: '22px', fontWeight: 500 },
      body1: { fontSize: 14, lineHeight: '22px' },
      body2: { fontSize: 13, lineHeight: '20px' },
      caption: { fontSize: 12, lineHeight: '18px' },
      button: { fontSize: 14, lineHeight: '20px', fontWeight: 500, textTransform: 'none' },
    },
    components: {
      MuiCssBaseline: {
        styleOverrides: {
          body: { fontVariantNumeric: 'tabular-nums', WebkitFontSmoothing: 'antialiased' },
          '@media (prefers-reduced-motion: reduce)': { '*': { animationDuration: '1ms !important', transitionDuration: '1ms !important' } },
        },
      },
      MuiButtonBase: { defaultProps: { disableRipple: true } },
      MuiButton: {
        defaultProps: { disableElevation: true },
        styleOverrides: {
          root: {
            borderRadius: 6, height: 36, paddingInline: 16, whiteSpace: 'nowrap',
            '&.Mui-focusVisible': { boxShadow: focusRing },
          },
          sizeSmall: { height: 32, paddingInline: 12, fontSize: 13 },
          sizeLarge: { height: 40, paddingInline: 20 },
          outlined: { borderColor: S.outline, color: S.onSurface, backgroundColor: S.container, '&:hover': { backgroundColor: S.containerHigh, borderColor: S.outline } },
          text: { paddingInline: 10 },
        },
        variants: [
          // 次要按钮：浅灰底、正文色文字（规范中的 secondary）
          { props: { variant: 'tonal' }, style: { backgroundColor: S.containerHigh, color: S.onSurface, '&:hover': { backgroundColor: alpha(S.onSurface, 0.1) } } },
        ],
      },
      MuiIconButton: {
        styleOverrides: { root: { color: S.onSurfaceVariant, borderRadius: 6, '&.Mui-focusVisible': { boxShadow: focusRing } } },
      },
      MuiToggleButtonGroup: { styleOverrides: { root: { borderRadius: 6 } } },
      MuiToggleButton: {
        styleOverrides: {
          root: {
            height: 32, paddingInline: 12, borderColor: S.outline, color: S.onSurfaceVariant, textTransform: 'none', fontWeight: 500, gap: 6,
            '&.Mui-selected': { backgroundColor: S.containerHigh, color: S.onSurface, '&:hover': { backgroundColor: S.containerHigh } },
            '&:first-of-type': { borderTopLeftRadius: 6, borderBottomLeftRadius: 6 },
            '&:last-of-type': { borderTopRightRadius: 6, borderBottomRightRadius: 6 },
          },
        },
      },
      MuiChip: {
        styleOverrides: {
          // 筛选胶囊 6px 圆角；小号用作徽章，全圆角（规范 3.3）
          root: { height: 28, borderRadius: 6, fontWeight: 500, fontSize: 13 },
          sizeSmall: { height: 22, borderRadius: 999, fontSize: 12 },
          outlined: { borderColor: S.outline },
          filled: { backgroundColor: S.containerHigh, color: S.onSurface },
        },
      },
      MuiCard: {
        defaultProps: { variant: 'outlined' },
        styleOverrides: { root: { borderRadius: 12, borderColor: S.outlineVariant, backgroundImage: 'none' } },
      },
      MuiPaper: { styleOverrides: { root: { backgroundImage: 'none' }, rounded: { borderRadius: 12 } } },
      MuiDialog: { styleOverrides: { paper: { borderRadius: 16, backgroundColor: S.container, boxShadow: t.shadowPopover } } },
      MuiDialogTitle: { styleOverrides: { root: { fontSize: 18, lineHeight: '26px', fontWeight: 600 } } },
      MuiMenu: { styleOverrides: { paper: { borderRadius: 8, border: `1px solid ${S.outlineVariant}`, backgroundColor: S.container, boxShadow: t.shadowPopover } } },
      MuiMenuItem: { styleOverrides: { root: { minHeight: 36, fontSize: 14, borderRadius: 4, marginInline: 4 } } },
      MuiPopover: { styleOverrides: { paper: { borderRadius: 8, border: `1px solid ${S.outlineVariant}`, backgroundColor: S.container, boxShadow: t.shadowPopover } } },
      MuiListItemButton: {
        styleOverrides: {
          root: {
            borderRadius: 6, minHeight: 36, paddingInline: 10,
            '&.Mui-selected': { backgroundColor: S.secondaryContainer, color: S.onSecondaryContainer, '&:hover': { backgroundColor: S.secondaryContainer } },
          },
        },
      },
      MuiListItemIcon: { styleOverrides: { root: { minWidth: 30, color: 'inherit' } } },
      // 输入框与 MUI 官方 Text Field 一致（https://mui.com/material-ui/react-text-field/）：文字 16px，高 56px（small 为 40px），
      // 标签上浮到边框缺口处并缩为 12px，聚焦时边框加粗为 2px 强调色。不加底色和外发光——它们会和上浮的标签重叠。
      // 正文是 14px，而 MUI 的标签位移和 0.75 倍缩放都按 16px 设计，所以输入框和标签单独用 16px。
      MuiOutlinedInput: {
        styleOverrides: {
          root: { fontSize: 16, '&:hover:not(.Mui-focused):not(.Mui-error):not(.Mui-disabled) .MuiOutlinedInput-notchedOutline': { borderColor: alpha(S.onSurface, 0.3) } },
          notchedOutline: { borderColor: S.outline },
        },
      },
      MuiInputLabel: { styleOverrides: { root: { fontSize: 16 } } },
      MuiTextField: { defaultProps: { size: 'small' } },
      MuiSelect: { defaultProps: { size: 'small' } },
      MuiAlert: { styleOverrides: { root: { borderRadius: 8 } } },
      MuiSnackbarContent: {
        styleOverrides: { root: { backgroundColor: S.inverseSurface, color: S.inverseOnSurface, borderRadius: 8 }, action: { color: S.inversePrimary } },
      },
      MuiLinearProgress: { styleOverrides: { root: { height: 4, borderRadius: 2, backgroundColor: S.containerHigh } } },
      MuiTooltip: { styleOverrides: { tooltip: { backgroundColor: S.inverseSurface, color: S.inverseOnSurface, fontSize: 12, borderRadius: 6 } } },
      MuiTableCell: { styleOverrides: { root: { borderColor: S.outlineVariant }, head: { fontSize: 12, fontWeight: 500, color: S.onSurfaceVariant } } },
      MuiAccordion: { defaultProps: { disableGutters: true, elevation: 0 }, styleOverrides: { root: { background: 'transparent', '&:before': { display: 'none' } } } },
      MuiDrawer: { styleOverrides: { paper: { backgroundColor: S.containerLow, backgroundImage: 'none' } } },
    },
  });
}

/** MUI 自带的文字（“没有选项”、分页等）跟随界面语言（v1.10.0）。 */
export function localized(theme: Theme) {
  return createTheme(theme, currentLang() === 'en' ? enUS : zhCN);
}
