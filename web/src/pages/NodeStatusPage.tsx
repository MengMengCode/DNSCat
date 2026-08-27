import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import ReactEChartsCore from 'echarts-for-react/lib/core';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import {
  ArrowLeft,
  Cpu,
  MemoryStick,
  HardDrive,
  Network,
  Activity,
  Timer,
  Terminal,
  Server,
} from 'lucide-react';
import { NodeMetricPoint, NodeMetricsResponse, TrendRange } from '../types';
import { Button, Badge, Modal, CodeBox } from '../components/GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { formatCompact, formatFull } from '../lib/format';
import { useDialog } from '../components/DialogProvider';

// 按需注册 echarts 模块，控制打包体积（与 QpsTrendChart 保持一致的做法）。
echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer]);

const RANGES: { key: TrendRange; label: string }[] = [
  { key: '1h', label: '1H' },
  { key: '12h', label: '12H' },
  { key: '24h', label: '24H' },
  { key: '3d', label: '3D' },
  { key: '7d', label: '7D' },
  { key: '14d', label: '14D' },
  { key: '30d', label: '30D' },
];

// 短区间刷新更频繁以获得实时感；长区间变化缓慢，降低请求频率。
const refreshIntervalMs = (range: TrendRange): number =>
  range === '1h' || range === '12h' || range === '24h' ? 10000 : 60000;

const useIsDark = (): boolean => {
  const [isDark, setIsDark] = useState(
    () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  );
  useEffect(() => {
    const el = document.documentElement;
    const observer = new MutationObserver(() => setIsDark(el.classList.contains('dark')));
    observer.observe(el, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);
  return isDark;
};

// 把字节/秒格式化为可读速率。
const formatBps = (bps: number): string => {
  if (!bps || bps <= 0) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let value = bps;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
};

// 依据区间跨度决定横轴标签粒度：一天以内显示时:分，跨天显示月-日 时:分。
const formatAxisLabel = (ts: number, range: TrendRange): string => {
  const d = new Date(ts * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  if (range === '1h' || range === '12h' || range === '24h') {
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

export const NodeStatusPage: React.FC = () => {
  const { nodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert } = useDialog();
  const isDark = useIsDark();

  const [range, setRange] = useState<TrendRange>('24h');
  const [data, setData] = useState<NodeMetricsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [script, setScript] = useState<string | null>(null);

  // 供轮询回调读取最新 range，避免闭包捕获旧值。
  const rangeRef = useRef<TrendRange>(range);
  rangeRef.current = range;

  useEffect(() => {
    if (!nodeId) return;
    let cancelled = false;

    const load = async () => {
      try {
        const res = await api.getNodeMetrics(nodeId, rangeRef.current);
        if (!cancelled) {
          setData(res);
          setNotFound(false);
          setLoading(false);
        }
      } catch (err: any) {
        if (!cancelled) {
          // 节点不存在时给出明确空态，而不是一直转圈。
          if (String(err?.message || '').includes('not found')) setNotFound(true);
          setLoading(false);
        }
      }
    };

    setLoading(true);
    load();
    const timer = setInterval(load, refreshIntervalMs(range));
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [nodeId, range]);

  const points: NodeMetricPoint[] = data?.points || [];
  const node = data?.node;

  const axisColor = isDark ? '#6b7280' : '#8f8f8f';
  const gridColor = isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.06)';

  const labels = useMemo(() => points.map((p) => formatAxisLabel(p.ts, range)), [points, range]);

  // 统一的图表配置生成器：series 由各图表按需传入。
  // 无采样的桶用 null，echarts 会断开曲线，避免把缺数据画成 0。
  const buildOption = (
    series: {
      name: string;
      color: string;
      data: (number | null)[];
      area?: boolean;
    }[],
    opts: { yMax?: number; yFormatter?: (v: number) => string; showLegend?: boolean } = {}
  ) => ({
    animationDuration: 300,
    grid: { top: opts.showLegend ? 32 : 12, right: 14, bottom: 22, left: 52 },
    legend: opts.showLegend
      ? {
          top: 0,
          right: 0,
          itemWidth: 10,
          itemHeight: 10,
          textStyle: { color: axisColor, fontSize: 10 },
        }
      : undefined,
    tooltip: {
      trigger: 'axis',
      backgroundColor: isDark ? 'rgba(10,10,10,0.92)' : 'rgba(255,255,255,0.98)',
      borderColor: isDark ? '#242424' : '#eaeaea',
      borderWidth: 1,
      padding: [8, 12],
      textStyle: { color: isDark ? '#ededed' : '#171717', fontSize: 11 },
      formatter: (params: any) => {
        const arr = Array.isArray(params) ? params : [params];
        const idx = arr[0]?.dataIndex ?? 0;
        const p = points[idx];
        if (!p) return '';
        const when = new Date(p.ts * 1000).toLocaleString();
        const rows = arr
          .filter((s: any) => s.value !== null && s.value !== undefined)
          .map((s: any) => {
            const shown = opts.yFormatter ? opts.yFormatter(s.value) : s.value;
            return `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${s.color};margin-right:6px"></span>${s.seriesName}: <b>${shown}</b></div>`;
          })
          .join('');
        const empty = !p.has
          ? `<div style="opacity:.6">${isZh ? '该时段无采样' : 'No samples'}</div>`
          : '';
        return `<div style="font-family:monospace"><div style="opacity:.7;margin-bottom:2px">${when}</div>${rows}${empty}</div>`;
      },
    },
    xAxis: {
      type: 'category',
      data: labels,
      boundaryGap: false,
      axisTick: { show: false },
      axisLine: { lineStyle: { color: gridColor } },
      axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      max: opts.yMax,
      splitLine: { lineStyle: { color: gridColor } },
      axisLabel: {
        color: axisColor,
        fontSize: 10,
        formatter: opts.yFormatter ? (v: number) => opts.yFormatter!(v) : undefined,
      },
    },
    series: series.map((s) => ({
      name: s.name,
      type: 'line',
      smooth: true,
      showSymbol: false,
      sampling: 'lttb',
      connectNulls: false,
      data: s.data,
      lineStyle: { width: 2, color: s.color },
      itemStyle: { color: s.color },
      areaStyle: s.area
        ? {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: `${s.color}59` },
              { offset: 1, color: `${s.color}05` },
            ]),
          }
        : undefined,
    })),
  });

  // 取值辅助：无采样桶返回 null 以断开曲线。
  const pick = (fn: (p: NodeMetricPoint) => number) =>
    points.map((p) => (p.has ? fn(p) : null));

  const pctFormatter = (v: number) => `${Math.round(v)}%`;

  const cpuOption = useMemo(
    () => buildOption([{ name: 'CPU', color: '#3b82f6', data: pick((p) => p.cpu_usage), area: true }],
      { yMax: 100, yFormatter: pctFormatter }),
    [points, isDark, labels]
  );

  const memOption = useMemo(
    () => buildOption([{ name: isZh ? '内存' : 'RAM', color: '#8b5cf6', data: pick((p) => p.memory_usage), area: true }],
      { yMax: 100, yFormatter: pctFormatter }),
    [points, isDark, labels, isZh]
  );

  const diskOption = useMemo(
    () => buildOption([{ name: isZh ? '磁盘' : 'Disk', color: '#f59e0b', data: pick((p) => p.disk_usage), area: true }],
      { yMax: 100, yFormatter: pctFormatter }),
    [points, isDark, labels, isZh]
  );

  const netOption = useMemo(
    () =>
      buildOption(
        [
          { name: isZh ? '入向' : 'RX', color: '#10b981', data: pick((p) => p.net_rx_bps) },
          { name: isZh ? '出向' : 'TX', color: '#ef4444', data: pick((p) => p.net_tx_bps) },
        ],
        { yFormatter: formatBps, showLegend: true }
      ),
    [points, isDark, labels, isZh]
  );

  const qpsOption = useMemo(
    () => buildOption([{ name: 'QPS', color: '#06b6d4', data: pick((p) => p.qps), area: true }]),
    [points, isDark, labels]
  );

  const latencyOption = useMemo(
    () =>
      buildOption(
        [{ name: isZh ? '响应延迟' : 'Latency', color: '#ec4899', data: pick((p) => p.latency_ms), area: true }],
        { yFormatter: (v: number) => `${Math.round(v)}ms` }
      ),
    [points, isDark, labels, isZh]
  );

  const handleViewScript = async () => {
    if (!nodeId) return;
    try {
      const res = await api.getInstallScript(nodeId);
      setScript(res.script);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  const hasAnySample = (data?.summary.sample_count || 0) > 0;

  // 单个图表卡片：标题 + 当前值 + 曲线。
  const ChartCard: React.FC<{
    icon: React.ReactNode;
    title: string;
    current?: string;
    option: any;
  }> = ({ icon, title, current, option }) => (
    <div className="geist-card p-4 space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-bold text-primary uppercase tracking-wide flex items-center gap-1.5">
          <span className="flex-shrink-0">{icon}</span>
          {title}
        </h3>
        {current !== undefined && (
          <span className="text-xs font-bold text-primary">{current}</span>
        )}
      </div>
      <div className="h-40 w-full">
        {!loading && !hasAnySample ? (
          <div className="h-full flex items-center justify-center text-xs text-tertiary">
            {isZh ? '该时间范围内暂无采样数据' : 'No samples in this range'}
          </div>
        ) : (
          <ReactEChartsCore
            echarts={echarts}
            option={option}
            notMerge
            lazyUpdate
            style={{ height: '100%', width: '100%' }}
            opts={{ renderer: 'canvas' }}
          />
        )}
      </div>
    </div>
  );

  if (notFound) {
    return (
      <div className="space-y-6 animate-in fade-in duration-150">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold text-primary truncate">{t('nav.nodes')}</h2>
          <Button size="sm" variant="secondary" onClick={() => navigate('/nodes')} icon={<ArrowLeft className="w-3.5 h-3.5" />}>
            {isZh ? '返回节点列表' : 'Back to Nodes'}
          </Button>
        </div>
        <div className="geist-card p-10 text-center text-sm text-secondary">
          {isZh ? '未找到该边缘节点。' : 'Edge node not found.'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 标题（左）+ 返回/操作（右）：行高由按钮决定 */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h2 className="text-sm font-semibold text-primary truncate flex items-center gap-2">
          <Server className="w-4 h-4 text-secondary flex-shrink-0" />
          {node?.name || nodeId}
          {node && (
            <Badge variant={node.is_online ? 'success' : 'default'} size="sm">
              {node.is_online ? t('common.active') : t('common.pending')}
            </Badge>
          )}
        </h2>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" onClick={handleViewScript} icon={<Terminal className="w-3.5 h-3.5" />}>
            {t('nodes.deploy_cmd')}
          </Button>
          <Button size="sm" variant="secondary" onClick={() => navigate('/nodes')} icon={<ArrowLeft className="w-3.5 h-3.5" />}>
            {isZh ? '返回' : 'Back'}
          </Button>
        </div>
      </div>

      {/* 节点概览 + 时间范围选择 */}
      <div className="geist-card p-4 flex flex-col lg:flex-row lg:items-center justify-between gap-4">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-2 text-xs min-w-0">
          <div className="min-w-0">
            <div className="text-tertiary">{isZh ? '节点 ID' : 'Node ID'}</div>
            <div className="text-primary font-bold truncate">{node?.node_id || nodeId}</div>
          </div>
          <div className="min-w-0">
            <div className="text-tertiary">{isZh ? '公网 IP' : 'Public IP'}</div>
            <div className="text-primary font-bold truncate">{node?.ip || '-'}</div>
          </div>
          <div className="min-w-0">
            <div className="text-tertiary">{isZh ? '地区' : 'Region'}</div>
            <div className="text-primary font-bold truncate">{node?.region || '-'}</div>
          </div>
          <div className="min-w-0">
            <div className="text-tertiary">{isZh ? '最近心跳' : 'Last Heartbeat'}</div>
            <div className="text-primary font-bold truncate">
              {node?.last_heartbeat ? new Date(node.last_heartbeat).toLocaleString() : '-'}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-1 bg-bg-subtle border border-border rounded-sm p-0.5 self-start lg:self-center flex-shrink-0">
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
      </div>

      {/* 区间汇总：放在图表之前，先给出所选时间区间的整体结论，再看逐项曲线 */}
      {data && (
        <div className="geist-card p-4 grid grid-cols-2 sm:grid-cols-4 gap-4 text-xs">
          <div>
            <div className="text-tertiary">{isZh ? '区间平均 CPU' : 'Avg CPU'}</div>
            <div className="text-primary font-bold">{data.summary.avg_cpu.toFixed(1)}%</div>
          </div>
          <div>
            <div className="text-tertiary">{isZh ? '区间平均内存' : 'Avg Memory'}</div>
            <div className="text-primary font-bold">{data.summary.avg_memory.toFixed(1)}%</div>
          </div>
          <div>
            <div className="text-tertiary">{isZh ? '峰值 QPS' : 'Peak QPS'}</div>
            <div className="text-primary font-bold" title={formatFull(data.summary.peak_qps)}>
              {formatCompact(data.summary.peak_qps)}
            </div>
          </div>
          <div>
            <div className="text-tertiary">{isZh ? '峰值延迟' : 'Peak Latency'}</div>
            <div className="text-primary font-bold">{data.summary.peak_latency}ms</div>
          </div>
        </div>
      )}

      {/* 性能图表 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <ChartCard
          icon={<Cpu className="w-3.5 h-3.5 text-blue-500" />}
          title={isZh ? 'CPU 使用率' : 'CPU Usage'}
          current={node ? `${(node.cpu_usage || 0).toFixed(1)}%` : undefined}
          option={cpuOption}
        />
        <ChartCard
          icon={<MemoryStick className="w-3.5 h-3.5 text-violet-500" />}
          title={isZh ? '内存使用率' : 'Memory Usage'}
          current={node ? `${(node.memory_usage || 0).toFixed(1)}%` : undefined}
          option={memOption}
        />
        <ChartCard
          icon={<HardDrive className="w-3.5 h-3.5 text-amber-500" />}
          title={isZh ? '磁盘使用率' : 'Disk Usage'}
          current={node ? `${(node.disk_usage || 0).toFixed(1)}%` : undefined}
          option={diskOption}
        />
        <ChartCard
          icon={<Network className="w-3.5 h-3.5 text-emerald-500" />}
          title={isZh ? '网络吞吐' : 'Network Throughput'}
          current={
            node ? `↓${formatBps(node.net_rx_bps || 0)} ↑${formatBps(node.net_tx_bps || 0)}` : undefined
          }
          option={netOption}
        />
        <ChartCard
          icon={<Activity className="w-3.5 h-3.5 text-cyan-500" />}
          title={isZh ? '实时 QPS' : 'Live QPS'}
          current={node ? formatCompact(node.qps || 0) : undefined}
          option={qpsOption}
        />
        <ChartCard
          icon={<Timer className="w-3.5 h-3.5 text-pink-500" />}
          title={isZh ? '查询响应延迟' : 'Query Response Latency'}
          current={node ? `${node.latency_ms || 0}ms` : undefined}
          option={latencyOption}
        />
      </div>

      {/* 部署指令弹窗 */}
      <Modal
        isOpen={script !== null}
        onClose={() => setScript(null)}
        title={t('nodes.deploy_cmd')}
        description={isZh ? '在您的 VPS / 边缘服务器上运行此命令以接入集群' : 'Run this command on your VPS / edge server to connect to this cluster'}
        maxWidth="lg"
      >
        <div className="space-y-4">
          <CodeBox code={script || ''} />
          <div className="flex justify-end pt-2">
            <Button variant="primary" size="sm" onClick={() => setScript(null)}>
              {t('common.cancel')}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};
