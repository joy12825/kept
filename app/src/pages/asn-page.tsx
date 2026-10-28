// 按原件编号打开（v1.5.3）：/asn/123 打开当前空间里原件编号为 123 的文档。可以把这个地址印成二维码贴在纸质原件上，
// 扫一下就能打开电子版。没有这个编号时说明一下，并可以去文件列表。
import { useEffect } from 'react';
import { Navigate, useNavigate, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Hash } from 'lucide-react';
import { api, unwrap } from '../api/client';
import { formatAsn } from '../api/documents';
import { EmptyState, ErrorState, LoadingState } from '../components/common';
import { SW } from '../components/icons';
import { t } from '../i18n';

export function AsnPage() {
  const { n } = useParams();
  const nav = useNavigate();
  const asn = /^\d{1,9}$/.test(n ?? '') ? Number(n) : 0;
  const q = useQuery({
    queryKey: ['documents', 'asn', asn],
    enabled: asn > 0,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents', {
      params: { query: { filter: JSON.stringify({ field: 'asn', op: 'eq', value: asn }), limit: 1 } },
    })),
  });
  const id = q.data?.items[0]?.id;
  useEffect(() => { if (id) nav(`/docs/${id}`, { replace: true }); }, [id, nav]);
  if (!asn) return <Navigate to="/files" replace />;
  if (q.isError) return <ErrorState onRetry={() => void q.refetch()} />;
  if (q.isPending || id) return <LoadingState />;
  return (
    <EmptyState icon={<Hash size={40} strokeWidth={SW} />} title={t('没有原件编号为 {asn} 的文档', { asn: formatAsn(asn) })}
      text={t('这个空间里还没有文档用这个编号。编号可能还没填，或者文档在别的空间、已移到回收站。')}
      action={t('打开文件列表')} onAction={() => nav('/files')} />
  );
}
