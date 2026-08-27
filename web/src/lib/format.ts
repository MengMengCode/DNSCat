// 大数字的紧凑展示工具。统计卡片、表格与地图里的查询量动辄百万级，
// 直接铺开千分位数字会把布局撑破，这里统一折算成 k / M / B / T。

const COMPACT_SUFFIXES = ['', 'k', 'M', 'B', 'T'];

// 小数位随量级收敛，让读数宽度稳定在 5 个字符内：1.23k / 12.3k / 123k。
const pickDigits = (abs: number): number => (abs >= 100 ? 0 : abs >= 10 ? 1 : 2);

// trimTrailingZeros 去掉定点小数末尾多余的 0（1.20 → 1.2、1.00 → 1），
// 避免出现 "1.00M" 这类冗余读数。
const trimTrailingZeros = (s: string): string =>
  s.includes('.') ? s.replace(/\.?0+$/, '') : s;

/**
 * formatCompact 把计数压缩成紧凑记数：
 *   999 → "999"、1234 → "1.23k"、12345 → "12.3k"、123456 → "123k"、
 *   1234567 → "1.23M"、1500000000 → "1.5B"
 *
 * 1000 以内保持原样；低于 1 的小数（如低流量下的 QPS 0.03）保留两位小数，
 * 不会被抹成 0。
 */
export const formatCompact = (input: number): string => {
  const n = Number(input);
  if (!Number.isFinite(n)) return '0';

  const abs = Math.abs(n);
  if (abs < 1000) {
    return Number.isInteger(n) ? String(n) : String(Math.round(n * 100) / 100);
  }

  const maxTier = COMPACT_SUFFIXES.length - 1;
  let tier = Math.min(maxTier, Math.floor(Math.log10(abs) / 3));
  let scaled = n / 1000 ** tier;
  let digits = pickDigits(Math.abs(scaled));

  // 四舍五入后可能顶到下一个量级（999,999 会算出 "1000k"），这里升一级改写成 "1M"。
  if (Math.abs(Number(scaled.toFixed(digits))) >= 1000 && tier < maxTier) {
    tier += 1;
    scaled = n / 1000 ** tier;
    digits = pickDigits(Math.abs(scaled));
  }

  return `${trimTrailingZeros(scaled.toFixed(digits))}${COMPACT_SUFFIXES[tier]}`;
};

/**
 * formatFull 输出带千分位的完整数值，用于 title 悬浮提示，
 * 使数字被缩写后仍然可以看到精确值。
 */
export const formatFull = (input: number): string => {
  const n = Number(input);
  return Number.isFinite(n) ? n.toLocaleString() : '0';
};

/**
 * localizeDualName 处理形如「亚洲 (Asia)」的双语名称。
 * 这类值存在数据库里（例如内置分线线路名、出厂证书申请人名），
 * 一个字段同时承载中英两种称谓，需要按界面语言只显示对应的一种。
 *
 * 中文界面取括号前的中文，英文界面取括号内的英文；不符合该结构时原样返回，
 * 因此用户自建的纯中文或纯英文名称完全不受影响。
 *
 * 判定条件刻意收紧到「括号在末尾、括号内无中文、括号前有中文」三者同时成立，
 * 避免把括号内是术语或单位的正常文案拆坏，例如：
 *   「优先级 (MX / SRV)」    括号内是适用记录类型，非翻译
 *   「TTL (缓存有效时间)」   括号内是中文解释，方向相反
 *   「ECDSA P-256 (prime256v1)」括号前无中文，本身就是英文名
 */
export const localizeDualName = (name: string, isZh: boolean): string => {
  const raw = (name || '').trim();
  const matched = raw.match(/^(.*?)\s*\(([^()]*)\)$/);
  if (!matched) return raw;

  const zhPart = matched[1].trim();
  const enPart = matched[2].trim();
  const hasCJK = (s: string) => /[\u4e00-\u9fa5]/.test(s);

  if (!zhPart || !enPart) return raw;
  if (hasCJK(enPart)) return raw;
  if (!hasCJK(zhPart)) return raw;

  return isZh ? zhPart : enPart;
};
