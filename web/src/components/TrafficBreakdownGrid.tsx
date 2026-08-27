import React from 'react';
import { Globe, PieChart } from 'lucide-react';
import { GeoBreakdown, TrendRange, TypeBreakdown } from '../types';
import { useI18n } from '../i18n/I18nContext';
import { getRegionOption } from './FlagRegionSelect';
import { CODE_TO_GEO_NAME } from '../lib/countryMap';
import { formatCompact, formatFull } from '../lib/format';
import { RangeSelector } from './RangeSelector';

// 分布卡片单侧最多展示的行数。国家与查询类型的项数都可能很多，
// 全量渲染会把卡片拉得很长，这里只保留数据量最大的前 N 项。
const MAX_BREAKDOWN_ROWS = 15;

interface TrafficBreakdownGridProps {
  geoData: GeoBreakdown[];
  types: TypeBreakdown[];
  countryRange: TrendRange;
  typeRange: TrendRange;
  onCountryRangeChange: (range: TrendRange) => void;
  onTypeRangeChange: (range: TrendRange) => void;
  countryTitle?: string;
  typeTitle?: string;
  countryEmpty?: string;
  typeEmpty?: string;
}

// 全局仪表盘与域名仪表盘共用同一套分布卡片，避免标题、间距、顺序和图形样式再次漂移。
export const TrafficBreakdownGrid: React.FC<TrafficBreakdownGridProps> = ({
  geoData,
  types,
  countryRange,
  typeRange,
  onCountryRangeChange,
  onTypeRangeChange,
  countryTitle,
  typeTitle,
  countryEmpty,
  typeEmpty,
}) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';

  const totalQueries = types.reduce((acc, cur) => acc + (Number(cur.value) || 0), 0);
  const countryGeoData = geoData.filter((item) =>
    Boolean(CODE_TO_GEO_NAME[item.code?.toLowerCase()] && (item.queries || 0) > 0)
  );
  const topCountries = [...countryGeoData]
    .sort((a, b) => (b.queries || 0) - (a.queries || 0))
    .slice(0, MAX_BREAKDOWN_ROWS);
  const totalGeoQueries = countryGeoData.reduce((acc, cur) => acc + (cur.queries || 0), 0);
  // 查询类型同样限行。后端已按次数降序返回，这里再排一次以免数据来源变化后顺序失准。
  // 百分比仍以全部类型的总量为分母，保证截断后各行的占比读数依然正确。
  const topTypes = [...types]
    .sort((a, b) => (Number(b.value) || 0) - (Number(a.value) || 0))
    .slice(0, MAX_BREAKDOWN_ROWS);

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <div className="geist-card p-6 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Globe className="w-4 h-4 text-primary" />
            <h3 className="text-xs font-bold text-primary uppercase tracking-wide font-mono">
              {countryTitle ?? (isZh ? '请求来源国家 / 地区分布' : 'Top Countries / Regions')}
            </h3>
          </div>
          <RangeSelector value={countryRange} onChange={onCountryRangeChange} />
        </div>

        <div className="space-y-3 pt-2 font-mono text-xs">
          {topCountries.map((item) => {
            const opt = getRegionOption(item.code);
            const label = isZh ? opt.nameZh : opt.nameEn;
            const percent = totalGeoQueries > 0
              ? Math.round(((item.queries || 0) / totalGeoQueries) * 100)
              : 0;

            return (
              <div key={item.code} className="space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="font-medium text-primary flex items-center gap-1.5">
                    <span>{opt.flag}</span>
                    <span className="truncate">{label}</span>
                  </span>
                  <span className="text-secondary" title={formatFull(item.queries || 0)}>
                    {formatCompact(item.queries || 0)} ({percent}%)
                  </span>
                </div>
                <div className="w-full h-1.5 bg-bg-subtle rounded-full overflow-hidden">
                  <div
                    style={{ width: `${Math.max(percent, item.queries > 0 ? 3 : 0)}%` }}
                    className="h-full bg-primary rounded-full transition-all"
                  />
                </div>
              </div>
            );
          })}

          {topCountries.length === 0 && (
            <div className="text-center text-secondary py-6">
              {countryEmpty ?? (isZh ? '暂无国家 / 地区流量数据' : 'No country traffic recorded')}
            </div>
          )}
        </div>
      </div>

      <div className="geist-card p-6 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <PieChart className="w-4 h-4 text-primary" />
            <h3 className="text-xs font-bold text-primary uppercase tracking-wide font-mono">
              {typeTitle ?? (isZh ? 'DNS 查询类型分布' : 'DNS Query Type Distribution')}
            </h3>
          </div>
          <RangeSelector value={typeRange} onChange={onTypeRangeChange} />
        </div>

        <div className="space-y-3 pt-2 font-mono text-xs">
          {topTypes.map((item) => {
            const value = Number(item.value) || 0;
            const percent = totalQueries > 0 ? Math.round((value / totalQueries) * 100) : 0;
            return (
              <div key={item.name} className="space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="font-bold text-primary">{item.name}</span>
                  <span className="text-secondary" title={formatFull(value)}>
                    {formatCompact(value)} ({percent}%)
                  </span>
                </div>
                <div className="w-full h-1.5 bg-bg-subtle rounded-full overflow-hidden">
                  <div
                    style={{ width: `${Math.max(percent, value > 0 ? 3 : 0)}%` }}
                    className="h-full bg-primary rounded-full transition-all"
                  />
                </div>
              </div>
            );
          })}

          {topTypes.length === 0 && (
            <div className="text-center text-secondary py-6">
              {typeEmpty ?? (isZh ? '暂无请求类型记录' : 'No query types recorded')}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
