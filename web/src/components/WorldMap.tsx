import React, { useEffect, useMemo, useRef, useState } from 'react';
import { GeoBreakdown, TrendRange } from '../types';
import { getRegionOption, getRegionDisplayName } from './FlagRegionSelect';
import { useI18n } from '../i18n/I18nContext';
import { Globe, TrendingUp } from 'lucide-react';
import { Progress } from './GeistUI';
import { PageHeader } from './PageHeader';
import { CODE_TO_GEO_NAME, getCountryCodeByName } from '../lib/countryMap';
import { getWorldMapGeometry } from '../lib/worldGeo';
import { formatCompact, formatFull } from '../lib/format';
import { RangeSelector } from './RangeSelector';

interface WorldMapProps {
  geoData: GeoBreakdown[];
  range?: TrendRange;
  onRangeChange?: (range: TrendRange) => void;
}

interface CountryDatum {
  code: string;
  queries: number;
  percent: number;
}

interface HoveredCountry {
  name: string;
  code?: string;
  queries: number;
  percent: number;
  x: number;
  y: number;
}

const LOGO_BLUE = '#1296db';

interface MapPalette {
  /** 无数据国家 / 地区的底色 */
  empty: string;
  /** 常态国界线 */
  border: string;
  /** 悬停 / 聚焦时的国界线 */
  borderActive: string;
  /** 从无数据底色渐变到 Logo 蓝的色阶 */
  stops: string[];
}

// 浅色主题：Geist gray-100 底色 + gray-600 国界，逐步加蓝到 Logo 蓝。
const LIGHT_MAP_PALETTE: MapPalette = {
  empty: '#f2f2f2',
  border: '#a8a8a8',
  borderActive: '#5b6672',
  stops: ['#f2f2f2', '#d7edf8', '#9dd4ef', '#57b6e5', LOGO_BLUE],
};

// 深色主题：底色比卡片背景 (#0a0a0a) 略亮并配可见国界，保证无数据区域依然清晰。
const DARK_MAP_PALETTE: MapPalette = {
  empty: '#1f1f1f',
  border: '#454545',
  borderActive: '#8f8f8f',
  stops: ['#1f1f1f', '#17384c', '#125a7e', '#0f81b3', LOGO_BLUE],
};

// 有数据但占比极低时仍需可见，避免与无数据区域混淆。
const MIN_VISIBLE_RATIO = 0.14;

/** 跟随 documentElement 上的 dark 类切换地图色板 */
const useDarkMode = (): boolean => {
  const [isDark, setIsDark] = useState(
    () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  );

  useEffect(() => {
    const root = document.documentElement;
    const sync = () => setIsDark(root.classList.contains('dark'));

    sync();
    const observer = new MutationObserver(sync);
    observer.observe(root, { attributes: true, attributeFilter: ['class'] });

    return () => observer.disconnect();
  }, []);

  return isDark;
};

const clampPercent = (value: number) => Math.min(Math.max(value, 0), 100);

const hexToRgb = (hex: string): [number, number, number] => [
  parseInt(hex.slice(1, 3), 16),
  parseInt(hex.slice(3, 5), 16),
  parseInt(hex.slice(5, 7), 16),
];

const mixColor = (from: string, to: string, ratio: number): string => {
  const [fromRed, fromGreen, fromBlue] = hexToRgb(from);
  const [toRed, toGreen, toBlue] = hexToRgb(to);
  const channel = (start: number, end: number) => Math.round(start + (end - start) * ratio);

  return `rgb(${channel(fromRed, toRed)}, ${channel(fromGreen, toGreen)}, ${channel(fromBlue, toBlue)})`;
};

const colorForRatio = (stops: string[], ratio: number): string => {
  const bounded = Math.min(Math.max(ratio, 0), 1);
  const scaled = bounded * (stops.length - 1);
  const index = Math.min(Math.floor(scaled), stops.length - 2);
  return mixColor(stops[index], stops[index + 1], scaled - index);
};

export const WorldMap: React.FC<WorldMapProps> = ({ geoData, range, onRangeChange }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';

  const geometry = useMemo(() => getWorldMapGeometry(), []);
  const containerRef = useRef<HTMLDivElement>(null);
  const [hovered, setHovered] = useState<HoveredCountry | null>(null);

  const isDark = useDarkMode();
  const palette = isDark ? DARK_MAP_PALETTE : LIGHT_MAP_PALETTE;
  const legendGradient = useMemo(
    () => `linear-gradient(to right, ${palette.stops.join(', ')})`,
    [palette]
  );

  // 过滤掉 "default" / "eu" 这类无法对应国家多边形的路由分桶。
  const countryGeoData = useMemo(() => {
    return (geoData || []).filter((item) => {
      const code = item.code?.toLowerCase().trim();
      return Boolean(code && CODE_TO_GEO_NAME[code] && (item.queries || 0) > 0);
    });
  }, [geoData]);

  const totalQueries = useMemo(() => {
    return (geoData || []).reduce((acc, cur) => acc + (cur.queries || 0), 0);
  }, [geoData]);

  const topCountries = useMemo(() => {
    return [...countryGeoData]
      .sort((a, b) => (b.queries || 0) - (a.queries || 0))
      .slice(0, 8);
  }, [countryGeoData]);

  // 同一多边形可能对应多个国家码（例如 cn / hk / mo），需要合并统计。
  const countryData = useMemo(() => {
    const aggregated = new Map<string, CountryDatum>();

    countryGeoData.forEach((item) => {
      const code = item.code?.toLowerCase().trim();
      if (!code) return;

      const geoName = CODE_TO_GEO_NAME[code];
      if (!geoName) return;

      const queries = Number(item.queries) || 0;
      const apiPercent = Number(item.percent);
      const percent = clampPercent(
        Number.isFinite(apiPercent)
          ? apiPercent
          : totalQueries > 0
            ? (queries / totalQueries) * 100
            : 0
      );

      const existing = aggregated.get(geoName);
      if (existing) {
        existing.queries += queries;
        existing.percent = clampPercent(existing.percent + percent);
        return;
      }

      aggregated.set(geoName, { code, queries, percent });
    });

    return aggregated;
  }, [countryGeoData, totalQueries]);

  const maxPercent = useMemo(() => {
    let max = 0;
    countryData.forEach((datum) => {
      if (datum.percent > max) max = datum.percent;
    });
    return max;
  }, [countryData]);

  const renderedCountries = useMemo(() => {
    return geometry.countries
      .map((country) => ({ country, datum: countryData.get(country.name) }))
      .filter(({ country, datum }) => country.name !== 'Antarctica' || Boolean(datum));
  }, [geometry, countryData]);

  const fillForDatum = (datum?: CountryDatum): string => {
    if (!datum || datum.queries <= 0) return palette.empty;
    if (maxPercent <= 0) return colorForRatio(palette.stops, MIN_VISIBLE_RATIO);

    const relative = Math.pow(datum.percent / maxPercent, 0.7);
    return colorForRatio(
      palette.stops,
      MIN_VISIBLE_RATIO + relative * (1 - MIN_VISIBLE_RATIO)
    );
  };

  const showTooltip = (name: string, datum: CountryDatum | undefined, x: number, y: number) => {
    const container = containerRef.current;
    const width = container ? container.getBoundingClientRect().width : 0;

    setHovered({
      name,
      code: datum?.code,
      queries: datum?.queries ?? 0,
      percent: datum?.percent ?? 0,
      x: width > 0 ? Math.min(Math.max(x, 88), width - 88) : x,
      y,
    });
  };

  const handlePointerMove = (
    event: React.MouseEvent<SVGPathElement>,
    name: string,
    datum?: CountryDatum
  ) => {
    const container = containerRef.current;
    if (!container) return;

    const rect = container.getBoundingClientRect();
    showTooltip(name, datum, event.clientX - rect.left, event.clientY - rect.top);
  };

  const handleFocus = (
    name: string,
    centroid: [number, number],
    datum?: CountryDatum
  ) => {
    const container = containerRef.current;
    if (!container) return;

    const rect = container.getBoundingClientRect();
    const scale = rect.width / geometry.width;
    showTooltip(name, datum, centroid[0] * scale, centroid[1] * scale);
  };

  const hoveredCode = hovered
    ? getCountryCodeByName(hovered.name) || hovered.code || ''
    : '';
  const hoveredRegion = hoveredCode ? getRegionOption(hoveredCode) : null;

  const totalRequestIndicator = (
    <div className="text-primary font-bold text-xs font-mono" title={formatFull(totalQueries)}>
      {formatCompact(totalQueries)}{' '}
      <span className="text-secondary font-normal">{t('worldmap.total_reqs')}</span>
    </div>
  );

  return (
    <div className="geist-card p-6 space-y-6">
      {/* Header */}
      <PageHeader
        variant="section"
        icon={<Globe className="w-5 h-5 text-primary flex-shrink-0" />}
        title={t('worldmap.title')}
        subtitle={t('worldmap.subtitle')}
      >
        <div className="flex flex-wrap items-center justify-end gap-2">
          {range && onRangeChange && <RangeSelector value={range} onChange={onRangeChange} />}
          {totalRequestIndicator}
        </div>
      </PageHeader>

      {/* Map + Ranking Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* SVG Choropleth (d3-geo Natural Earth projection) */}
        <div
          ref={containerRef}
          className="lg:col-span-2 relative bg-card border border-border rounded-md p-2"
        >
          <svg
            viewBox={`0 0 ${geometry.width} ${geometry.height}`}
            className="block w-full h-auto"
            role="img"
            aria-label={
              isZh
                ? '按国家 / 地区请求占比着色的全球分布图'
                : 'World choropleth shaded by request share per country or region'
            }
            onMouseLeave={() => setHovered(null)}
          >
            {renderedCountries.map(({ country, datum }) => {
              const isActive = hovered?.name === country.name;
              const hasData = Boolean(datum && datum.queries > 0);

              return (
                <path
                  key={country.key}
                  d={country.d}
                  fill={fillForDatum(datum)}
                  stroke={isActive ? palette.borderActive : palette.border}
                  strokeWidth={isActive ? 1.4 : 0.7}
                  strokeLinejoin="round"
                  vectorEffect="non-scaling-stroke"
                  tabIndex={hasData ? 0 : -1}
                  className={
                    hasData
                      ? 'cursor-pointer outline-none focus-visible:stroke-[#1296db]'
                      : 'outline-none'
                  }
                  onMouseMove={(event) => handlePointerMove(event, country.name, datum)}
                  onFocus={() => handleFocus(country.name, country.centroid, datum)}
                  onBlur={() => setHovered(null)}
                >
                  <title>
                    {`${country.name}: ${(datum?.queries ?? 0).toLocaleString()} (${clampPercent(
                      datum?.percent ?? 0
                    ).toFixed(1)}%)`}
                  </title>
                </path>
              );
            })}
          </svg>

          {/* Legend */}
          <div className="absolute right-4 bottom-3 flex items-center gap-2 font-mono text-[10px] text-tertiary">
            <span>{t('worldmap.legend_low')}</span>
            <span
              className="h-2 w-24 rounded-sm border border-border"
              style={{ background: legendGradient }}
            />
            <span>{t('worldmap.legend_high')}</span>
            <span className="text-secondary">
              {maxPercent > 0 ? `${maxPercent.toFixed(1)}%` : '0%'}
            </span>
          </div>

          {/* Hover / focus tooltip */}
          {hovered && (
            <div
              className="pointer-events-none absolute z-20 min-w-[150px] rounded-md border border-border-muted bg-card px-3 py-2 font-mono text-[11px] shadow-popover"
              style={{
                left: hovered.x,
                top: hovered.y,
                transform: 'translate(-50%, calc(-100% - 12px))',
              }}
            >
              <div className="flex items-center gap-1.5 text-primary font-bold">
                <span>{hoveredRegion?.flag || '🌐'}</span>
                <span className="truncate">
                  {hoveredRegion ? getRegionDisplayName(hoveredRegion, isZh) : hovered.name}
                </span>
              </div>
              <div className="mt-1 flex items-center justify-between gap-4 text-secondary">
                <span>{t('worldmap.total_reqs')}</span>
                <span className="text-primary font-bold" title={formatFull(hovered.queries)}>
                  {formatCompact(hovered.queries)}
                </span>
              </div>
              <div className="flex items-center justify-between gap-4 text-secondary">
                <span>{t('worldmap.share')}</span>
                <span className="font-bold text-[#1296db]">
                  {clampPercent(hovered.percent).toFixed(1)}%
                </span>
              </div>
            </div>
          )}
        </div>

        {/* Top 8 Country Leaderboard */}
        <div className="space-y-3 font-mono text-xs flex flex-col">
          <div className="space-y-2">
            <div className="flex items-center justify-between pb-2 border-b border-border">
              <span className="font-bold text-primary flex items-center gap-1.5">
                <TrendingUp className="w-3.5 h-3.5 text-[#1296db]" />
                <span>{t('worldmap.leaderboard_title')}</span>
              </span>
              <span className="text-tertiary text-[11px]">Anycast</span>
            </div>

            <div className="space-y-3 max-h-[300px] overflow-y-auto pt-1">
              {topCountries.map((item, idx) => {
                const opt = getRegionOption(item.code);
                const percent = clampPercent(Number(item.percent) || 0);

                return (
                  <div key={item.code} className="space-y-1">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="text-tertiary text-[10px] w-3 text-right">{idx + 1}</span>
                        <span className="text-sm">{opt.flag}</span>
                        <span className="text-primary font-medium truncate">
                          {getRegionDisplayName(opt, isZh)}
                        </span>
                      </div>
                      <div className="shrink-0 text-secondary" title={formatFull(item.queries)}>
                        {formatCompact(item.queries)}{' '}
                        <span className="text-tertiary">({percent.toFixed(1)}%)</span>
                      </div>
                    </div>
                    <Progress value={percent} max={100} indicatorClassName="bg-[#1296db]" />
                  </div>
                );
              })}

              {topCountries.length === 0 && (
                <div className="text-center text-secondary py-8">
                  {t('worldmap.leaderboard_empty')}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
