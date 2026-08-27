import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Ban,
  ShieldCheck,
  Network,
  Plus,
  Trash2,
  Save,
  Search,
  AlertTriangle,
  Globe,
} from 'lucide-react';
import { Domain, SecurityPolicy } from '../types';
import { Button, Switch, Badge } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { CountryMultiSelect } from '../components/RoutingControls';
import { getRegionOption } from '../components/FlagRegionSelect';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface AccessControlPageProps {
  domain: Domain | null;
}

/** 后端把这些列表存成逗号 / 换行分隔的字符串，前端统一按数组编辑。 */
const parseList = (raw: string): string[] =>
  (raw || '')
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean);

const joinList = (items: string[]): string => items.join(',');

/** 校验 IP 或 CIDR（IPv4 / IPv6）。前端先挡一道，避免写入无效条目被后端静默忽略。 */
const isValidIPOrCIDR = (value: string): boolean => {
  const [addr, prefix] = value.split('/');
  if (prefix !== undefined) {
    const n = Number(prefix);
    if (!Number.isInteger(n) || n < 0) return false;
    const max = addr.includes(':') ? 128 : 32;
    if (n > max) return false;
  }
  if (addr.includes(':')) {
    // IPv6：宽松校验，只允许十六进制段与压缩写法
    return /^[0-9a-fA-F:]+$/.test(addr) && (addr.match(/::/g) || []).length <= 1;
  }
  const parts = addr.split('.');
  if (parts.length !== 4) return false;
  return parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) >= 0 && Number(p) <= 255);
};

/** 校验 ASN，允许带 AS 前缀。 */
const normalizeASN = (value: string): string | null => {
  const v = value.trim().toUpperCase().replace(/^AS/, '');
  if (!/^\d{1,10}$/.test(v)) return null;
  const n = Number(v);
  if (n <= 0 || n > 4294967295) return null;
  return String(n);
};

type ListKind = 'ip' | 'asn';

/** 可增删、可搜索、可分页的条目表格。 */
const EntryTable: React.FC<{
  title: string;
  description: string;
  icon: React.ReactNode;
  kind: ListKind;
  entries: string[];
  onChange: (next: string[]) => void;
  placeholder: string;
  accent?: 'danger' | 'safe' | 'info';
  badge?: React.ReactNode;
}> = ({ title, description, icon, kind, entries, onChange, placeholder, accent = 'info', badge }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const [draft, setDraft] = useState('');
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');

  const filtered = useMemo(() => {
    const kw = search.trim().toLowerCase();
    if (!kw) return entries;
    return entries.filter((e) => e.toLowerCase().includes(kw));
  }, [entries, search]);

  const pager = usePagination(filtered, 10);

  const add = () => {
    setError('');
    // 支持一次粘贴多条（换行 / 逗号 / 空格分隔），批量导入更省事
    const candidates = parseList(draft);
    if (candidates.length === 0) return;

    const accepted: string[] = [];
    const rejected: string[] = [];
    for (const raw of candidates) {
      if (kind === 'ip') {
        if (isValidIPOrCIDR(raw)) accepted.push(raw);
        else rejected.push(raw);
      } else {
        const asn = normalizeASN(raw);
        if (asn) accepted.push(asn);
        else rejected.push(raw);
      }
    }

    if (rejected.length > 0) {
      setError(
        (isZh ? '格式无效，已忽略：' : 'Invalid, ignored: ') + rejected.slice(0, 5).join(', ')
      );
    }
    if (accepted.length === 0) return;

    // 去重后合并
    const merged = Array.from(new Set([...entries, ...accepted]));
    onChange(merged);
    setDraft('');
  };

  const remove = (value: string) => {
    onChange(entries.filter((e) => e !== value));
  };

  const accentClass =
    accent === 'danger' ? 'text-red-500' : accent === 'safe' ? 'text-green-500' : 'text-blue-500';

  return (
    <div className="geist-card overflow-hidden">
      <div className="p-5 space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-start gap-3 min-w-0">
            <div className={`w-8 h-8 rounded-md bg-bg-subtle border border-border flex items-center justify-center flex-shrink-0 ${accentClass}`}>
              {icon}
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <h3 className="text-sm font-semibold text-primary">{title}</h3>
                <Badge variant="default" size="sm">
                  {entries.length}
                </Badge>
                {badge}
              </div>
              <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">{description}</p>
            </div>
          </div>
        </div>

        {/* 添加 + 搜索 */}
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex-1 flex gap-2 min-w-0">
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  add();
                }
              }}
              placeholder={placeholder}
              className="flex-1 min-w-0 h-9 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono transition-colors placeholder:text-tertiary focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary"
            />
            <Button size="sm" variant="secondary" onClick={add} icon={<Plus className="w-3.5 h-3.5" />}>
              {isZh ? '添加' : 'Add'}
            </Button>
          </div>
          {entries.length > 5 && (
            <div className="relative sm:w-56">
              <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-tertiary pointer-events-none" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={isZh ? '搜索条目…' : 'Search entries…'}
                className="w-full h-9 bg-card text-primary text-xs rounded-sm border border-border pl-9 pr-3 font-mono focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary"
              />
            </div>
          )}
        </div>

        {error && (
          <div className="text-[11px] text-amber-500 flex items-start gap-1.5">
            <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-px" />
            <span className="break-all">{error}</span>
          </div>
        )}
      </div>

      {/* 条目表格 */}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="bg-bg-subtle border-y border-border text-secondary select-none">
            <tr>
              <th className="py-2.5 px-4 font-semibold w-12">#</th>
              <th className="py-2.5 px-4 font-semibold">
                {kind === 'ip' ? (isZh ? 'IP / CIDR' : 'IP / CIDR') : isZh ? 'ASN' : 'ASN'}
              </th>
              <th className="py-2.5 px-4 font-semibold text-right">{isZh ? '操作' : 'Action'}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border font-mono">
            {pager.pageItems.map((entry, idx) => (
              <tr key={entry} className="hover:bg-bg-subtle/50 transition-colors">
                <td className="py-2.5 px-4 text-tertiary">
                  {(pager.page - 1) * pager.pageSize + idx + 1}
                </td>
                <td className="py-2.5 px-4 text-primary break-all">
                  {kind === 'asn' ? `AS${entry}` : entry}
                </td>
                <td className="py-2.5 px-4 text-right">
                  <button
                    onClick={() => remove(entry)}
                    className="p-1.5 rounded-sm border border-border text-secondary hover:bg-red-500/10 hover:text-red-500 hover:border-red-500/20 transition-colors cursor-pointer"
                    title={isZh ? '移除' : 'Remove'}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={3} className="py-8 text-center text-tertiary">
                  {entries.length === 0
                    ? isZh
                      ? '暂无条目'
                      : 'No entries'
                    : isZh
                      ? '没有匹配的条目'
                      : 'No matching entries'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pagination
        page={pager.page}
        pageSize={pager.pageSize}
        total={pager.total}
        totalPages={pager.totalPages}
        from={pager.from}
        to={pager.to}
        onPageChange={pager.setPage}
        onPageSizeChange={pager.setPageSize}
        pageSizeOptions={[10, 20, 50]}
      />
    </div>
  );
};

export const AccessControlPage: React.FC<AccessControlPageProps> = ({ domain }) => {
  const { language, t } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert } = useDialog();

  const [policy, setPolicy] = useState<SecurityPolicy | null>(null);
  const [blockedHits, setBlockedHits] = useState(0);
  const [aclHits, setAclHits] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [savedMsg, setSavedMsg] = useState('');

  const domainId = domain?.id;

  const load = useCallback(async () => {
    if (!domainId) return;
    try {
      const res = await api.getSecurity(domainId);
      setPolicy(res.policy);
      setBlockedHits(res.stats.blocked_blacklist);
      setAclHits(res.stats.blocked_acl);
      setDirty(false);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, [domainId]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  const patch = <K extends keyof SecurityPolicy>(key: K, value: SecurityPolicy[K]) => {
    setPolicy((prev) => (prev ? { ...prev, [key]: value } : prev));
    setDirty(true);
  };

  const handleSave = async () => {
    if (!domainId || !policy) return;
    setSaving(true);
    setSavedMsg('');
    try {
      const res = await api.updateSecurity(domainId, policy);
      setPolicy(res.policy);
      setDirty(false);
      setSavedMsg(isZh ? '访问控制已保存并同步到全部节点' : 'Access control saved and synced to all nodes');
      setTimeout(() => setSavedMsg(''), 4000);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message || (isZh ? '保存失败' : 'Save failed') });
    } finally {
      setSaving(false);
    }
  };

  if (!domain) {
    return (
      <div className="geist-card p-8 text-center text-secondary text-xs font-mono">
        {isZh ? '请先在左侧选择一个托管域名。' : 'Select a hosted domain first.'}
      </div>
    );
  }

  if (loading || !policy) {
    return (
      <div className="geist-card p-8 text-center text-secondary text-xs font-mono">
        {isZh ? '正在加载访问控制…' : 'Loading access control…'}
      </div>
    );
  }

  const blockedCountries = parseList(policy.blocked_countries);

  return (
    // 本页刻意不加 animate-in 入场动画：规则卡片较多，淡入会让整块内容有「展开」的观感
    <div className="space-y-6">
      {/* 页眉 */}
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-base font-bold text-primary tracking-tight flex items-center gap-2">
            <Ban className="w-4 h-4 text-red-500" />
            {t('nav.access_control')}
          </h2>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? `${domain.name} 的来源准入控制。黑名单是整条解析链路里优先级最高的规则，命中即拒绝，早于智能分线路由。`
              : `Source admission control for ${domain.name}. The blacklist is the highest-priority rule in the pipeline — a hit is refused before smart routing.`}
          </p>
        </div>
        <Button
          size="sm"
          variant="primary"
          loading={saving}
          disabled={!dirty}
          onClick={handleSave}
          icon={<Save className="w-3.5 h-3.5" />}
        >
          {isZh ? '保存' : 'Save'}
        </Button>
      </div>

      {savedMsg && (
        <div className="p-3 rounded-sm bg-green-500/10 border border-green-500/20 text-xs text-green-500 font-medium">
          {savedMsg}
        </div>
      )}
      {dirty && !savedMsg && (
        <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-500">
          {isZh ? '有未保存的修改，点击「保存」后才会下发生效。' : 'Unsaved changes — click Save to apply.'}
        </div>
      )}

      {/* 优先级说明 + 命中统计 */}
      <div className="geist-card p-5 space-y-4">
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div className="flex items-start gap-3 min-w-0">
            <div className="w-8 h-8 rounded-md bg-red-500/10 border border-red-500/20 flex items-center justify-center text-red-500 flex-shrink-0">
              <Ban className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <h3 className="text-sm font-semibold text-primary">
                  {isZh ? '启用黑名单' : 'Enable blacklist'}
                </h3>
                <Badge variant="error" size="sm">{isZh ? '最高优先级' : 'Top priority'}</Badge>
              </div>
              <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">
                {isZh
                  ? '命中 IP / ASN / 地区任一条黑名单即返回 REFUSED，不进入智能分线选路，也不返回任何记录。'
                  : 'Any IP / ASN / region blacklist hit returns REFUSED without entering routing or producing records.'}
              </p>
            </div>
          </div>
          <Switch
            checked={policy.blacklist_enabled}
            onChange={(v) => patch('blacklist_enabled', v)}
            size="sm"
          />
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 font-mono text-xs">
          <div className="p-3 bg-bg-subtle border border-border rounded-sm space-y-1">
            <div className="text-[11px] text-tertiary">{isZh ? '黑名单累计拦截' : 'Blacklist blocks'}</div>
            <div className={`text-sm font-bold ${blockedHits > 0 ? 'text-red-500' : 'text-primary'}`}>
              {blockedHits.toLocaleString()}
            </div>
          </div>
          <div className="p-3 bg-bg-subtle border border-border rounded-sm space-y-1">
            <div className="text-[11px] text-tertiary">{isZh ? 'ACL 累计拦截' : 'ACL blocks'}</div>
            <div className={`text-sm font-bold ${aclHits > 0 ? 'text-red-500' : 'text-primary'}`}>
              {aclHits.toLocaleString()}
            </div>
          </div>
          <div className="p-3 bg-bg-subtle border border-border rounded-sm space-y-1">
            <div className="text-[11px] text-tertiary">{isZh ? '黑名单条目' : 'Blocked entries'}</div>
            <div className="text-sm font-bold text-primary">
              {parseList(policy.blocked_ips).length +
                parseList(policy.blocked_asns).length +
                blockedCountries.length}
            </div>
          </div>
          <div className="p-3 bg-bg-subtle border border-border rounded-sm space-y-1">
            <div className="text-[11px] text-tertiary">{isZh ? '白名单条目' : 'Allowed entries'}</div>
            <div className="text-sm font-bold text-primary">
              {parseList(policy.allowed_ips).length}
            </div>
          </div>
        </div>

        {!policy.blacklist_enabled && (
          <div className="p-3 rounded-sm bg-bg-subtle border border-border text-[11px] text-tertiary">
            {isZh
              ? '黑名单当前未启用，下面的 IP / ASN / 地区条目不会生效（白名单与 Resolver ACL 不受此开关影响）。'
              : 'The blacklist is disabled, so the IP / ASN / region entries below are not enforced. The allow list and resolver ACL are unaffected.'}
          </div>
        )}
      </div>

      {/* 封禁 IP / CIDR */}
      <EntryTable
        title={isZh ? '封禁 IP / CIDR' : 'Blocked IPs / CIDRs'}
        description={
          isZh
            ? '支持单个 IP 与 CIDR 网段（IPv4 / IPv6）。可一次粘贴多条，用换行、逗号或空格分隔。'
            : 'Single IPs and CIDR ranges (IPv4 / IPv6). Paste many at once separated by newline, comma or space.'
        }
        icon={<Ban className="w-4 h-4" />}
        kind="ip"
        accent="danger"
        entries={parseList(policy.blocked_ips)}
        onChange={(next) => patch('blocked_ips', joinList(next))}
        placeholder="203.0.113.7 或 198.51.100.0/24"
      />

      {/* 封禁 ASN */}
      <EntryTable
        title={isZh ? '封禁 ASN（运营商 / 自治域）' : 'Blocked ASNs'}
        description={
          isZh
            ? '按自治域号封禁整个运营商或云厂商网络。可带或不带 AS 前缀，例如 4134 或 AS4134。'
            : 'Block an entire carrier or cloud network by autonomous system number. With or without the AS prefix.'
        }
        icon={<Network className="w-4 h-4" />}
        kind="asn"
        accent="danger"
        entries={parseList(policy.blocked_asns)}
        onChange={(next) => patch('blocked_asns', joinList(next))}
        placeholder="4134 或 AS4837"
      />

      {/* 封禁国家 / 地区 */}
      <div className="geist-card p-5 space-y-4">
        <div className="flex items-start gap-3 min-w-0">
          <div className="w-8 h-8 rounded-md bg-bg-subtle border border-border flex items-center justify-center text-red-500 flex-shrink-0">
            <Globe className="w-4 h-4" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h3 className="text-sm font-semibold text-primary">
                {isZh ? '封禁国家 / 地区' : 'Blocked Countries / Regions'}
              </h3>
              <Badge variant="default" size="sm">
                {blockedCountries.length}
              </Badge>
            </div>
            <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">
              {isZh
                ? '按 GeoIP 定位的国家/地区封禁；也支持填大洲码（AS/EU/NA/SA/AF/OC/AN）一次封整个大洲。'
                : 'Block by GeoIP country/region; continent codes (AS/EU/NA/SA/AF/OC/AN) block a whole continent.'}
            </p>
          </div>
        </div>

        <CountryMultiSelect
          value={policy.blocked_countries}
          onChange={(v) => patch('blocked_countries', v)}
        />

        {blockedCountries.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {blockedCountries.map((code) => {
              const opt = getRegionOption(code);
              return (
                <span
                  key={code}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-red-500/10 border border-red-500/20 text-[11px] text-red-500"
                >
                  <span>{opt.flag}</span>
                  <span>{isZh ? opt.nameZh.split(' ')[0] : opt.nameEn}</span>
                </span>
              );
            })}
          </div>
        )}
      </div>

      {/* 放行白名单 */}
      <EntryTable
        title={isZh ? '放行白名单（跳过全部限制）' : 'Allow List (bypasses every rule)'}
        description={
          isZh
            ? '命中白名单的来源会跳过黑名单与所有限速规则，是管理员的逃生通道。请只放自己的监控与运维出口。'
            : 'Whitelisted sources bypass the blacklist and every rate limit — an admin escape hatch. Keep it to your own monitoring and ops egress.'
        }
        icon={<ShieldCheck className="w-4 h-4" />}
        kind="ip"
        accent="safe"
        entries={parseList(policy.allowed_ips)}
        onChange={(next) => patch('allowed_ips', joinList(next))}
        placeholder="10.0.0.0/8 或 192.0.2.1"
        badge={<Badge variant="success" size="sm">{isZh ? '优先于黑名单' : 'Beats blacklist'}</Badge>}
      />

      {/* Resolver ACL */}
      <div className="geist-card p-5 space-y-4">
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div className="flex items-start gap-3 min-w-0">
            <div className="w-8 h-8 rounded-md bg-bg-subtle border border-border flex items-center justify-center text-blue-500 flex-shrink-0">
              <Network className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <h3 className="text-sm font-semibold text-primary">
                  {isZh ? 'Resolver / IP ACL' : 'Resolver / IP ACL'}
                </h3>
                {policy.acl_enabled && (
                  <Badge variant="warning" size="sm">{isZh ? '仅白名单可查' : 'Restricted'}</Badge>
                )}
              </div>
              <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">
                {isZh
                  ? '开启后只有下表内的解析器地址可以查询本区域，其余一律 REFUSED，适合内网私有区域。'
                  : 'When on, only the resolvers below may query this zone; everything else is REFUSED. Good for private zones.'}
              </p>
            </div>
          </div>
          <Switch checked={policy.acl_enabled} onChange={(v) => patch('acl_enabled', v)} size="sm" />
        </div>

        {policy.acl_enabled && parseList(policy.acl_allowed_ips).length === 0 && (
          <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-[11px] text-red-500 flex items-start gap-1.5">
            <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-px" />
            <span>
              {isZh
                ? 'ACL 已开启但白名单为空，这会拒绝所有查询。保存时服务端会拒绝这种配置，请先添加地址或关闭 ACL。'
                : 'ACL is on but the list is empty, which would block every query. The server rejects this on save — add addresses or turn ACL off.'}
            </span>
          </div>
        )}
      </div>

      <EntryTable
        title={isZh ? 'ACL 允许查询的解析器' : 'ACL Allowed Resolvers'}
        description={
          isZh
            ? '仅在上方 Resolver ACL 开启时生效。填写允许查询本区域的解析器 IP 或网段。'
            : 'Only enforced when Resolver ACL is enabled above. List resolver IPs or ranges allowed to query this zone.'
        }
        icon={<Network className="w-4 h-4" />}
        kind="ip"
        accent="info"
        entries={parseList(policy.acl_allowed_ips)}
        onChange={(next) => patch('acl_allowed_ips', joinList(next))}
        placeholder="192.0.2.53 或 10.0.0.0/8"
      />
    </div>
  );
};
