import React, { useState, useEffect } from 'react';
import {
  Globe,
  PieChart,
  RefreshCw,
} from 'lucide-react';
import { TypeBreakdown, GeoBreakdown } from '../types';
import { Button, Badge } from '../components/GeistUI';
import { WorldMap } from '../components/WorldMap';
import { QpsTrendChart } from '../components/QpsTrendChart';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { getRegionOption } from '../components/FlagRegionSelect';
import { CODE_TO_GEO_NAME } from '../lib/countryMap';
import { formatCompact, formatFull } from '../lib/format';

export const AnalyticsPage: React.FC = () => {
  const { t, language } = useI18n();
  const [types, setTypes] = useState<TypeBreakdown[]>([]);
  const [geo, setGeo] = useState<GeoBreakdown[]>([]);
  const [loading, setLoading] = useState(true);

  const isZh = language === 'zh-CN';

  const loadAnalytics = async () => {
    try {
      setLoading(true);
      const [typesRes, geoRes] = await Promise.all([
        api.getTypeBreakdown(),
        api.getGeoBreakdown(),
      ]);
      setTypes(typesRes.types || []);
      setGeo(geoRes.geo || []);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAnalytics();
    const interval = setInterval(loadAnalytics, 10000);
    return () => clearInterval(interval);
  }, []);

  const totalQueries = types.reduce((acc, cur) => acc + (Number(cur.value) || 0), 0);

  const countryGeoData = geo.filter((item) =>
    Boolean(CODE_TO_GEO_NAME[item.code.toLowerCase()] && (item.queries || 0) > 0)
  );
  const topCountries = [...countryGeoData]
    .sort((a, b) => (b.queries || 0) - (a.queries || 0))
    .slice(0, 8);
  const totalGeoQueries = countryGeoData.reduce((acc, cur) => acc + (cur.queries || 0), 0);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.analytics')}</h2>
        <Button
          size="sm"
          variant="secondary"
          loading={loading}
          onClick={loadAnalytics}
          icon={<RefreshCw className="w-3.5 h-3.5" />}
        >
          {t('common.refresh')}
        </Button>
      </div>

      {/* 1. Cloudflare-Style Global World Map Canvas & Ranking */}
      <WorldMap geoData={geo} />

      {/* 2. QPS Trend (SafeLine-style live area chart with range selector) */}
      <QpsTrendChart />

      {/* 3. Breakdown Grid: Types & Top Countries */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Query Types Breakdown */}
        <div className="geist-card p-5 space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
              <PieChart className="w-4 h-4 text-purple-400" />
              {isZh ? 'DNS 记录类型分布' : 'Query Types Distribution'}
            </h3>
            <span className="text-xs text-tertiary font-mono">{totalQueries} {isZh ? '次' : 'Total'}</span>
          </div>

          <div className="space-y-3 font-mono text-xs max-h-64 overflow-y-auto pr-1">
            {types.map((tItem) => {
              const val = Number(tItem.value) || 0;
              const pct = totalQueries > 0 ? Math.round((val / totalQueries) * 100) : 0;

              return (
                <div key={tItem.name} className="space-y-1">
                  <div className="flex justify-between text-secondary">
                    <span className="text-primary font-bold">{tItem.name}</span>
                    <span title={formatFull(val)}>
                      {formatCompact(val)} {isZh ? '次' : 'reqs'} ({pct}%)
                    </span>
                  </div>
                  <div className="w-full h-1.5 bg-bg-subtle rounded-full overflow-hidden">
                    <div
                      style={{ width: `${Math.max(pct, val > 0 ? 3 : 0)}%` }}
                      className="h-full bg-purple-500 rounded-full transition-all"
                    />
                  </div>
                </div>
              );
            })}

            {types.length === 0 && (
              <div className="text-center text-secondary py-8 text-xs">
                {isZh ? '暂无 DNS 查询记录。' : 'No DNS queries recorded yet.'}
              </div>
            )}
          </div>
        </div>

        {/* Top Countries / Regions Breakdown */}
        <div className="geist-card p-5 space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
              <Globe className="w-4 h-4 text-blue-400" />
              {isZh ? '请求来源国家 / 地区分布' : 'Top Countries / Regions'}
            </h3>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 font-mono text-xs">
            {topCountries.map((item) => {
              const opt = getRegionOption(item.code);
              const label = isZh ? opt.nameZh : opt.nameEn;
              const queries = Number(item.queries) || 0;
              const pct = totalGeoQueries > 0
                ? Math.round((queries / totalGeoQueries) * 100)
                : 0;

              return (
                <div key={item.code} className="p-3 bg-bg-subtle border border-border rounded-sm space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-bold text-primary truncate flex items-center gap-1.5">
                      <span>{opt.flag}</span>
                      <span className="truncate">{label}</span>
                    </span>
                    <Badge variant="default" size="sm">
                      {pct}%
                    </Badge>
                  </div>
                  <div className="text-primary font-bold text-sm" title={formatFull(queries)}>
                    {formatCompact(queries)}{' '}
                    <span className="text-xs font-normal text-tertiary">{isZh ? '次请求' : 'reqs'}</span>
                  </div>
                  <div className="w-full h-1.5 bg-card rounded-full overflow-hidden">
                    <div
                      style={{ width: `${Math.max(pct, queries > 0 ? 3 : 0)}%` }}
                      className="h-full bg-blue-500 rounded-full transition-all"
                    />
                  </div>
                </div>
              );
            })}

            {topCountries.length === 0 && (
              <div className="col-span-full text-center text-secondary py-8 text-xs">
                {isZh ? '暂无国家 / 地区流量数据' : 'No country traffic recorded'}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};
