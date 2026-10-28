// 设置 → 版本与授权（v1.12，仅实例管理员）：当前版本、试用或到期、成员与共享空间的用量、实例编号、导入或移除许可证。
// 开源构建（v1.13.1）始终是免费版：写明是开源版，不显示导入许可证。
// 收费功能的设置页顶部用 EditionNotice 说明当前版本不包含它；已有的设置照常显示，数据不受影响。
import { useState, type ChangeEvent } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Alert, Box, Button, Card, Chip, Stack, TextField, Typography } from '@mui/material';
import { Copy, FileUp } from 'lucide-react';
import { editionLabel, FEATURE_EDITION, hasFeature, useEdition, useInstallLicense, useRemoveLicense, type EditionStatus, type Feature } from '../../api/edition';
import { useMe } from '../../api/session';
import { ErrorState, LoadingState } from '../../components/common';
import { t } from '../../i18n';
import { useApp } from '../../store/app-store';

/** 每个版本包含什么，按页面上的说法。 */
const INCLUDES = () => ({
  free: t('文档的收、理、找、提醒，书房，两步验证，备份与导出；最多 6 个成员、1 个共享空间'),
  pro: t('加上 AI、家人的私密文档与紧急访问、场景、外部存储与云端加密、邮件与远程收件、上传链接、工作流、分享包与共享集、水印；最多 15 个成员，共享空间不限'),
  team: t('再加上单点登录、配额与上传额度、名称与 Logo、迁移到 Postgres 的工具；成员按许可证'),
});

export function EditionPanel() {
  const q = useEdition();
  if (q.isPending) return <LoadingState />;
  if (q.isError) return <ErrorState text={q.error.message} onRetry={() => void q.refetch()} />;
  const ed = q.data;
  return (
    <Box sx={{ maxWidth: 720 }}>
      <Typography variant="h4" sx={{ mb: 3 }}>{t('版本与授权')}</Typography>
      <Current ed={ed} />
      <Usage ed={ed} />
      {ed.openSource ? <OpenSource /> : <LicenseForm ed={ed} />}
    </Box>
  );
}

function Current({ ed }: { ed: EditionStatus }) {
  const app = useApp();
  const remove = useRemoveLicense();
  const line = ed.overridden ? t('开发构建：由 KEPT_EDITION 指定')
    : ed.openSource ? t('开源构建：不含收费模块，始终是免费版')
    : ed.trial ? t('收费版试用，还剩 {n} 天', { n: ed.daysLeft })
    : ed.license && ed.expired ? t('许可证已于 {date} 到期，现在是免费版', { date: ed.license.expires ?? '' })
    : ed.license ? (ed.license.expires ? t('许可证有效期到 {date}（还剩 {n} 天）', { date: ed.license.expires, n: ed.daysLeft }) : t('许可证长期有效'))
    : ed.trialEndsAt ? t('试用已结束') : '';
  const ask = () => app.confirm({
    title: t('移除许可证？'),
    body: t('移除后回到免费版（试用期内仍是试用）。文档、外部存储上的文件和加密的文件都不受影响，照常能看、能下载、能导出；收费功能不能再新建和运行。'),
    verb: t('移除'),
    run: () => remove.mutate(undefined, { onSuccess: () => { app.confirm(null); app.toast(t('已移除许可证')); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });
  return (
    <Card variant="outlined" sx={{ p: 2.5, mb: 2.5 }}>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', mb: 1 }}>
        <Typography variant="h5">{editionLabel(ed.edition)}</Typography>
        {ed.openSource && <Chip size="small" variant="outlined" label={t('开源版')} />}
        {ed.trial && <Chip size="small" color="info" variant="outlined" label={t('试用')} />}
        {ed.expired && <Chip size="small" color="warning" variant="outlined" label={t('已到期')} />}
      </Stack>
      {line && <Typography sx={{ color: ed.warn || ed.expired ? 'warning.main' : 'text.secondary', mb: 1 }} data-testid="edition-line">{line}</Typography>}
      {ed.license && (
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
          {t('持有人：{holder} · 许可证编号：{id} · 签发于 {date}', { holder: ed.license.holder, id: ed.license.id, date: ed.license.issued })}
        </Typography>
      )}
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5 }}>{INCLUDES()[ed.edition]}</Typography>
      {ed.license && <Button size="small" color="error" sx={{ mt: 1.5 }} onClick={ask}>{t('移除许可证')}</Button>}
    </Card>
  );
}

function Usage({ ed }: { ed: EditionStatus }) {
  const app = useApp();
  const row = (label: string, u: EditionStatus['members']) => (
    <>
      <Typography sx={{ color: 'text.secondary' }}>{label}</Typography>
      <Typography sx={{ color: u.limit > 0 && u.used >= u.limit ? 'warning.main' : 'text.primary' }}>
        {u.limit > 0 ? `${u.used} / ${u.limit}` : t('{n}（不限）', { n: u.used })}
      </Typography>
    </>
  );
  const copy = async () => {
    try { await navigator.clipboard.writeText(ed.instance ?? ''); app.toast(t('已复制实例编号')); } catch { app.toast(t('复制失败，请手动选中复制')); }
  };
  return (
    <Card variant="outlined" sx={{ p: 2.5, mb: 2.5 }}>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'max-content auto', columnGap: 3, rowGap: 1.25, alignItems: 'center' }}>
        {row(t('成员'), ed.members)}
        {row(t('共享空间'), ed.sharedWorkspaces)}
        <Typography sx={{ color: 'text.secondary' }}>{t('实例编号')}</Typography>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <Typography sx={{ fontFamily: 'monospace' }} data-testid="instance-id">{ed.instance}</Typography>
          <Button size="small" startIcon={<Copy size={14} />} onClick={copy}>{t('复制')}</Button>
        </Stack>
      </Box>
      <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 1.5 }}>
        {ed.openSource ? t('超过上限时已有的成员和空间不受影响，只是不能再加。')
          : t('超过上限时已有的成员和空间不受影响，只是不能再加。申请许可证时请提供实例编号，许可证只在这台服务器上有效。')}
      </Typography>
    </Card>
  );
}

function LicenseForm({ ed }: { ed: EditionStatus }) {
  const app = useApp();
  const install = useInstallLicense();
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    setError(null);
    install.mutate(text, {
      onSuccess: (st) => { setText(''); app.toast(t('已导入，现在是{edition}', { edition: editionLabel(st.edition) })); },
      onError: (e) => setError(e.message),
    });
  };
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (file) setText((await file.text()).trim());
  };
  return (
    <Card variant="outlined" sx={{ p: 2.5 }}>
      <Typography variant="h6" sx={{ mb: 1.5 }}>{ed.license ? t('更换许可证') : t('导入许可证')}</Typography>
      <TextField label={t('许可证')} placeholder="KEPT1.…" multiline minRows={3} fullWidth value={text} onChange={(e) => setText(e.target.value)}
        slotProps={{ htmlInput: { 'aria-label': t('许可证'), spellCheck: false, style: { fontFamily: 'monospace', fontSize: 13 } } }} />
      {error && <Alert severity="error" sx={{ mt: 1.5 }}>{error}</Alert>}
      <Stack direction="row" spacing={1.5} sx={{ mt: 1.5 }}>
        <Button variant="contained" disabled={!text.trim() || install.isPending} onClick={submit}>{t('导入')}</Button>
        <Button component="label" startIcon={<FileUp size={16} />}>
          {t('选择许可证文件')}
          <input type="file" hidden accept=".txt,.lic,.license,text/plain" onChange={onFile} />
        </Button>
      </Stack>
      <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 1.5 }}>
        {t('许可证离线校验，不需要联网。到期后回到免费版，所有数据照常能看、能下载、能导出。')}
      </Typography>
    </Card>
  );
}

/** 开源构建：不能导入许可证，说明怎样用上收费功能。 */
function OpenSource() {
  return (
    <Card variant="outlined" sx={{ p: 2.5 }} data-testid="edition-open-source">
      <Typography variant="h6" sx={{ mb: 1 }}>{t('这是开源的免费版')}</Typography>
      <Typography sx={{ color: 'text.secondary' }}>
        {t('开源构建不含收费模块，不能导入许可证。要用收费版或企业版的功能，请换成官方发布的镜像或安装包：数据目录直接沿用，换好后在这里导入许可证。')}
      </Typography>
    </Card>
  );
}

/** 收费功能的设置页顶部：当前版本不包含它时说明一句；已有的设置照常显示。 */
export function EditionNotice({ feature }: { feature: Feature }) {
  const ed = useEdition().data;
  const isAdmin = !!useMe().data?.user.isAdmin;
  if (hasFeature(ed, feature)) return null;
  const need = FEATURE_EDITION[feature] === 'team' ? t('企业版') : t('收费版');
  if (ed?.openSource) {
    return (
      <Alert severity="info" sx={{ mb: 2.5 }} data-testid="edition-notice">
        {t('这是{edition}的功能，开源的免费版不包含：已有的设置和数据保留，可以查看和删除，不能新建或修改。', { edition: need })}
      </Alert>
    );
  }
  return (
    <Alert severity="info" sx={{ mb: 2.5 }} data-testid="edition-notice"
      action={isAdmin ? <Button color="inherit" size="small" component={RouterLink} to="/settings/edition">{t('版本与授权')}</Button> : undefined}>
      {t('这是{edition}的功能，当前版本不包含：已有的设置和数据保留，可以查看和删除，不能新建或修改。', { edition: need })}
      {!isAdmin && ' ' + t('请联系实例管理员升级。')}
    </Alert>
  );
}
