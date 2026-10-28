// 快捷键说明（v1.5.9，参照 paperless-ngx）：按 ? 打开，设置 → 界面里也有入口。列表与各页的按键处理保持一致。
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Typography } from '@mui/material';
import { useApp } from '../store/app-store';
import { t } from '../i18n';

const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
const MOD = mac ? '⌘' : 'Ctrl';

export const SHORTCUTS: { group: string; keys: [string[], string][] }[] = [
  { group: t('全局'), keys: [
    [[MOD, 'K'], t('搜索文档、跳转到页面')], [['G', 'H'], t('首页')], [['G', 'I'], t('待整理')], [['G', 'F'], t('全部文件')], [['G', 'R'], t('年度回顾')],
    [['U'], t('上传')], [['['], t('收起或展开侧栏')], [['?'], t('快捷键说明')],
  ] },
  { group: t('文件列表'), keys: [
    [['/'], t('搜索')], [['J'], t('下一份')], [['K'], t('上一份')], [['X'], t('选中当前这份')], [['Shift', t('点选')], t('选中一段')], [['Enter'], t('打开')], [['Esc'], t('取消选择')],
  ] },
  { group: t('文档详情'), keys: [
    [['J'], t('下一份')], [['K'], t('上一份')], [['V'], t('切换预览方式')], [['T'], t('改标签')], [['Y'], t('改类型')], [['C'], t('改往来方')],
    [[MOD, 'F'], t('在预览里查找')], [['Esc'], t('返回列表')],
  ] },
  { group: t('文档预览（详情页）'), keys: [
    [['+'], t('放大')], [['−'], t('缩小')], [['0'], t('适合宽度')], [['R'], t('旋转查看')], [['P'], t('PDF 的页面和目录')], [['F'], t('全屏')],
  ] },
  { group: t('待整理'), keys: [
    [['A'], t('完成整理')], [['S'], t('跳过')], [['J'], t('下一份')], [['K'], t('上一份')], [['V'], t('切换预览方式')], [['T'], t('改标签')], [['Y'], t('改类型')], [['C'], t('改往来方')],
  ] },
];

function Key({ k }: { k: string }) {
  return <Box component="kbd" sx={{ display: 'inline-block', minWidth: 22, px: 0.75, py: 0.125, border: 1, borderColor: 'divider', borderBottomWidth: 2, borderRadius: 1, fontFamily: 'inherit', fontSize: 12, textAlign: 'center', bgcolor: 'background.paper' }}>{k}</Box>;
}

export function ShortcutsDialog() {
  const app = useApp();
  const close = () => app.set({ shortcutsOpen: false });
  return (
    <Dialog open={app.s.shortcutsOpen} onClose={close} fullWidth maxWidth="md" aria-labelledby="shortcuts-title">
      <DialogTitle id="shortcuts-title">{t('快捷键')}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 3 }}>
          {SHORTCUTS.map((g) => (
            <Box key={g.group} component="section" aria-label={g.group}>
              <Typography variant="h6" sx={{ mb: 1 }}>{g.group}</Typography>
              {g.keys.map(([keys, what]) => (
                <Box key={what + keys.join()} sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 0.5 }}>
                  <Typography variant="body2" sx={{ flex: 1 }}>{what}</Typography>
                  <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
                    {keys.map((k, i) => <Key key={i} k={k} />)}
                  </Box>
                </Box>
              ))}
            </Box>
          ))}
        </Box>
        <Typography variant="caption" component="p" sx={{ color: 'text.secondary', mt: 2 }}>{t('在输入框里打字时，这些按键不起作用（{mod} K 除外）。G H 这类是先按 G，再按 H。', { mod: MOD })}</Typography>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}><Button onClick={close}>{t('关闭')}</Button></DialogActions>
    </Dialog>
  );
}
