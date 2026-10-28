import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';
import { cleanup, configure } from '@testing-library/react';

// 未开启 Vitest globals，Testing Library 不会自动清理，需要在这里注册
afterEach(() => cleanup());

// 整套测试并行跑时机器负载高，默认 1 秒的 findBy / waitFor 等待偶尔不够（例如多步的对话框流程）
configure({ asyncUtilTimeout: 3000 });
