import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  FileText,
  RefreshCw,
  Search,
  X,
  ShieldAlert,
  CheckCircle2,
  Database,
  Save,
  Layers,
  AlertTriangle,
  Ban,
  Eraser,
} from 'lucide-react';
import { Domain, SecurityEvent, SecurityStats } from '../types';
import { Button, Badge, Input } from '../components/GeistUI';
import { Pagination } from '../components/Pagination';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { formatCompact, formatFull } from '../lib/format';
import { useDialog } from '../components/DialogProvider';

interface SecurityLogsPageProps {
  domain: Domain | null;
}

/** 规则标识 -> 展示名。与后端 SecRule* 常量对应。 */
const RULE_LABELS: Record<string, { zh: string; en: string }> = {
  rate_limit: { zh: '查询速率限制', en: 'Rate limit' },
  flood: { zh: 'Flood / DDoS', en: 'Flood / DDoS' },
  rrl: { zh: 'RRL 应答限速', en: 'RRL' },
  blacklist: { zh: 'IP/ASN/地区黑名单', en: 'Blacklist' },
  acl: { zh: 'Resolver ACL', en: 'Resolver ACL' },
  axfr: { zh: 'AXFR 区域传送', en: 'AXFR' },
  qtype: { zh: '异常 QTYPE', en: 'Abnormal QTYPE' },
  any: { zh: 'ANY 限制', en: 'ANY restriction' },
  zone_qps: { zh: '单域名 QPS', en: 'Zone QPS' },
  nx_abuse: { zh: 'NXDOMAIN 滥用', en: 'NXDOMAIN abuse' },
  query: { zh: '普通查询', en: 'Query' },
};

const ruleLabel = (rule: string, isZh: boolean): string => {
  const item = RULE_LABELS[rule];
  if (!item) return rule;
  return isZh ? item.zh : item.en;
};

/** 处置动作的配色：截断属于降级放行用琥珀，其余拦截用红色。 */
const actionVariant = (action: string, blocked: boolean): 'error' | 'warning' | 'default' => {
  if (!blocked) return 'default';
  if (action === 'truncated') return 'warning';
  return 'error';
};

type Outcome = '' | 'blocked' | 'allowed';

export const SecurityLogsPage: React.FC<SecurityLogsPageProps> = ({ domain }) => {
  const { language, t } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert, confirm } = useDialog();

  const [events, setEvents] = useState<SecurityEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [stored, setStored] = useState(0);
  // 攻击统计与当前封禁数。与日志明细同页展示：日志是「发生了什么」，
  // 这两组数字是「累计发生了多少」，放在一起才能对照着看。
  const [secStats, setSecStats] = useState<SecurityStats | null>(null);
  const [activeBans, setActiveBans] = useState(0);
  const [rules, setRules] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);

  // 分页（服务端）
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  // 筛选
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [ruleFilter, setRuleFilter] = useState('');
  const [outcomeFilter, setOutcomeFilter] = useState<Outcome>('');

  // 保留上限
  const [retention, setRetention] = useState(10000);
  const [retentionDraft, setRetentionDraft] = useState('10000');
  const [savingRetention, setSavingRetention] = useState(false);
  const [savedMsg, setSavedMsg] = useState('');
  // 首次拿到后端值后不再被轮询覆盖，避免打字时输入框被重置。
  const retentionTouched = useRef(false);

  const domainId = domain?.id;

  // 搜索输入防抖，避免每敲一个字就打一次接口。
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 350);
    return () => clearTimeout(timer);
  }, [search]);

  // 筛选条件变化时回到第一页。
  useEffect(() => {
    setPage(1);
  }, [debouncedSearch, ruleFilter, outcomeFilter, pageSize]);

  const load = useCallback(
    async (showSpinner = false) => {
      if (!domainId) return;
      if (showSpinner) setLoading(true);
      try {
        const res = await api.getSecurityEvents(domainId, {
          page,
          pageSize,
          rule: ruleFilter,
          outcome: outcomeFilter,
          search: debouncedSearch,
        });
        setEvents(res.events || []);
        setTotal(res.total || 0);
        setStored(res.stored || 0);
        setRules(res.rules || []);
        setRetention(res.retention_limit);
        if (!retentionTouched.current) {
          setRetentionDraft(String(res.retention_limit));
        }
      } catch (err) {
        console.error(err);
      } finally {
        setLoading(false);
      }
    },
    [domainId, page, pageSize, ruleFilter, outcomeFilter, debouncedSearch]
  );

  useEffect(() => {
    load(true);
  }, [load]);

  // 攻击统计走独立接口，与日志分页解耦：翻页、改筛选都不需要重新拉统计。
  const loadStats = useCallback(async () => {
    if (!domainId) return;
    try {
      const sec = await api.getSecurity(domainId);
      setSecStats(sec.stats);
      setActiveBans(sec.active_bans);
    } catch (err) {
      console.error(err);
    }
  }, [domainId]);

  useEffect(() => {
    loadStats();
  }, [loadStats]);

  useEffect(() => {
    if (!domainId || !autoRefresh) return;
    const timer = setInterval(() => {
      load(false);
      loadStats();
    }, 10000);
    return () => clearInterval(timer);
  }, [domainId, autoRefresh, load, loadStats]);

  const handleResetStats = async () => {
    if (!domainId) return;
    const ok = await confirm({
      variant: 'danger',
      message: isZh
        ? '确定要清零该域名的攻击统计与事件记录吗？此操作不可撤销。'
        : 'Reset attack statistics and event log for this zone? This cannot be undone.',
    });
    if (!ok) return;
    try {
      await api.resetSecurityStats(domainId);
      await Promise.all([load(true), loadStats()]);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  const handleClearBans = async () => {
    if (!domainId) return;
    try {
      const res = await api.clearSecurityBans(domainId);
      await loadStats();
      await alert(
        isZh ? `已解除 ${res.cleared} 个客户端的临时封禁` : `Cleared ${res.cleared} client ban(s)`
      );
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  const handleSaveRetention = async () => {
    if (!domainId) return;
    const n = Number(retentionDraft);
    if (!Number.isFinite(n) || n < 100 || n > 200000) {
      await alert({
        variant: 'danger',
        message: isZh ? '保留上限需在 100 ~ 200000 之间。' : 'Retention limit must be between 100 and 200000.',
      });
      return;
    }
    setSavingRetention(true);
    setSavedMsg('');
    try {
      const res = await api.updateSecurity(domainId, { log_retention_limit: Math.floor(n) });
      setRetention(res.policy.log_retention_limit);
      setRetentionDraft(String(res.policy.log_retention_limit));
      retentionTouched.current = false;
      setSavedMsg(
        isZh
          ? `保留上限已设为 ${res.policy.log_retention_limit.toLocaleString()} 条`
          : `Retention limit set to ${res.policy.log_retention_limit.toLocaleString()}`
      );
      setTimeout(() => setSavedMsg(''), 4000);
      await load(false);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message || (isZh ? '保存失败' : 'Save failed') });
    } finally {
      setSavingRetention(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  const hasFilter = !!search.trim() || !!ruleFilter || outcomeFilter !== '';
  const usagePercent = retention > 0 ? Math.min(100, Math.round((stored / retention) * 100)) : 0;
  const retentionDirty = String(retention) !== retentionDraft.trim();

  const clearFilters = () => {
    setSearch('');
    setRuleFilter('');
    setOutcomeFilter('');
  };

  const blockedOnPage = useMemo(() => events.filter((e) => e.blocked).length, [events]);

  // 攻击统计概览与按规则拆分的拦截次数。数据源与安全策略同一个接口，
  // 统计展示集中在本页，安全防护页只负责规则配置。
  const attackOverview = [
    {
      label: isZh ? '累计查询' : 'Total queries',
      value: secStats?.total_queries ?? 0,
      tint: 'text-secondary',
      icon: <Layers className="w-4 h-4" />,
    },
    {
      label: isZh ? '累计拦截' : 'Blocked',
      value: secStats?.blocked_total ?? 0,
      tint: 'text-red-500',
      icon: <ShieldAlert className="w-4 h-4" />,
    },
    {
      label: 'NXDOMAIN',
      value: secStats?.nxdomain_count ?? 0,
      tint: 'text-amber-500',
      icon: <AlertTriangle className="w-4 h-4" />,
    },
    {
      label: isZh ? '当前封禁 IP' : 'Active bans',
      value: activeBans,
      tint: 'text-blue-500',
      icon: <Ban className="w-4 h-4" />,
    },
  ];

  const blockedBreakdown: { label: string; value: number }[] = [
    { label: isZh ? '速率限制' : 'Rate limit', value: secStats?.blocked_rate_limit ?? 0 },
    { label: isZh ? 'Flood/DDoS' : 'Flood/DDoS', value: secStats?.blocked_flood ?? 0 },
    { label: 'RRL', value: secStats?.blocked_rrl ?? 0 },
    { label: isZh ? '黑名单' : 'Blacklist', value: secStats?.blocked_blacklist ?? 0 },
    { label: 'ACL', value: secStats?.blocked_acl ?? 0 },
    { label: 'AXFR', value: secStats?.blocked_axfr ?? 0 },
    { label: isZh ? '异常 QTYPE' : 'Bad QTYPE', value: secStats?.blocked_qtype ?? 0 },
    { label: 'ANY', value: secStats?.blocked_any ?? 0 },
    { label: isZh ? '单域 QPS' : 'Zone QPS', value: secStats?.blocked_zone_qps ?? 0 },
    { label: isZh ? '随机子域' : 'NX abuse', value: secStats?.blocked_nx_abuse ?? 0 },
  ];

  if (!domain) {
    return (
      <div className="geist-card p-8 text-center text-secondary text-xs font-mono">
        {isZh ? '请先在左侧选择一个托管域名。' : 'Select a hosted domain first.'}
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 页眉 */}
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-base font-bold text-primary tracking-tight flex items-center gap-2">
            <FileText className="w-4 h-4 text-blue-500" />
            {t('nav.security_logs')}
          </h2>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? `${domain.name} 的安全事件明细：被各安全规则拦截的查询会实时出现在这里。`
              : `Security event detail for ${domain.name}: queries blocked by security rules appear here in real time.`}
          </p>
        </div>
        <div className="flex items-center gap-3 flex-shrink-0">
          <label className="flex items-center gap-1.5 text-xs text-secondary cursor-pointer select-none">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
              className="accent-blue-500 cursor-pointer"
            />
            {isZh ? '自动刷新' : 'Auto refresh'}
          </label>
          <Button
            size="sm"
            variant="secondary"
            loading={loading}
            onClick={() => load(true)}
            icon={<RefreshCw className="w-3.5 h-3.5" />}
          >
            {t('common.refresh')}
          </Button>
        </div>
      </div>

      {savedMsg && (
        <div className="p-3 rounded-sm bg-green-500/10 border border-green-500/20 text-xs text-green-500 font-medium">
          {savedMsg}
        </div>
      )}

      {/* 攻击统计概览：累计态势，与下方日志明细相互对照 */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {attackOverview.map((o) => (
          <div key={o.label} className="geist-card p-3.5 flex items-center justify-between">
            <div className="flex flex-col gap-0.5 min-w-0">
              <span className="text-[11px] text-tertiary truncate">{o.label}</span>
              <span className="text-lg font-bold tracking-tight text-primary" title={formatFull(o.value)}>
                {formatCompact(o.value)}
              </span>
            </div>
            <span className={o.tint}>{o.icon}</span>
          </div>
        ))}
      </div>

      {/* 各规则拦截次数 */}
      <div className="geist-card p-5 space-y-4">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <ShieldAlert className="w-4 h-4 text-red-500" />
            {isZh ? '各规则拦截次数' : 'Blocks by rule'}
          </h3>
          <div className="flex items-center gap-1.5">
            <Button size="sm" variant="secondary" onClick={handleClearBans} icon={<Eraser className="w-3.5 h-3.5" />}>
              {isZh ? '解除封禁' : 'Clear bans'}
            </Button>
            <Button size="sm" variant="secondary" onClick={handleResetStats} icon={<RefreshCw className="w-3.5 h-3.5" />}>
              {isZh ? '清零统计' : 'Reset'}
            </Button>
          </div>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 font-mono text-xs">
          {blockedBreakdown.map((b) => (
            <div key={b.label} className="p-3 bg-bg-subtle border border-border rounded-sm space-y-1">
              <div className="text-[11px] text-tertiary truncate">{b.label}</div>
              <div
                className={`text-sm font-bold ${b.value > 0 ? 'text-red-500' : 'text-primary'}`}
                title={formatFull(b.value)}
              >
                {formatCompact(b.value)}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* 保留上限 + 用量 */}
      <div className="geist-card p-5 space-y-4">
        <div className="flex flex-col lg:flex-row lg:items-end justify-between gap-4">
          <div className="flex items-start gap-3 min-w-0">
            <div className="w-8 h-8 rounded-md bg-bg-subtle border border-border flex items-center justify-center text-secondary flex-shrink-0">
              <Database className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold text-primary">
                {isZh ? '日志保留上限' : 'Log Retention Limit'}
              </h3>
              <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">
                {isZh
                  ? '按先进先出迭代：写满上限后自动丢弃最旧的一条，始终保留最新的日志。范围 100 ~ 200000。'
                  : 'FIFO rotation: once full, the oldest entry is dropped so the newest are always kept. Range 100–200000.'}
              </p>
            </div>
          </div>

          <div className="flex items-end gap-2 flex-shrink-0">
            <div className="w-40">
              <Input
                label={isZh ? '保留条数' : 'Entries kept'}
                // 高度对齐右侧 size="sm" 的保存按钮（h-8），避免输入框比按钮高一截
                className="h-8 text-xs"
                type="number"
                min={100}
                max={200000}
                value={retentionDraft}
                onChange={(e) => {
                  retentionTouched.current = true;
                  setRetentionDraft(e.target.value);
                }}
              />
            </div>
            <Button
              size="sm"
              variant="primary"
              loading={savingRetention}
              disabled={!retentionDirty}
              onClick={handleSaveRetention}
              icon={<Save className="w-3.5 h-3.5" />}
            >
              {isZh ? '保存' : 'Save'}
            </Button>
          </div>
        </div>

        {/* 用量条 */}
        <div className="space-y-1.5">
          <div className="flex items-center justify-between text-[11px] font-mono">
            <span className="text-tertiary">
              {isZh ? '已用' : 'Used'}:{' '}
              <span className="text-primary font-semibold">{stored.toLocaleString()}</span>
              {' / '}
              {retention.toLocaleString()}
            </span>
            <span className={usagePercent >= 100 ? 'text-amber-500' : 'text-tertiary'}>
              {usagePercent}%
              {usagePercent >= 100 && (isZh ? ' · 已开始滚动覆盖最旧日志' : ' · rotating oldest out')}
            </span>
          </div>
          <div className="w-full h-1.5 bg-bg-subtle rounded-full overflow-hidden">
            <div
              style={{ width: `${Math.max(usagePercent, stored > 0 ? 2 : 0)}%` }}
              className={`h-full rounded-full transition-all ${
                usagePercent >= 100 ? 'bg-amber-500' : 'bg-blue-500'
              }`}
            />
          </div>
        </div>
      </div>

      {/* 概览 */}
      <div className="grid grid-cols-3 gap-3">
        <div className="geist-card p-3.5 flex items-center justify-between">
          <div className="flex flex-col gap-0.5 min-w-0">
            <span className="text-[11px] text-tertiary truncate">
              {isZh ? '已保留事件' : 'Stored events'}
            </span>
            <span className="text-lg font-bold tracking-tight text-primary">
              {stored.toLocaleString()}
            </span>
          </div>
          <span className="text-secondary">
            <FileText className="w-4 h-4" />
          </span>
        </div>
        <div className="geist-card p-3.5 flex items-center justify-between">
          <div className="flex flex-col gap-0.5 min-w-0">
            <span className="text-[11px] text-tertiary truncate">
              {isZh ? '当前筛选命中' : 'Matching filter'}
            </span>
            <span className="text-lg font-bold tracking-tight text-primary">
              {total.toLocaleString()}
            </span>
          </div>
          <span className="text-blue-500">
            <Search className="w-4 h-4" />
          </span>
        </div>
        <div className="geist-card p-3.5 flex items-center justify-between">
          <div className="flex flex-col gap-0.5 min-w-0">
            <span className="text-[11px] text-tertiary truncate">
              {isZh ? '本页拦截条数' : 'Blocked on page'}
            </span>
            <span className={`text-lg font-bold tracking-tight ${blockedOnPage > 0 ? 'text-red-500' : 'text-primary'}`}>
              {blockedOnPage.toLocaleString()}
            </span>
          </div>
          <span className="text-red-500">
            <ShieldAlert className="w-4 h-4" />
          </span>
        </div>
      </div>

      {/* 搜索与筛选 */}
      <div className="geist-card p-4">
        <div className="flex flex-col lg:flex-row items-stretch lg:items-center gap-3">
          <div className="relative flex-1 min-w-0">
            <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-tertiary pointer-events-none" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={
                isZh
                  ? '搜索来源 IP / 查询名 / 类型 / 国家码 / ASN…'
                  : 'Search client IP / query name / type / country / ASN…'
              }
              className="w-full h-9 bg-card text-primary text-xs rounded-sm border border-border pl-9 pr-8 font-mono transition-colors placeholder:text-tertiary focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary"
            />
            {search && (
              <button
                type="button"
                onClick={() => setSearch('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-tertiary hover:text-primary cursor-pointer"
                aria-label={isZh ? '清空搜索' : 'Clear search'}
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>

          <select
            value={ruleFilter}
            onChange={(e) => setRuleFilter(e.target.value)}
            className="h-9 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary cursor-pointer"
            aria-label={isZh ? '按规则筛选' : 'Filter by rule'}
          >
            <option value="">{isZh ? '全部规则' : 'All rules'}</option>
            {rules.map((r) => (
              <option key={r} value={r}>
                {ruleLabel(r, isZh)}
              </option>
            ))}
          </select>

          <div className="flex items-center gap-1 bg-bg-subtle border border-border rounded-sm p-0.5 font-mono self-start lg:self-auto">
            {(
              [
                { key: '' as Outcome, label: isZh ? '全部' : 'All' },
                { key: 'blocked' as Outcome, label: isZh ? '已拦截' : 'Blocked' },
                { key: 'allowed' as Outcome, label: isZh ? '已放行' : 'Allowed' },
              ]
            ).map((opt) => (
              <button
                key={opt.key || 'all'}
                type="button"
                onClick={() => setOutcomeFilter(opt.key)}
                className={`px-2.5 py-1 text-[11px] rounded-sm transition-colors cursor-pointer ${
                  outcomeFilter === opt.key
                    ? 'bg-primary text-bg font-semibold'
                    : 'text-secondary hover:text-primary'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>

          {hasFilter && (
            <Button size="sm" variant="tertiary" onClick={clearFilters} icon={<X className="w-3.5 h-3.5" />}>
              {isZh ? '清除筛选' : 'Clear'}
            </Button>
          )}
        </div>
      </div>

      {/* 事件表格 */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-semibold whitespace-nowrap">{isZh ? '时间' : 'Time'}</th>
                <th className="py-3 px-4 font-semibold">{isZh ? '来源 IP' : 'Client IP'}</th>
                <th className="py-3 px-4 font-semibold">{isZh ? '地区 / ASN' : 'Region / ASN'}</th>
                <th className="py-3 px-4 font-semibold">{isZh ? '查询' : 'Query'}</th>
                <th className="py-3 px-4 font-semibold">{isZh ? '命中规则' : 'Rule'}</th>
                <th className="py-3 px-4 font-semibold">{isZh ? '处置' : 'Action'}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border font-mono">
              {events.map((ev, idx) => (
                <tr key={`${ev.at}-${idx}`} className="hover:bg-bg-subtle/50 transition-colors">
                  <td className="py-2.5 px-4 text-secondary whitespace-nowrap">
                    {new Date(ev.at).toLocaleString()}
                  </td>
                  <td className="py-2.5 px-4 text-primary whitespace-nowrap">{ev.client_ip || '—'}</td>
                  <td className="py-2.5 px-4 text-secondary whitespace-nowrap">
                    {(ev.country || '—').toUpperCase()}
                    {ev.asn ? ` / AS${ev.asn}` : ''}
                  </td>
                  <td className="py-2.5 px-4 text-secondary">
                    <span className="break-all">{ev.qname}</span>{' '}
                    <span className="text-tertiary">{ev.qtype}</span>
                  </td>
                  <td className="py-2.5 px-4 whitespace-nowrap">
                    <Badge variant={ev.blocked ? 'error' : 'default'} size="sm">
                      {ruleLabel(ev.rule, isZh)}
                    </Badge>
                  </td>
                  <td className="py-2.5 px-4 whitespace-nowrap">
                    <Badge variant={actionVariant(ev.action, ev.blocked)} size="sm">
                      {ev.action}
                    </Badge>
                  </td>
                </tr>
              ))}

              {events.length === 0 && !loading && (
                <tr>
                  <td colSpan={6} className="py-12 text-center">
                    <div className="flex flex-col items-center justify-center gap-3">
                      <div className="w-11 h-11 rounded-lg bg-bg-subtle border border-border flex items-center justify-center text-tertiary">
                        {hasFilter ? <Search className="w-5 h-5" /> : <CheckCircle2 className="w-5 h-5" />}
                      </div>
                      <div className="text-sm text-primary font-medium font-sans">
                        {hasFilter
                          ? isZh
                            ? '没有符合条件的事件'
                            : 'No events match the filter'
                          : isZh
                            ? '暂无安全事件'
                            : 'No security events yet'}
                      </div>
                      <div className="text-xs text-tertiary max-w-md font-sans">
                        {hasFilter
                          ? isZh
                            ? '试着放宽搜索关键词或切换规则筛选。'
                            : 'Try a broader keyword or a different rule filter.'
                          : isZh
                            ? '被安全规则拦截的查询会实时出现在这里。日志明细保存在内存中，按上面的保留上限先进先出滚动，服务重启（含升级部署）后会清空；上方的累计统计存在数据库里，重启不丢。'
                            : 'Blocked queries appear here in real time. Event detail lives in memory with FIFO rotation at the limit above and is cleared on restart (including upgrades); the cumulative counters above are persisted in the database.'}
                      </div>
                      {hasFilter && (
                        <Button size="sm" variant="secondary" onClick={clearFilters}>
                          {isZh ? '清除筛选' : 'Clear filters'}
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <Pagination
          page={page}
          pageSize={pageSize}
          total={total}
          totalPages={totalPages}
          from={from}
          to={to}
          onPageChange={setPage}
          onPageSizeChange={(n) => setPageSize(n)}
          pageSizeOptions={[20, 50, 100, 200]}
        />
      </div>
    </div>
  );
};
