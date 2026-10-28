// 分享保护（v1.9.2）：水印文字和“允许下载”，单份分享和分享包的对话框共用。
import { FormControlLabel, Stack, Switch, TextField, Typography } from '@mui/material';
import { useMe } from '../api/session';
import { hasFeature, useEdition } from '../api/edition';
import { t } from '../i18n';

export interface ProtectValue { watermark: string; allowDownload: boolean }
export const NO_PROTECT: ProtectValue = { watermark: '', allowDownload: true };

/** 请求里的两项：空水印不传 */
export const protectBody = (v: ProtectValue) => ({ ...(v.watermark.trim() ? { watermark: v.watermark.trim() } : {}), allowDownload: v.allowDownload });

export function ShareProtectFields({ value, onChange, noPDF }: {
  value: ProtectValue; onChange: (v: ProtectValue) => void;
  /** 这份文档没有 PDF 或图片，不能加水印（单份分享） */
  noPDF?: boolean;
}) {
  const serverCan = useMe().data?.features?.watermark ?? false;
  // 水印是收费版的分享增强（v1.13.3）；开源构建写明不包含
  const ed = useEdition().data;
  const paid = hasFeature(ed, 'sharing');
  const disabled = !paid || !serverCan || noPDF;
  const help = !paid ? (ed?.openSource ? t('开源的免费版不包含水印') : t('加水印是收费版的功能'))
    : !serverCan ? t('服务器缺少中文字体，暂时不能加水印，请联系管理员')
    : noPDF ? t('这份文档没有 PDF 或图片，不能加水印')
      : t('斜着铺一层浅色文字，例如“仅供某某银行办理房贷使用”。分享图片原件时仍给图片，其余给 PDF');
  return (
    <Stack spacing={1}>
      <TextField label={t('水印文字（可不填）')} value={value.watermark} disabled={disabled} onChange={(e) => onChange({ ...value, watermark: e.target.value })}
        helperText={help} slotProps={{ htmlInput: { maxLength: 40 } }} />
      <FormControlLabel control={<Switch checked={value.allowDownload} onChange={(e) => onChange({ ...value, allowDownload: e.target.checked })} />}
        label={t('允许下载')} sx={{ alignSelf: 'flex-start', ml: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', mt: '-4px !important' }}>
        {value.allowDownload
          ? t('对方可以在网页上看，也可以下载')
          : t('对方只能在网页上看，没有下载按钮。看的人仍然可以截图，要防外传请同时加水印')}
      </Typography>
    </Stack>
  );
}

/** 列表里的说明：“加了水印 · 只能在线看” */
export function protectNote(p: { watermark?: string; allowDownload: boolean }): string {
  return [p.watermark ? t('水印“{text}”', { text: p.watermark }) : '', p.allowDownload ? '' : t('只能在线看')].filter(Boolean).join(' · ');
}
