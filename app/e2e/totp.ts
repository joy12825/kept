import { createHmac } from 'node:crypto';

// 与 server/internal/auth/totp.go 对应的最小 RFC 6238 实现：HMAC-SHA1、30 秒时间片、6 位数字。

const STEP_SECONDS = 30;

/** 解码不带填充的大写 Base32（RFC 4648），与后端 totpSecretEncoding 一致。 */
function base32Decode(input: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  const clean = input.replace(/\s+/g, '').toUpperCase();
  let bits = '';
  for (const c of clean) {
    const idx = alphabet.indexOf(c);
    if (idx === -1) throw new Error(`不是合法的 Base32 字符：${c}`);
    bits += idx.toString(2).padStart(5, '0');
  }
  const bytes: number[] = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) {
    bytes.push(parseInt(bits.slice(i, i + 8), 2));
  }
  return Buffer.from(bytes);
}

function hotp(secret: Buffer, counter: number): string {
  const buf = Buffer.alloc(8);
  buf.writeBigUInt64BE(BigInt(counter));
  const hmac = createHmac('sha1', secret).update(buf).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const code = ((hmac[offset] & 0x7f) << 24) | (hmac[offset + 1] << 16) | (hmac[offset + 2] << 8) | hmac[offset + 3];
  return String(code % 1_000_000).padStart(6, '0');
}

export function totpStepAt(epochMs: number): number {
  return Math.floor(epochMs / 1000 / STEP_SECONDS);
}

/** 计算给定 Base32 密钥、给定时刻（默认当前时间）所在时间片的 6 位验证码。 */
export function totpAt(secretBase32: string, epochMs = Date.now()): { code: string; step: number } {
  const step = totpStepAt(epochMs);
  return { code: hotp(base32Decode(secretBase32), step), step };
}

/**
 * 计算一个"新鲜"的验证码：如果当前时间片与 `avoidStep`（例如上一次已经用掉的时间片）相同，
 * 就等到下一个时间片再算，避免服务端拒绝重复使用同一时间片的验证码。
 */
export async function freshTotp(secretBase32: string, avoidStep?: number): Promise<{ code: string; step: number }> {
  let { code, step } = totpAt(secretBase32);
  while (avoidStep !== undefined && step <= avoidStep) {
    const msIntoStep = Date.now() % (STEP_SECONDS * 1000);
    const waitMs = STEP_SECONDS * 1000 - msIntoStep + 250;
    await new Promise((resolve) => setTimeout(resolve, waitMs));
    ({ code, step } = totpAt(secretBase32));
  }
  return { code, step };
}
