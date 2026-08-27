import React, { useState, useEffect } from 'react';
import {
  Globe,
  Server,
  Zap,
  TrendingUp
} from 'lucide-react';
import {
  SummaryStats,
  Domain,
  ClusterNode,
  GeoBreakdown,
  TypeBreakdown,
  TrendRange
} from '../types';
import { StatCard } from '../components/GeistUI';
import { WorldMap } from '../components/WorldMap';
import { QpsTrendChart } from '../components/QpsTrendChart';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { rangeRefreshMs } from '../components/RangeSelector';
import { TrafficBreakdownGrid } from '../components/TrafficBreakdownGrid';
import { formatCompact } from '../lib/format';

interface OverviewPageProps {
  stats: SummaryStats | null;
  domains: Domain[];
  nodes: ClusterNode[];
  onSelectDomain: (d: Domain) => void;
  onOpenAddDomain: () => void;
  onGoToNodes: () => void;
  onGoToAnalytics: () => void;
}

export const OverviewPage: React.FC<OverviewPageProps> = ({
  stats,
  domains,
  nodes,
  onSelectDomain,
  onOpenAddDomain,
  onGoToNodes,
  onGoToAnalytics,
}) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';

  // Analytics states integrated into Overview
  const [mapGeoData, setMapGeoData] = useState<GeoBreakdown[]>([]);
  const [countryGeoData, setCountryGeoData] = useState<GeoBreakdown[]>([]);
  const [types, setTypes] = useState<TypeBreakdown[]>([]);
  const [mapRange, setMapRange] = useState<TrendRange>('24h');
  const [countryRange, setCountryRange] = useState<TrendRange>('24h');
  const [typeRange, setTypeRange] = useState<TrendRange>('24h');
  // 本地维护实时汇总，使顶部指标卡也能随轮询自动更新，
  // 不再依赖父组件仅在登录时加载一次的 stats。
  const [liveStats, setLiveStats] = useState<SummaryStats | null>(stats);

  const loadSummary = async () => {
    try {
      setLiveStats(await api.getSummary());
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    loadSummary();
    const interval = setInterval(loadSummary, 3000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getGeoBreakdown(mapRange);
        if (!cancelled) setMapGeoData(res.geo || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(mapRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [mapRange]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getGeoBreakdown(countryRange);
        if (!cancelled) setCountryGeoData(res.geo || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(countryRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [countryRange]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.getTypeBreakdown(typeRange);
        if (!cancelled) setTypes(res.types || []);
      } catch (err) {
        console.error(err);
      }
    };
    load();
    const interval = setInterval(load, rangeRefreshMs(typeRange));
    return () => { cancelled = true; clearInterval(interval); };
  }, [typeRange]);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 1. Real-time Live Metrics Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title={t('overview.total_qps')}
          value={liveStats ? `${formatCompact(liveStats.qps)} QPS` : '0 QPS'}
          subValue={isZh ? '实时 60 秒滑动窗口吞吐' : 'Real-time rolling 60s window'}
          icon={<Zap className="w-4 h-4 text-amber-500" />}
        />
        <StatCard
          title={t('overview.total_queries')}
          value={liveStats ? formatCompact(liveStats.total_queries) : '0'}
          subValue={isZh ? '全网累计处理请求数' : 'Live processed query counter'}
          icon={<TrendingUp className="w-4 h-4 text-blue-500" />}
        />
        <StatCard
          title={t('overview.managed_domains')}
          value={liveStats ? liveStats.domain_count : domains.length}
          subValue={isZh ? '当前已接入权威区域数' : 'Authoritative zone count'}
          icon={<Globe className="w-4 h-4 text-green-500" />}
        />
        <StatCard
          title={t('overview.online_nodes')}
          value={liveStats ? `${liveStats.online_node_count} / ${liveStats.node_count}` : '0 / 0'}
          subValue={isZh ? '在线边缘 Anycast 节点' : 'Active Anycast edge nodes'}
          icon={<Server className="w-4 h-4 text-purple-500" />}
        />
      </div>

      {/* 2. Interactive Cloudflare-Style Global World Map & Traffic Heatmap */}
      <WorldMap geoData={mapGeoData} range={mapRange} onRangeChange={setMapRange} />

      {/* 2. QPS Trend (SafeLine-style live area chart with range selector) */}
      <QpsTrendChart />

      <TrafficBreakdownGrid
        geoData={countryGeoData}
        types={types}
        countryRange={countryRange}
        typeRange={typeRange}
        onCountryRangeChange={setCountryRange}
        onTypeRangeChange={setTypeRange}
      />

    </div>
  );
};
