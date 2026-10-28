// 任务记录（v1.7.18 从“设置”移出，成为单独的页面）：这个空间在后台处理的任务，可以按种类筛选、只看自己的。
// 入口：顶栏任务面板的“查看全部任务”、存储页的“在任务记录里查看”、命令面板。
import { Navigate, useLocation } from 'react-router-dom';
import { Box } from '@mui/material';
import { TasksPanel } from './settings/system';

export default function TasksPage() {
  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 10 }}>
      <TasksPanel />
    </Box>
  );
}

/** 旧地址 /settings/tasks：带着筛选条件跳到 /tasks，以前存的链接仍然能用。 */
export function OldTasksRedirect() {
  const { search } = useLocation();
  return <Navigate to={`/tasks${search}`} replace />;
}
