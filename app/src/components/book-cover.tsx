// 书的封面：有封面图用封面，否则用缩略图，都没有时写书名。书房、书单、年度回顾共用。
import { Box, Typography } from '@mui/material';
import { coverUrl } from '../api/books';
import { fileUrl } from '../api/documents';

export function BookCover({ id, title, hasCover, hasThumbnail, width }: { id: number; title: string; hasCover?: boolean; hasThumbnail?: boolean; width?: number | string }) {
  return (
    <Box sx={{ width: width ?? '100%', aspectRatio: '2 / 3', borderRadius: 1.5, overflow: 'hidden', bgcolor: 'surface.containerHigh', boxShadow: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 'none', printColorAdjust: 'exact' }}>
      {hasCover || hasThumbnail
        ? <Box component="img" src={hasCover ? coverUrl(id) : fileUrl(id, 'thumbnail')} alt="" loading="lazy" sx={{ width: '100%', height: '100%', objectFit: 'cover' }} />
        : <Typography sx={{ p: 1, fontSize: 13, fontWeight: 500, textAlign: 'center', overflowWrap: 'anywhere' }}>{title}</Typography>}
    </Box>
  );
}
