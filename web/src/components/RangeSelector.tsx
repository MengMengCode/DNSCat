import React from 'react';
import { TrendRange } from '../types';

// 统一的时间范围选项：1H~30D。与后端 trendSpecs / QpsTrendChart 的 RANGES 保持一致。
export const TREND_RANGES: { key: TrendRange; label: string }[] = [
  { key: '1h', label: '1H' },
  { key: '12h', label: '12H' },
  { key: '24h', label: '24H' },
  { key: '3d', label: '3D' },
  { key: '7d', label: '7D' },
  { key: '14d', label: '14D' },
  { key: '30d', label: '30D' },
];

// 短区间数据变化快，刷新更频繁；长区间降低刷新频率减少无谓请求。
export const rangeRefreshMs = (range: TrendRange): number =>
  range === '1h' || range === '12h' || range === '24h' ? 5000 : 60000;

interface RangeSelectorProps {
  value: TrendRange;
  onChange: (r: TrendRange) => void;
  className?: string;
}

// 时间范围选择器（1H~30D）。样式与 QpsTrendChart 内的按钮组一致，
// 供地理分布 / Top Countries / 查询类型等统计块复用。
export const RangeSelector: React.FC<RangeSelectorProps> = ({ value, onChange, className }) => (
  <div
    className={`flex items-center gap-0.5 bg-bg-subtle border border-border rounded-sm p-0.5 font-mono ${
      className || ''
    }`}
  >
    {TREND_RANGES.map((r) => (
      <button
        key={r.key}
        type="button"
        onClick={() => onChange(r.key)}
        className={`px-1.5 py-0.5 text-[10px] rounded-sm transition-colors cursor-pointer ${
          value === r.key ? 'bg-primary text-bg font-semibold' : 'text-secondary hover:text-primary'
        }`}
      >
        {r.label}
      </button>
    ))}
  </div>
);
