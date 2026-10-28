// 添加、修改存储（v1.7.2）：实例管理员在页面上接入 S3 兼容存储、WebDAV（v1.7.5）或本机目录。
// 密钥保存后不再显示；修改时 Secret Key 留空表示不变。已经放了文档的存储，类型、目录、桶和前缀不能再改。
import { useState } from 'react';
import {
  Alert, Box, Button, Checkbox, Chip, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, InputAdornment, Radio, RadioGroup, Stack, TextField, Typography,
} from '@mui/material';
import { useSaveStorage, useTryStorage, type AdminStorage, type StorageInput, type StorageTest } from '../../api/storage';
import { useApp } from '../../store/app-store';
import { t, tc } from '../../i18n';

type Kind = StorageInput['kind'];

/** onCreated：新加的存储保存后调用，encrypt 为是否要开启加密（v1.11.0，S3 和 WebDAV 默认开）。 */
export function StorageFormDialog({ storage, onClose, onCreated }: { storage?: AdminStorage; onClose: () => void; onCreated?: (name: string, encrypt: boolean) => void }) {
  const app = useApp();
  const save = useSaveStorage();
  const tryIt = useTryStorage();
  const editing = !!storage;
  const set = storage?.settings ?? {};
  const fixed = editing && storage.documents > 0; // 文件放在哪里不能再改
  const [name, setName] = useState(storage?.name ?? '');
  const [description, setDescription] = useState(storage?.description ?? '');
  const [kind, setKind] = useState<Kind>(storage?.kind === 'dir' || storage?.kind === 'webdav' ? storage.kind : 's3');
  const [encrypt, setEncrypt] = useState<boolean | null>(null); // null：按类型的默认
  const encrypting = encrypt ?? kind !== 'dir';
  const [davUrl, setDavUrl] = useState(set.url ?? '');
  const [username, setUsername] = useState(set.username ?? '');
  const [path, setPath] = useState(set.path ?? '');
  const [endpoint, setEndpoint] = useState(set.endpoint ?? '');
  const [region, setRegion] = useState(set.region ?? '');
  const [bucket, setBucket] = useState(set.bucket ?? '');
  const [prefix, setPrefix] = useState(set.prefix ?? '');
  const [pathStyle, setPathStyle] = useState(set.pathStyle ?? true);
  const [accessKey, setAccessKey] = useState(set.accessKey ?? '');
  const [secretKey, setSecretKey] = useState('');
  const [selectable, setSelectable] = useState(storage?.selectable ?? true);
  const [capacityGb, setCapacityGb] = useState(storage?.capacity ? String(Math.round((storage.capacity / (1 << 30)) * 10) / 10) : '');
  const capOk = capacityGb === '' || Number(capacityGb) > 0;
  const [result, setResult] = useState<StorageTest | string | null>(null);

  const nameOk = editing || /^[a-z][a-z0-9-]{0,31}$/.test(name);
  const ready = nameOk && capOk && (kind === 'dir'
    ? path.trim().startsWith('/')
    : kind === 'webdav'
      ? /^https?:\/\/[^/]+/.test(davUrl.trim())
      : bucket.trim() !== '' && accessKey.trim() !== '' && (secretKey !== '' || editing));
  const input = (): StorageInput => ({
    name: editing ? undefined : name, kind, description, selectable,
    capacity: capacityGb === '' ? 0 : Math.round(Number(capacityGb) * (1 << 30)),
    settings: kind === 'dir' ? { path } : kind === 'webdav' ? { url: davUrl, username } : { endpoint, region, bucket, prefix, pathStyle, accessKey },
    secretKey: kind !== 'dir' && secretKey ? secretKey : undefined,
  });
  const runTest = () => {
    setResult(null);
    tryIt.mutate({ existing: storage?.name, storage: input() }, { onSuccess: setResult, onError: (e) => setResult(e.message) });
  };
  const submit = () => save.mutate({ name: storage?.name, input: input() }, {
    onSuccess: () => {
      app.toast(editing ? t('已保存存储设置') : t('已添加存储“{name}”，可以在下面给空间换存储了', { name }));
      onClose();
      if (!editing) onCreated?.(name, encrypting);
    },
    onError: (e) => app.toast(e.message),
  });

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{editing ? t('修改存储“{name}”', { name: storage.name }) : t('添加存储')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {fixed && <Alert severity="info">{t('这里已经放了 {n} 份文档，类型、目录、桶和前缀不能再改；地址和密钥可以改。', { n: storage.documents })}</Alert>}
          {storage?.error && <Alert severity="error">{t('{error}。重新填写 Secret Key 后保存即可恢复。', { error: storage.error })}</Alert>}
          <TextField label={t('名称')} value={name} onChange={(e) => setName(e.target.value.toLowerCase())} disabled={editing} required
            error={!nameOk && name !== ''} helperText={t('小写字母、数字和 -，以字母开头，例如 nas、oss-archive；保存后不能改')} />
          <TextField label={t('说明')} value={description} onChange={(e) => setDescription(e.target.value)}
            helperText={t('空间管理员选择时看到，例如“家里的 NAS”“阿里云 OSS 归档”')} />
          <RadioGroup row value={kind} onChange={(e) => setKind(e.target.value as Kind)} aria-label={t('存储类型')}>
            <FormControlLabel value="s3" control={<Radio />} label={t('S3 兼容存储')} disabled={fixed} />
            <FormControlLabel value="webdav" control={<Radio />} label={t('WebDAV（网盘、NAS）')} disabled={fixed} />
            <FormControlLabel value="dir" control={<Radio />} label={t('服务器上的目录')} disabled={fixed} />
          </RadioGroup>
          {kind === 'webdav' && (
            <>
              <TextField label={t('地址')} value={davUrl} onChange={(e) => setDavUrl(e.target.value)} required disabled={fixed}
                helperText={t('文件夹的完整地址，例如坚果云 https://dav.jianguoyun.com/dav/kept、Nextcloud https://域名/remote.php/dav/files/用户名/kept、群晖 https://NAS地址:5006/kept；文件夹不存在时会自动建')} />
              <TextField label={t('用户名')} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
              <TextField label={t('密码')} type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} autoComplete="new-password"
                helperText={editing ? t('已加密保存，不修改请留空') : t('坚果云等要用“应用密码”，不是登录密码；加密后保存')} />
            </>
          )}
          {kind === 'webdav' ? null : kind === 'dir' ? (
            <TextField label={tc('文件夹', '目录')} value={path} onChange={(e) => setPath(e.target.value)} required disabled={fixed}
              helperText={t('服务器上的绝对路径，例如 /mnt/nas/kept；用 Docker 部署时，要先把这个目录挂进容器')} />
          ) : (
            <>
              <TextField label={t('服务地址')} value={endpoint} onChange={(e) => setEndpoint(e.target.value)}
                helperText={t('例如 http://192.168.1.10:9000（局域网里没有证书的 MinIO 写 http://）、oss-cn-hangzhou.aliyuncs.com；留空为 AWS S3')} />
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
                <TextField label={t('桶')} value={bucket} onChange={(e) => setBucket(e.target.value)} required disabled={fixed} sx={{ flex: 1 }} helperText={t('要先在服务里建好')} />
                <TextField label={t('前缀')} value={prefix} onChange={(e) => setPrefix(e.target.value)} disabled={fixed} sx={{ flex: 1 }} helperText={t('可以不填，例如 kept')} />
              </Stack>
              <TextField label={t('区域')} value={region} onChange={(e) => setRegion(e.target.value)} helperText={t('按服务商要求填写，例如 cn-hangzhou；MinIO 可以不填')} />
              <FormControlLabel control={<Checkbox checked={pathStyle} onChange={(e) => setPathStyle(e.target.checked)} />}
                label={t('桶名放在路径里（MinIO 和多数 NAS 需要勾选，云存储一般不勾）')} />
              <TextField label="Access Key" value={accessKey} onChange={(e) => setAccessKey(e.target.value)} required autoComplete="off" />
              <TextField label="Secret Key" type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} required={!editing} autoComplete="new-password"
                helperText={editing ? t('已加密保存，不修改请留空') : t('加密后保存，之后不再显示')} />
            </>
          )}
          <TextField label={t('容量上限')} type="number" value={capacityGb} onChange={(e) => setCapacityGb(e.target.value)} error={!capOk}
            sx={{ maxWidth: 220 }} slotProps={{ input: { endAdornment: <InputAdornment position="end">GB</InputAdornment> }, htmlInput: { min: 0.1, step: 0.1 } }}
            helperText={t('可以不填；满了以后不再往这里写新文档')} />
          <FormControlLabel control={<Checkbox checked={selectable} onChange={(e) => setSelectable(e.target.checked)} />}
            label={t('空间管理员可以把自己的空间换到这里')} />
          {!editing && (
            <FormControlLabel control={<Checkbox checked={encrypting} onChange={(e) => setEncrypt(e.target.checked)} />}
              label={t('加密存放：文件在本机加密后才放进去（推荐云端存储开启，以后也可以在列表里开关）')} />
          )}
          {result !== null && (
            <Box>
              {typeof result === 'string' || !result.ok
                ? <Alert severity="error">{t('连接失败：{error}', { error: typeof result === 'string' ? result : result.error ?? '' })}</Alert>
                : <Chip color="success" variant="outlined" label={t('可以读写 · {ms} ms', { ms: result.tookMs })} />}
            </Box>
          )}
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>
            {t('“测试连接”会写入一个小文件、读回再删除。密钥要有读、写、删除和列出的权限。')}
          </Typography>
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={runTest} disabled={!ready || tryIt.isPending} sx={{ mr: 'auto' }}>{tryIt.isPending ? t('正在测试…') : t('测试连接')}</Button>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" onClick={submit} disabled={!ready || save.isPending}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}
