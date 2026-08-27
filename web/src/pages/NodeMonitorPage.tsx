import React, { useEffect, useRef, useState } from 'react';
import { Activity, RefreshCw, Server, Wifi, WifiOff } from 'lucide-react';
import { NodeUptime, NodeUptimeResponse, TrendRange, UptimeBucket, UptimeStatus } from '../types';
import { Badge } from '../components/GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';

// 时间范围选项，与 QpsTrendChart / 后端 uptimeRanges 保持一致。
const RANGES: { key: TrendRange; label: string }[] = [
  { key: '1h', label: '1H' },
  { key: '12h', label: '12H' },
  { key: '24h', label: '24H' },
  { key: '3d', label: '3D' },
  { key: '7d', label: '7D' },
  { key: '14d', label: '14D' },
  { key: '30d', label: '30D' },
];

// 状态条颜色：在线 / 离线 / 部分在线 / 无数据。用固定色值避免 Tailwind 动态类被裁剪。
const STATUS_COLOR: Record<UptimeStatus, string> = {
  up: '#22c55e',
  down: '#ef4444',
  partial: '#f59e0b',
  none: 'rgba(148,163,184,0.25)',
};

// 短区间刷新更频繁，长区间降低频率。
const refreshIntervalMs = (range: TrendRange): number =>
  range === '1h' || range === '12h' || range === '24h' ? 15000 : 60000;

const uptimeTextClass = (v: number, hasData: boolean): string => {
  if (!hasData) return 'text-tertiary';
  if (v >= 99.9) return 'text-green-500';
  if (v >= 99) return 'text-green-500';
  if (v >= 95) return 'text-amber-500';
  return 'text-red-500';
};

// 单个节点的在线状态条，带 hover tooltip。
const StatusBar: React.FC<{ buckets: UptimeBucket[]; isZh: boolean }> = ({ buckets, isZh }) => {
  const [hover, setHover] = useState<number | null>(null);
  const active = hover !== null ? buckets[hover] : null;

  const statusLabel = (s: UptimeStatus): string => {
    switch (s) {
      case 'up':
        return isZh ? '在线' : 'Operational';
      case 'down':
        return isZh ? '离线' : 'Down';
      case 'partial':
        return isZh ? '部分在线' : 'Partial';
      default:
        return isZh ? '无数据' : 'No data';
    }
  };

  return (
    <div className="relative">
      <div className="flex items-stretch gap-[2px] h-9 w-full">
        {buckets.map((b, i) => (
          <div
            key={i}
            onMouseEnter={() => setHover(i)}
            onMouseLeave={() => setHover((h) => (h === i ? null : h))}
            className="flex-1 min-w-[2px] rounded-[2px] cursor-pointer transition-opacity duration-100"
            style={{
              backgroundColor: STATUS_COLOR[b.status],
              opacity: hover === null || hover === i ? 1 : 0.45,
            }}
          />
        ))}
      </div>

      {active && (
        <div
          className="absolute z-30 bottom-full mb-2 -translate-x-1/2 pointer-events-none whitespace-nowrap rounded-sm border border-border bg-card shadow-xl px-2.5 py-1.5 text-[11px] font-mono"
          style={{ left: `${((hover! + 0.5) / buckets.length) * 100}%` }}
        >
          <div className="text-tertiary">{new Date(active.ts * 1000).toLocaleString()}</div>
          <div className="flex items-center gap-1.5 mt-0.5">
            <span
              className="inline-block w-2 h-2 rounded-full flex-shrink-0"
              style={{ backgroundColor: STATUS_COLOR[active.status] }}
            />
            <span className="text-primary font-semibold">{statusLabel(active.status)}</span>
            {active.total > 0 && <span className="text-secondary">· {active.uptime.toFixed(1)}%</span>}
          </div>
          {active.total > 0 && (
            <div className="text-tertiary mt-0.5">
              {isZh ? '采样' : 'Samples'}: {active.up}/{active.total}
              {active.avg_latency_ms > 0 && ` · ${active.avg_latency_ms}ms`}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

const NodeCard: React.FC<{ node: NodeUptime; isZh: boolean }> = ({ node, isZh }) => {
  const hasData = node.sample_count > 0;

  return (
    <div className="geist-card p-5 space-y-3">
      {/* 头部：节点信息 + 总在线率 */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2.5 min-w-0">
          <div className="mt-0.5 flex-shrink-0">
            {node.is_online ? (
              <Wifi className="w-4 h-4 text-green-500" />
            ) : (
              <WifiOff className="w-4 h-4 text-red-500" />
            )}
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-sm font-semibold text-primary truncate">{node.name}</span>
              <Badge variant={node.is_online ? 'success' : 'default'} size="sm">
                {node.is_online ? (isZh ? '在线' : 'Online') : (isZh ? '离线' : 'Offline')}
              </Badge>
              {node.region && (
                <Badge variant="default" size="sm">
                  {node.region}
                </Badge>
              )}
            </div>
            <div className="text-[11px] text-tertiary font-mono mt-0.5 truncate">
              {node.node_id} · {node.ip}
            </div>
          </div>
        </div>

        <div className="text-right flex-shrink-0">
          <div className={`text-lg font-bold font-mono leading-none ${uptimeTextClass(node.overall_uptime, hasData)}`}>
            {hasData ? `${node.overall_uptime.toFixed(2)}%` : '—'}
          </div>
          <div className="text-[10px] text-tertiary uppercase tracking-wider mt-1">
            {isZh ? '在线率' : 'Uptime'}
          </div>
        </div>
      </div>

      {/* 状态条 */}
      <StatusBar buckets={node.buckets} isZh={isZh} />

      {/* 底部：时间轴两端 + 延迟 */}
      <div className="flex items-center justify-between text-[10px] text-tertiary font-mono">
        <span>
          {node.buckets.length > 0
            ? new Date(node.buckets[0].ts * 1000).toLocaleString()
            : ''}
        </span>
        <span className="text-secondary">
          {isZh ? '平均延迟' : 'Avg latency'}:{' '}
          <span className="text-primary font-semibold">
            {node.avg_latency_ms > 0 ? `${node.avg_latency_ms}ms` : '—'}
          </span>
        </span>
        <span>
          {node.buckets.length > 0
            ? new Date(node.buckets[node.buckets.length - 1].ts * 1000).toLocaleString()
            : ''}
        </span>
      </div>
    </div>
  );
};

export const NodeMonitorPage: React.FC = () => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';

  const [range, setRange] = useState<TrendRange>('24h');
  const [data, setData] = useState<NodeUptimeResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const rangeRef = useRef<TrendRange>(range);
  rangeRef.current = range;

  useEffect(() => {
    let cancelled = false;

    const load = async (isAuto: boolean) => {
      try {
        if (!isAuto) setLoading(true);
        else setRefreshing(true);
        const res = await api.getNodeUptime(rangeRef.current);
        if (!cancelled) setData(res);
      } catch (err) {
        console.error(err);
      } finally {
        if (!cancelled) {
          setLoading(false);
          setRefreshing(false);
        }
      }
    };

    load(false);
    const timer = setInterval(() => load(true), refreshIntervalMs(range));
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [range]);

  const nodes = data?.nodes || [];
  const onlineCount = nodes.filter((n) => n.is_online).length;
  const nodesWithData = nodes.filter((n) => n.sample_count > 0);
  const avgUptime =
    nodesWithData.length > 0
      ? nodesWithData.reduce((sum, n) => sum + n.overall_uptime, 0) / nodesWithData.length
      : 0;

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 标题 + 时间范围选择器 */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Activity className="w-4 h-4 text-primary" />
            {t('nav.node_monitor')}
          </h2>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? '各边缘节点的历史在线状态与在线率，数据每 30 秒采样一次。'
              : 'Historical availability of each edge node. Sampled every 30 seconds.'}
          </p>
        </div>

        <div className="flex items-center gap-2 self-start">
          <div className="flex items-center gap-1 bg-bg-subtle border border-border rounded-sm p-0.5 font-mono">
            {RANGES.map((r) => (
              <button
                key={r.key}
                type="button"
                onClick={() => setRange(r.key)}
                className={`px-2.5 py-1 text-[11px] rounded-sm transition-colors cursor-pointer ${
                  range === r.key ? 'bg-primary text-bg font-semibold' : 'text-secondary hover:text-primary'
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>
          {refreshing && <RefreshCw className="w-3.5 h-3.5 text-tertiary animate-spin" />}
        </div>
      </div>

      {/* 概览统计 */}
      <div className="grid grid-cols-3 gap-3">
        <div className="geist-card p-4">
          <div className="text-[10px] text-tertiary uppercase tracking-wider">{isZh ? '节点总数' : 'Total Nodes'}</div>
          <div className="text-xl font-bold text-primary font-mono mt-1">{nodes.length}</div>
        </div>
        <div className="geist-card p-4">
          <div className="text-[10px] text-tertiary uppercase tracking-wider">{isZh ? '当前在线' : 'Online Now'}</div>
          <div className="text-xl font-bold font-mono mt-1">
            <span className="text-green-500">{onlineCount}</span>
            <span className="text-tertiary text-sm"> / {nodes.length}</span>
          </div>
        </div>
        <div className="geist-card p-4">
          <div className="text-[10px] text-tertiary uppercase tracking-wider">{isZh ? '平均在线率' : 'Avg Uptime'}</div>
          <div className={`text-xl font-bold font-mono mt-1 ${uptimeTextClass(avgUptime, nodesWithData.length > 0)}`}>
            {nodesWithData.length > 0 ? `${avgUptime.toFixed(2)}%` : '—'}
          </div>
        </div>
      </div>

      {/* 图例 */}
      <div className="flex items-center gap-4 text-[11px] text-secondary font-mono">
        {(['up', 'partial', 'down', 'none'] as UptimeStatus[]).map((s) => (
          <span key={s} className="flex items-center gap-1.5">
            <span className="inline-block w-2.5 h-2.5 rounded-[2px]" style={{ backgroundColor: STATUS_COLOR[s] }} />
            {s === 'up'
              ? isZh ? '在线' : 'Operational'
              : s === 'partial'
              ? isZh ? '部分在线' : 'Partial'
              : s === 'down'
              ? isZh ? '离线' : 'Down'
              : isZh ? '无数据' : 'No data'}
          </span>
        ))}
      </div>

      {/* 节点列表 */}
      {loading ? (
        <div className="flex items-center justify-center h-40 text-xs text-tertiary font-mono">
          {isZh ? '正在加载监控数据...' : 'Loading monitoring data...'}
        </div>
      ) : nodes.length === 0 ? (
        <div className="geist-card p-10 flex flex-col items-center justify-center text-center gap-2">
          <Server className="w-8 h-8 text-tertiary" />
          <div className="text-sm text-secondary">
            {isZh ? '暂无边缘节点接入' : 'No edge nodes connected'}
          </div>
          <div className="text-xs text-tertiary">
            {isZh
              ? '在「边缘节点集群」中部署节点后，此处将开始记录在线状态。'
              : 'Deploy nodes under "Edge Nodes" and their availability will be recorded here.'}
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          {nodes.map((node) => (
            <NodeCard key={node.node_id} node={node} isZh={isZh} />
          ))}
        </div>
      )}
    </div>
  );
};
