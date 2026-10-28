import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { StoreProvider, useApp } from './app-store';
import { ShortcutsDialog } from '../components/shortcuts-dialog';

const LS = 'kept-proto-prefs';

function stubSystem(dark: boolean) {
  type L = (e: { matches: boolean }) => void;
  const listeners = new Set<L>();
  const mq = { matches: dark, addEventListener: (_: string, f: L) => listeners.add(f), removeEventListener: (_: string, f: L) => listeners.delete(f) };
  vi.stubGlobal('matchMedia', vi.fn(() => mq));
  return (next: boolean) => { mq.matches = next; listeners.forEach((f) => f({ matches: next })); };
}

function Probe() {
  const { s, setTheme } = useApp();
  return (
    <>
      <p>{s.theme} {s.mode}</p>
      <button onClick={() => setTheme('light')}>浅色</button>
      <button onClick={() => setTheme('system')}>跟随系统</button>
    </>
  );
}

afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); });

describe('主题（v1.5.9）', () => {
  it('新用户默认跟随系统，系统切换时跟着变', () => {
    const flip = stubSystem(true);
    render(<StoreProvider><Probe /></StoreProvider>);
    expect(screen.getByText('system dark')).toBeInTheDocument();
    act(() => flip(false));
    expect(screen.getByText('system light')).toBeInTheDocument();
  });

  it('以前选过深色的保持深色；以前存的浅色（可能没选过）改为跟随系统', () => {
    stubSystem(true);
    localStorage.setItem(LS, JSON.stringify({ mode: 'dark', density: 'comfy' }));
    const a = render(<StoreProvider><Probe /></StoreProvider>);
    expect(screen.getByText('dark dark')).toBeInTheDocument();
    a.unmount();
    localStorage.setItem(LS, JSON.stringify({ mode: 'light', density: 'comfy' }));
    render(<StoreProvider><Probe /></StoreProvider>);
    expect(screen.getByText('system dark')).toBeInTheDocument();
  });

  it('选了浅色就不再跟随系统，并记住', async () => {
    const flip = stubSystem(true);
    render(<StoreProvider><Probe /></StoreProvider>);
    await userEvent.click(screen.getByRole('button', { name: '浅色' }));
    expect(screen.getByText('light light')).toBeInTheDocument();
    act(() => flip(true));
    expect(screen.getByText('light light')).toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem(LS)!)).toMatchObject({ theme: 'light' });
  });
});

describe('快捷键说明', () => {
  it('按分组列出按键', () => {
    function Open() { const { set } = useApp(); return <button onClick={() => set({ shortcutsOpen: true })}>打开</button>; }
    render(<StoreProvider><Open /><ShortcutsDialog /></StoreProvider>);
    act(() => screen.getByRole('button', { name: '打开' }).click());
    const dialog = screen.getByRole('dialog', { name: '快捷键' });
    for (const g of ['全局', '文件列表', '文档详情', '待整理']) expect(screen.getByRole('region', { name: g })).toBeInTheDocument();
    expect(dialog).toHaveTextContent('完成整理');
    expect(dialog).toHaveTextContent('在预览里查找');
  });
});
