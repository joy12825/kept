import { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Chip, List, ListItemButton, ListItemText, Stack, Tab, Tabs, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { useApp } from '../store/app-store';
import { PasswordForm, ProfileForm } from './settings/account';
import { TwoFactorPanel } from './settings/two-factor';
import { SSOStatus, BrandingPanel, UploadLinksPanel, EmergencyContactsPanel, MyEmergencyAccessPanel, ScenesPanel, MailPanel, AiPanel, WorkflowsPanel, RemoteInboxPanel, CollectionsPanel } from '@ee';
import { TokensPanel } from './settings/tokens';
import { AccountsPanel } from './settings/accounts';
import { MembersPanel } from './settings/members';
import { ActivityPanel } from './settings/activity';
import { ClassifierPanel } from './settings/classifier';
import { LogsPanel, SystemPanel } from './settings/system';
import { DevicesPanel, PairPanel } from './settings/devices';
import { MyPrivacyPanel } from './settings/private';
import { NotificationsPanel } from './settings/notifications';
import { InstanceStoragePanel, StoragePanel } from './settings/storage';
import { ShareLinksPanel } from './settings/share-links';
import { EditionNotice, EditionPanel } from './settings/edition';
import { FEATURE_EDITION, hasFeature, useEdition, type Feature } from '../api/edition';
import type { WorkspaceRole } from '../api/client';
import { useMe, useSetLocale } from '../api/session';
import { t, tc, type LangPreference } from '../i18n';

/** 设置的分组：个人的设置、当前空间的设置、实例管理。owner 只给当前空间的管理员，editor 给成员和管理员，admin 只给实例管理员。 */
type Item = [key: string, label: string, only?: 'owner' | 'editor' | 'admin'];
const GROUPS: { key: 'me' | 'space' | 'instance'; items: Item[] }[] = [
  { key: 'me', items: [['profile', t('个人资料')], ['password', t('密码与安全')], ['notify', t('通知')], ['ui', t('界面')], ['token', t('API Token')], ['pair', t('连接手机')], ['devices', t('已连接的设备')], ['emergency-access', t('紧急访问')]] },
  { key: 'space', items: [['members', t('空间成员')], ['private', t('我的私密')], ['activity', t('活动记录'), 'owner'], ['storage', t('存储')], ['classifier', t('自动分类')], ['scenes', t('场景模板')], ['ai', t('AI 与智能体')], ['workflows', t('工作流'), 'owner'], ['mail', t('邮件收取'), 'owner'], ['sharing', t('对外共享'), 'owner'], ['links', tc('复数', '分享链接'), 'editor'], ['upload-links', tc('复数', '上传链接'), 'editor'], ['remote-inbox', t('远程收件夹'), 'owner'], ['emergency', t('紧急联系人'), 'owner']] },
  { key: 'instance', items: [['accounts', t('账号管理'), 'admin'], ['admin-activity', t('账号活动'), 'admin'], ['brand', t('名称与 Logo'), 'admin'], ['storages', t('文件安全'), 'admin'], ['edition', t('版本与授权'), 'admin'], ['system', t('系统状态'), 'admin'], ['logs', t('日志'), 'admin']] },
];

type Who = { isAdmin: boolean; role: WorkspaceRole | undefined };

/** 命令面板里除了名称也认的词（v1.13.9）：熟悉存储的人搜原来的名字或技术名词，也能找到改了名的设置项。 */
export const SETTINGS_KEYWORDS: Record<string, string> = {
  storages: [t('存储管理'), t('存储位置'), t('备份'), t('冷热分层'), t('可读副本'), t('云端加密'), t('换存储'), t('各空间的存储'), t('配额'), 'S3 WebDAV MinIO OSS COS NAS'].join(' '),
  storage: [t('存储'), t('配额'), t('旧版本'), t('导出')].join(' '),
};

/** 属于收费版、企业版的设置项（v1.12）：当前版本不包含时，菜单上标出，页面顶部说明。 */
const PAID: Record<string, Feature> = {
  ai: 'ai', scenes: 'scenes', workflows: 'workflows', mail: 'mail', sharing: 'sharing', 'upload-links': 'uploadLinks',
  'remote-inbox': 'mail', emergency: 'privacy', brand: 'branding',
};

/** 当前用户能看到的设置分组（空的分组不出现）。 */
export function settingsGroups({ isAdmin, role }: Who): { key: string; items: [string, string][] }[] {
  return GROUPS.map((g) => ({
    key: g.key,
    items: g.items.filter(([, , only]) => !only || (only === 'owner' ? role === 'owner' : only === 'editor' ? role === 'owner' || role === 'editor' : isAdmin)).map(([k, l]): [string, string] => [k, l]),
  })).filter((g) => g.items.length > 0);
}

/** 当前用户能看到的设置页签：活动记录、工作流等只给当前空间的管理员，账号管理、账号活动、存储管理、系统状态和日志只给实例管理员。 */
export function settingsTabs(who: Who): [string, string][] {
  return settingsGroups(who).flatMap((g) => g.items);
}

/** 每个分组上次看的项：切回这个分组时回到那里（只在本次打开网页期间记住）。 */
const lastInGroup = new Map<string, string>();

export default function SettingsPage() {
  const { tab = 'pair' } = useParams(); const nav = useNavigate(); const app = useApp(); const { s } = app;
  const me = useMe().data;
  const edition = useEdition().data;
  const who = { isAdmin: !!me?.user.isAdmin, role: me?.workspace.role };
  const paid = PAID[tab];
  const groups = settingsGroups(who);
  const tabs = groups.flatMap((g) => g.items);
  const heading = { me: t('个人'), space: t('空间'), instance: t('实例管理') } as Record<string, string>;
  // 直接打开没有权限的页签（或切换空间后角色变了）：回到个人资料
  const allowed = tabs.some(([k]) => k === tab);
  useEffect(() => { if (me && !allowed) nav('/settings/profile', { replace: true }); }, [me, allowed, nav]);
  // 当前分组由地址决定，命令面板跳转、直接打开链接、刷新都对得上
  const current = groups.find((g) => g.items.some(([k]) => k === tab)) ?? groups[0];
  useEffect(() => { if (current && allowed) lastInGroup.set(current.key, tab); }, [current, allowed, tab]);
  const openGroup = (key: string) => {
    const g = groups.find((x) => x.key === key);
    if (!g) return;
    const last = lastInGroup.get(key);
    nav(`/settings/${last && g.items.some(([k]) => k === last) ? last : g.items[0][0]}`);
  };
  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 10 }}>
      {groups.length > 0 && (
        <Tabs value={current.key} onChange={(_, v: string) => openGroup(v)} variant="scrollable" scrollButtons={false} aria-label={t('设置分组')}
          sx={{ mb: 3, minHeight: 40, borderBottom: 1, borderColor: 'divider', '& .MuiTab-root': { minHeight: 40, px: 2, textTransform: 'none', fontSize: 14, fontWeight: 500 } }}>
          {groups.map((g) => <Tab key={g.key} value={g.key} label={heading[g.key]} />)}
        </Tabs>
      )}
      <Box sx={{ display: 'flex', gap: 4, flexWrap: 'wrap', alignItems: 'flex-start' }}>
      <List component="nav" aria-label={heading[current?.key ?? 'me']} sx={{ width: 200, flex: 'none' }} disablePadding>
        {current?.items.map(([k, l]) => <ListItemButton key={k} selected={tab === k} onClick={() => nav(`/settings/${k}`)} sx={{ mb: 0.25, minHeight: 40 }}><ListItemText primary={l} slotProps={{
          primary: { sx: { fontSize: 14 } }
        }} />{PAID[k] && !hasFeature(edition, PAID[k]) && <Chip size="small" variant="outlined" label={FEATURE_EDITION[PAID[k]] === 'team' ? t('企业版') : t('收费版')} sx={{ height: 20, fontSize: 11, ml: 1 }} />}</ListItemButton>)}
      </List>
      <Box sx={{ flex: 1, minWidth: 320 }}>
        {paid && allowed && <EditionNotice feature={paid} />}
        {tab === 'edition' && allowed && <EditionPanel />}
        {tab === 'pair' && <PairPanel />}
        {tab === 'ai' && <AiPanel />}
        {tab === 'scenes' && <ScenesPanel />}
        {tab === 'devices' && <DevicesPanel />}
        {tab === 'members' && <MembersPanel />}
        {tab === 'storage' && <StoragePanel />}
        {tab === 'links' && <ShareLinksPanel />}
        {tab === 'upload-links' && allowed && <UploadLinksPanel />}
        {tab === 'remote-inbox' && allowed && <RemoteInboxPanel />}
        {tab === 'emergency' && allowed && <EmergencyContactsPanel />}
        {tab === 'emergency-access' && <MyEmergencyAccessPanel />}
        {tab === 'private' && <MyPrivacyPanel />}
        {tab === 'classifier' && <ClassifierPanel />}
        {tab === 'activity' && allowed && <ActivityPanel scope="workspace" />}
        {tab === 'mail' && allowed && <MailPanel />}
        {tab === 'workflows' && allowed && <WorkflowsPanel />}
        {tab === 'sharing' && allowed && <CollectionsPanel />}
        {tab === 'admin-activity' && allowed && <ActivityPanel scope="instance" />}
        {tab === 'brand' && allowed && <BrandingPanel />}
        {tab === 'ui' && (
          <>
            <Typography variant="h4" sx={{ mb: 3 }}>{t('界面')}</Typography>
            <Box sx={{ display: 'grid', gridTemplateColumns: 'minmax(108px, max-content) auto', alignItems: 'center', gap: 2.5 }}>
              <Typography>{tc('外观', '主题')}</Typography>
              <ToggleButtonGroup size="small" exclusive value={s.theme} onChange={(_, v) => v && app.setTheme(v)} aria-label={t('主题')}><ToggleButton value="system">{t('跟随系统')}</ToggleButton><ToggleButton value="light">{t('浅色')}</ToggleButton><ToggleButton value="dark">{t('深色')}</ToggleButton></ToggleButtonGroup>
              <Typography>{t('快捷键')}</Typography>
              <Box><Button size="small" variant="outlined" onClick={() => app.set({ shortcutsOpen: true })}>{t('查看快捷键')}</Button><Typography variant="caption" sx={{ color: 'text.secondary', ml: 1.5 }}>{t('也可以随时按 ?')}</Typography></Box>
              <Typography>{t('文件列表密度')}</Typography>
              <Stack spacing={0.5} sx={{ alignItems: 'flex-start' }}>
                <ToggleButtonGroup size="small" exclusive value={s.density} onChange={(_, v) => v && app.set({ density: v })}><ToggleButton value="comfy">{t('舒适')}</ToggleButton><ToggleButton value="compact">{t('紧凑')}</ToggleButton></ToggleButtonGroup>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('紧凑时行更矮、不显示缩略图（全部文件、待整理、抽屉和目录条目的文件列表）')}</Typography>
              </Stack>
              <Typography>{t('语言')}</Typography>
              <LanguageChoice />
            </Box>
          </>
        )}
        {tab === 'notify' && <NotificationsPanel />}
        {tab === 'profile' && <ProfileForm />}
        {tab === 'password' && <><PasswordForm /><TwoFactorPanel /><SSOStatus /></>}
        {tab === 'token' && <TokensPanel />}
        {tab === 'accounts' && me?.user.isAdmin && <AccountsPanel />}
        {tab === 'storages' && allowed && <InstanceStoragePanel />}
        {tab === 'system' && allowed && <SystemPanel />}
        {tab === 'logs' && allowed && <LogsPanel />}
      </Box>
      </Box>
    </Box>
  );
}

/** 界面语言（v1.10.0）：存在账号上，换设备也一样；改了就按新语言重新加载页面。 */
function LanguageChoice() {
  const me = useMe();
  const set = useSetLocale();
  const app = useApp();
  return (
    <Stack spacing={0.5} sx={{ alignItems: 'flex-start' }}>
      <ToggleButtonGroup size="small" exclusive value={me.data?.locale ?? ''} disabled={set.isPending} aria-label={t('语言')}
        onChange={(_, v: LangPreference | null) => { if (v !== null) set.mutate(v, { onError: (e) => app.toast(e.message) }); }}>
        <ToggleButton value="">{t('跟随浏览器')}</ToggleButton>
        <ToggleButton value="zh" lang="zh-CN">中文</ToggleButton>{/* i18n-ignore：各用自己的语言写 */}
        <ToggleButton value="en" lang="en">English</ToggleButton>
      </ToggleButtonGroup>
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('跟随浏览器时，浏览器是中文就用中文，否则用英文')}</Typography>
    </Stack>
  );
}
