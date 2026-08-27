import React, { useEffect, useMemo, useRef, useState } from 'react';
import ReactEChartsCore from 'echarts-for-react/lib/core';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { Activity } from 'lucide-react';
import { QPSTrendPoint, TrendRange } from '../types';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';

// echarts 6 按需注册：只引入折线图 + 网格 + tooltip + Canvas 渲染器，控制打包体积。
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer]);

const RANGES: { key: TrendRange; label: string }[] = [
  { key: '1h', label: '1H' },
  { key: '12h', label: '12H' },
  { key: '24h', label: '24H' },
  { key: '3d', label: '3D' },
  { key: '7d', label: '7D' },
  { key: '14d', label: '14D' },
  { key: '30d', label: '30D' },
];

// 短区间数据变化快，刷新更频繁以获得「时间线不断推进」的实时感；
// 长区间变化缓慢，降低刷新频率减少无谓请求。
const refreshIntervalMs = (range: TrendRange): number =>
  range === '1h' || range === '12h' || range === '24h' ? 5000 : 60000;

// QPS 是速率量，低流量下天然是小数（一小时 120 次查询 ≈ 0.03 QPS）。
// 取整会把这类真实流量显示成 0；高流量时小数没有意义，改回整数显示。
const formatQps = (v: number): string =>
  v >= 10 ? Math.round(v).toLocaleString() : String(Number(v.toFixed(2)));

// 跟踪 <html> 上的 dark class，使 echarts 主题色能随明暗切换实时更新。
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

interface QpsTrendChartProps {
  className?: string;
  // 受控模式：外部直接提供数据点。提供后组件不再自行拉取全网 QPS。
  points?: QPSTrendPoint[];
  // 纵轴度量：'qps' 为每秒速率；'queries' 为每个时间桶的解析次数(success+blocked)。默认 'qps'。
  metric?: 'qps' | 'queries';
  // 覆盖默认标题 / 副标题（受控复用到域名级趋势时使用）。
  title?: string;
  subtitle?: string;
  range?: TrendRange;
  onRangeChange?: (range: TrendRange) => void;
}

export const QpsTrendChart: React.FC<QpsTrendChartProps> = ({
  className,
  points: externalPoints,
  metric = 'qps',
  title,
  subtitle,
  range: externalRange,
  onRangeChange,
}) => {
  const { t, language } = useI18n();
  const isDark = useIsDark();
  const isZh = language === 'zh-CN';
  // 受控：外部提供 points 时，组件只负责渲染；外部提供区间回调时仍显示同款筛选器。
  const controlled = externalPoints !== undefined;

  const [internalRange, setInternalRange] = useState<TrendRange>('24h');
  const range = externalRange ?? internalRange;
  const [fetchedPoints, setFetchedPoints] = useState<QPSTrendPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const points = controlled ? (externalPoints as QPSTrendPoint[]) : fetchedPoints;
  // 用 ref 保存当前 range，供轮询定时器回调读取最新值，避免闭包捕获旧值。
  const rangeRef = useRef<TrendRange>(range);
  rangeRef.current = range;

  useEffect(() => {
    // 受控模式下数据由外部驱动，无需自拉与轮询。
    if (controlled) {
      setLoading(false);
      return;
    }
    let cancelled = false;

    const load = async () => {
      try {
        const res = await api.getQPSTrend(rangeRef.current);
        if (!cancelled) {
          setFetchedPoints(res.points || []);
          setLoading(false);
        }
      } catch (err) {
        console.error(err);
        if (!cancelled) setLoading(false);
      }
    };

    setLoading(true);
    load();
    const timer = setInterval(load, refreshIntervalMs(range));
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [range, controlled]);

  // 纵轴取值：qps 用速率，queries 用该桶的解析次数(success+blocked)。
  const valueAt = (p: QPSTrendPoint) =>
    metric === 'queries' ? (p.success || 0) + (p.blocked || 0) : p.qps;
  // 度量值格式化：次数为整数，QPS 低值保留两位小数。
  const fmtValue = (v: number) =>
    metric === 'queries' ? Math.round(v).toLocaleString() : formatQps(v);

  const peakValue = useMemo(
    () => points.reduce((m, p) => Math.max(m, valueAt(p)), 0),
    [points, metric]
  );
  const avgValue = useMemo(() => {
    if (points.length === 0) return 0;
    return points.reduce((sum, p) => sum + valueAt(p), 0) / points.length;
  }, [points, metric]);

  const axisColor = isDark ? '#6b7280' : '#8f8f8f';
  const gridColor = isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.06)';
  const lineColor = '#3b82f6';

  const option = useMemo(() => {
    const labels = points.map((p) => p.timestamp);
    const values = points.map(valueAt);

    return {
      animationDuration: 300,
      grid: { top: 12, right: 14, bottom: 22, left: 46 },
      tooltip: {
        trigger: 'axis',
        backgroundColor: isDark ? 'rgba(10,10,10,0.92)' : 'rgba(255,255,255,0.98)',
        borderColor: isDark ? '#242424' : '#eaeaea',
        borderWidth: 1,
        padding: [8, 12],
        textStyle: { color: isDark ? '#ededed' : '#171717', fontSize: 11 },
        // 用完整时间 + QPS + 查询数组织 tooltip，时间取后端返回的 ts（unix 秒）。
        formatter: (params: any) => {
          const p = Array.isArray(params) ? params[0] : params;
          const idx = p?.dataIndex ?? 0;
          const point = points[idx];
          if (!point) return '';
          const when = point.ts
            ? new Date(point.ts * 1000).toLocaleString()
            : point.timestamp;
          const queries = (point.success || 0) + (point.blocked || 0);
          // queries 模式主数值就是次数；qps 模式主数值是速率，次数作为副行。
          const mainLine =
            metric === 'queries'
              ? `<div><b>${queries.toLocaleString()}</b> ${t('trend.queries')}</div>`
              : `<div><b>${formatQps(point.qps)}</b> ${t('trend.qps')}</div>
                 <div style="opacity:.7">${queries.toLocaleString()} ${t('trend.queries')}</div>`;
          return `<div style="font-family:monospace">
            <div style="opacity:.7;margin-bottom:2px">${when}</div>
            ${mainLine}
          </div>`;
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
        // 只有峰值够大才锁定整数刻度。峰值不足 1 QPS 时若强制 minInterval:1，
        // 轴范围会被撑到 [0,1]，曲线全被压在底部，看着像「没有数据」。
        minInterval: peakValue >= 5 ? 1 : undefined,
        splitLine: { lineStyle: { color: gridColor } },
        axisLabel: {
          color: axisColor,
          fontSize: 10,
          formatter: (v: number) => fmtValue(v),
        },
      },
      series: [
        {
          type: 'line',
          smooth: true,
          showSymbol: false,
          symbolSize: 6,
          sampling: 'lttb',
          data: values,
          lineStyle: { width: 2, color: lineColor },
          itemStyle: { color: lineColor },
          // 雷池风格：主色到透明的纵向渐变面积填充。
          areaStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: 'rgba(59,130,246,0.35)' },
              { offset: 1, color: 'rgba(59,130,246,0.02)' },
            ]),
          },
        },
      ],
    };
  }, [points, peakValue, metric, isDark, axisColor, gridColor, t]);

  return (
    <div className={`geist-card p-6 space-y-4 ${className || ''}`}>
      {/* Header: title + range selector */}
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Activity className="w-4 h-4 text-primary" />
            {title ?? t('trend.title')}
          </h3>
          <p className="text-xs text-secondary mt-0.5">{subtitle ?? t('trend.subtitle')}</p>
        </div>

        {(!controlled || (externalRange !== undefined && onRangeChange)) && (
          <div className="flex items-center gap-1 bg-bg-subtle border border-border rounded-sm p-0.5 font-mono self-start">
            {RANGES.map((r) => (
              <button
                key={r.key}
                type="button"
                onClick={() => onRangeChange ? onRangeChange(r.key) : setInternalRange(r.key)}
                className={`px-2.5 py-1 text-[11px] rounded-sm transition-colors cursor-pointer ${
                  range === r.key
                    ? 'bg-primary text-bg font-semibold'
                    : 'text-secondary hover:text-primary'
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Quick stats */}
      <div className="flex items-center gap-4 text-xs font-mono">
        <span className="text-tertiary">
          {metric === 'queries' ? (isZh ? '峰值' : 'Peak') : t('trend.peak_qps')}:{' '}
          <span className="text-primary font-semibold">{fmtValue(peakValue)}</span>
        </span>
        <span className="text-tertiary">
          {metric === 'queries' ? (isZh ? '均值' : 'Avg') : t('trend.avg_qps')}:{' '}
          <span className="text-primary font-semibold">{fmtValue(avgValue)}</span>
        </span>
      </div>

      {/* Chart / empty state */}
      <div className="h-64 w-full">
        {!loading && points.length === 0 ? (
          <div className="h-full flex items-center justify-center text-xs text-tertiary font-mono">
            {t('trend.no_data')}
          </div>
        ) : (
          <ReactEChartsCore
            echarts={echarts}
            option={option}
            notMerge={false}
            lazyUpdate
            style={{ height: '100%', width: '100%' }}
            opts={{ renderer: 'canvas' }}
          />
        )}
      </div>
    </div>
  );
};
