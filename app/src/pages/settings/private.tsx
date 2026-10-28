// 设置 → 空间 → 我的私密（v1.8.0）：我在这个空间是哪位家人，我负责或被授权的家人，
// 谁帮我负责的家人上传过私密文档（可以收回），以及我是谁的紧急联系人。谁能看在“目录 → 家人”里改。
import { Alert, Box, Button, Card, Chip, Skeleton, Stack, Typography } from '@mui/material';
import { useNavigate } from 'react-router-dom';
import { EyeOff } from 'lucide-react';
import { LEVEL_LABEL, useMyPrivacy, usePersonEmergencyAction, useRevokeUploader } from '../../api/privacy';
import { waitLabel } from '../../api/emergency';
import { useApp } from '../../store/app-store';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { dateTime } from '../detail/time';
import { t } from '../../i18n';

export function MyPrivacyPanel() {
  const mine = useMyPrivacy();
  const revoke = useRevokeUploader();
  const emergency = usePersonEmergencyAction();
  const app = useApp();
  const nav = useNavigate();
  const data = mine.data;
  const name = (id: number) => data?.persons.find((p) => p.id === id)?.name ?? t('家人');

  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 1 }}>{t('我的私密')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>
        {t('家人的私密文档只有本人、照看人、被授权的人和帮忙上传的人看得到，空间管理员也看不到。谁能看，在“目录 → 家人”里打开这位家人，点“编辑”后设置。')}
      </Typography>
      {mine.isError && <ErrorState onRetry={() => mine.refetch()} />}
      {mine.isPending && <Skeleton variant="rounded" height={88} />}
      {data && (
        <Stack spacing={3}>
          {!data.self && (
            <Alert severity="info" action={<Button color="inherit" size="small" onClick={() => nav('/directory?tab=persons')}>{t('去目录')}</Button>}>
              {t('你还没有关联到某位家人。在“目录 → 家人”里打开自己，点“编辑”，再点“这是我”。')}
            </Alert>
          )}
          <Box>
            <Typography variant="h6" sx={{ mb: 1 }}>{t('我负责的家人')}</Typography>
            {!data.persons.length && <EmptyState icon={<EyeOff size={40} strokeWidth={SW} />} title={t('还没有')} text={t('关联了自己、成为照看人，或别人授权给你之后会出现在这里。')} />}
            <Stack spacing={1}>
              {data.persons.map((p) => (
                <Card key={p.id} variant="outlined" sx={{ p: 2 }}>
                  <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <Typography sx={{ fontWeight: 500 }}>{p.name}</Typography>
                        {p.privacy.myLevel && <Chip size="small" label={LEVEL_LABEL[p.privacy.myLevel]} />}
                      </Stack>
                      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>
                        {t('{n} 份私密文档 · 能看的人：{people}', { n: p.privacy.privateCount, people: (p.privacy.access ?? []).map((a) => t('{name}（{level}）', { name: a.displayName, level: LEVEL_LABEL[a.level] })).join(t('、')) || '—' })}
                      </Typography>
                      {p.privacy.needsReview && <Typography variant="body2" sx={{ color: 'warning.main' }}>{t('关联前就有照看人，请确认是否保留')}</Typography>}
                      {p.privacy.holder && !p.privacy.key && p.privacy.privateCount > 0 && (
                        <Typography variant="body2" sx={{ color: 'warning.main' }}>{t('还没有钥匙：私密文档只在这台服务器上，不会加密备份到云端')}</Typography>
                      )}
                    </Box>
                    <Button onClick={() => nav(`/directory/persons/${p.id}`)}>{t('查看')}</Button>
                  </Stack>
                </Card>
              ))}
            </Stack>
          </Box>

          {data.uploaders.length > 0 && (
            <Box>
              <Typography variant="h6" sx={{ mb: 1 }}>{t('帮忙上传的人')}</Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('他们能看到自己上传的那几份私密文档。收回后就看不到了。')}</Typography>
              <Stack spacing={1}>
                {data.uploaders.map((u) => (
                  <Card key={`${u.personId}-${u.userId}`} variant="outlined" sx={{ p: 2 }}>
                    <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                      <Typography sx={{ flex: 1 }}>{t('{name} 帮{person}上传了 {n} 份', { name: u.displayName, person: name(u.personId), n: u.documents })}</Typography>
                      <Button color="error" disabled={revoke.isPending}
                        onClick={() => revoke.mutate({ personId: u.personId, userId: u.userId }, { onSuccess: (r) => app.toast(t('已收回 {n} 份', { n: r.revoked })), onError: (e) => app.toast(e.message) })}>
                        {t('收回')}
                      </Button>
                    </Stack>
                  </Card>
                ))}
              </Stack>
            </Box>
          )}

          {data.emergencyFor.length > 0 && (
            <Box>
              <Typography variant="h6" sx={{ mb: 1 }}>{t('我是紧急联系人')}</Typography>
              <Stack spacing={1}>
                {data.emergencyFor.map((x) => (
                  <Card key={x.personId} variant="outlined" sx={{ p: 2 }}>
                    <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                      <Box sx={{ flex: 1, minWidth: 0 }}>
                        <Typography sx={{ fontWeight: 500 }}>{t('{name}的私密文档', { name: x.name })}</Typography>
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                          {x.emergency.status === 'pending' && x.emergency.grantAt ? t('已申请，{time} 前没有被拒绝就会生效', { time: dateTime(x.emergency.grantAt) })
                            : x.emergency.status === 'granted' ? t('已生效：可以只读查看')
                              : t('申请后等待 {wait}', { wait: waitLabel(x.emergency.waitHours) })}
                        </Typography>
                      </Box>
                      {x.emergency.status === 'idle' && (
                        <Button variant="outlined" color="error" disabled={emergency.isPending}
                          onClick={() => emergency.mutate({ personId: x.personId, userId: 0, action: 'request' }, { onSuccess: () => app.toast(t('已申请，对方会收到通知')), onError: (e) => app.toast(e.message) })}>
                          {t('申请查看')}
                        </Button>
                      )}
                      {x.emergency.status === 'pending' && (
                        <Button disabled={emergency.isPending}
                          onClick={() => emergency.mutate({ personId: x.personId, userId: 0, action: 'cancel' }, { onSuccess: () => app.toast(t('已撤回申请')), onError: (e) => app.toast(e.message) })}>
                          {t('撤回申请')}
                        </Button>
                      )}
                    </Stack>
                  </Card>
                ))}
              </Stack>
            </Box>
          )}
        </Stack>
      )}
    </Box>
  );
}
