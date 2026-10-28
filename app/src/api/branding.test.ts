import { describe, expect, it } from 'vitest';
import { defaultIcon } from './branding';

describe('defaultIcon', () => {
  it('是名称第一个字的 SVG 图标', () => {
    const svg = decodeURIComponent(defaultIcon('家庭档案').replace('data:image/svg+xml,', ''));
    expect(svg).toContain('>家</text>');
    expect(svg).toContain('fill="#2563EB"');
  });

  it('名称为空时用“收”，不把尖括号带进 SVG', () => {
    expect(decodeURIComponent(defaultIcon('  '))).toContain('>收</text>');
    expect(decodeURIComponent(defaultIcon('<x'))).toContain('>收</text>');
  });
});
