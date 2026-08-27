import React, { useState, useEffect } from 'react';
import {
  Zap,
  TrendingUp,
  ShieldCheck,
  ListOrdered
} from 'lucide-react';
import { Domain, DomainStats, GeoBreakdown, QPSTrendPoint, TypeBreakdown, TrendRange } from '../types';
import { StatCard } from '../components/GeistUI';
import { WorldMap } from '../components/WorldMap';
import { QpsTrendChart } from '../components/QpsTrendChart';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { rangeRefreshMs } from '../components/RangeSelector';
import { TrafficBreakdownGrid } from '../components/TrafficBreakdownGrid';
import { formatCompact } from '../lib/format';

interface DomainDashboardPageProps {
  domain: Domain | null;
}

export const DomainDashboardPage: React.FC<DomainDashboardPageProps> = ({ domain }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';

  const [stats, setStats] = useState<DomainStats | null>(null);
  const [mapGeo, setMapGeo] = useState<GeoBreakdown[]>([]);
  const [countryGeo, setCountryGeo] = useState<GeoBreakdown[]>([]);
  const [types, setTypes] = useState<TypeBreakdown[]>([]);
  const [trendPoints, setTrendPoints] = useState<QPSTrendPoint[]>([]);
  const [mapRange, setMapRange] = useState<TrendRange>('24h');
  const [countryRange, setCountryRange] = useState<TrendRange>('24h');
  const [typeRange, setTypeRange] = useState<TrendRange>('24h');
  const [trendRange, setTrendRange] = useState<TrendRange>('24h');

  const loadStats = async () => {
    if (!domain) return;

    try {
      const res = await api.getDomainStats(domain.id);
      setStats(res);
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    if (!domain) return;

    loadStats();
    const interval = setInterval(loadStats, 3000);
    return () => clearInterval(interval);
  }, [domain?.id]);

  useEffect(() => {
    if (!domain) return;
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getDomainStats(domain.id, mapRange);
        if (!cancelled) setMapGeo(res.geo || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(mapRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [domain?.id, mapRange]);

  useEffect(() => {
    if (!domain) return;
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getDomainStats(domain.id, countryRange);
        if (!cancelled) setCountryGeo(res.geo || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(countryRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [domain?.id, countryRange]);

  useEffect(() => {
    if (!domain) return;
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getDomainStats(domain.id, typeRange);
        if (!cancelled) setTypes(res.types || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(typeRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [domain?.id, typeRange]);

  useEffect(() => {
    if (!domain) return;
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getDomainStats(domain.id, trendRange);
        if (!cancelled) setTrendPoints(res.points || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(trendRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [domain?.id, trendRange]);

  if (!domain) {
    return (
      <div className="geist-card p-8 text-center text-secondary text-xs font-mono">
        {isZh ? '请先在左侧选择一个托管域名。' : 'Select a hosted domain first.'}
      </div>
    );
  }

  const summary = stats?.summary;

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 1. Domain Live Metrics */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title={isZh ? '本域名实时 QPS' : 'Zone QPS'}
          value={summary ? `${formatCompact(summary.qps)} QPS` : '0 QPS'}
          subValue={isZh ? '60 秒滑动窗口（含边缘节点）' : 'Rolling 60s window incl. edge PoPs'}
          icon={<Zap className="w-4 h-4 text-amber-500" />}
        />
        <StatCard
          title={isZh ? '本域名累计查询量' : 'Zone Total Queries'}
          value={summary ? formatCompact(summary.total_queries) : '0'}
          subValue={isZh ? '主控与全部边缘节点聚合' : 'Aggregated across master and edges'}
          icon={<TrendingUp className="w-4 h-4 text-blue-500" />}
        />
        <StatCard
          title={isZh ? '解析记录数' : 'DNS Records'}
          value={summary ? summary.record_count.toLocaleString() : '0'}
          subValue={isZh ? '当前区域已配置记录' : 'Configured records in this zone'}
          icon={<ListOrdered className="w-4 h-4 text-green-500" />}
        />
        <StatCard
          title="DNSSEC"
          value={
            domain.dnssec_enabled
              ? (isZh ? '已开启' : 'Enabled')
              : (isZh ? '未开启' : 'Disabled')
          }
          subValue={
            summary
              ? `${isZh ? '签名密钥' : 'Signing keys'}: ${summary.dnssec_key_count}`
              : undefined
          }
          icon={<ShieldCheck className="w-4 h-4 text-purple-500" />}
        />
      </div>

      {/* 3. Geographic Distribution for this zone */}
      <WorldMap geoData={mapGeo} range={mapRange} onRangeChange={setMapRange} />

      {/* 与全局仪表盘同款趋势卡片，数据范围由域名统计接口驱动。 */}
      <QpsTrendChart
        points={trendPoints}
        metric="queries"
        range={trendRange}
        onRangeChange={setTrendRange}
        title={isZh ? '本域名解析趋势' : 'Zone Query Trend'}
        subtitle={
          isZh
            ? '主控与全部边缘节点聚合 · 按所选时间范围统计'
            : 'Aggregated across master and edges · selected time range'
        }
      />

      <TrafficBreakdownGrid
        geoData={countryGeo}
        types={types}
        countryRange={countryRange}
        typeRange={typeRange}
        onCountryRangeChange={setCountryRange}
        onTypeRangeChange={setTypeRange}
        countryTitle={isZh ? '请求来源国家 / 地区分布' : 'Top Countries / Regions'}
        typeTitle={isZh ? 'DNS 查询类型分布' : 'DNS Query Type Distribution'}
        countryEmpty={isZh ? '暂无该域名的国家 / 地区流量数据' : 'No country traffic recorded'}
        typeEmpty={isZh ? '暂无该域名的请求类型记录' : 'No query types recorded'}
      />
    </div>
  );
};
