import React, { useCallback, useEffect, useState } from 'react';
import {
  Shield,
  ShieldAlert,
  Gauge,
  Waves,
  Ban,
  ListFilter,
  Layers,
  FileText,
  RefreshCw,
  Save,
  AlertTriangle,
  KeyRound,
  Eraser,
} from 'lucide-react';
import { Domain, SecurityPolicy, SecurityStats } from '../types';
import { Button, Input, Switch } from '../components/GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface SecurityPageProps {
  domain: Domain | null;
}

/** 与 GeistUI Input 视觉一致的多行输入，用于 IP / ASN / 国家码列表。 */
const TextArea: React.FC<{
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  helper?: string;
  rows?: number;
}> = ({ label, value, onChange, placeholder, helper, rows = 3 }) => (
  <div className="w-full flex flex-col gap-1.5">
    <label className="text-xs font-medium text-secondary">{label}</label>
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      rows={rows}
      className="w-full bg-card text-primary text-xs rounded-sm border border-border px-3 py-2 font-mono transition-colors placeholder:text-tertiary focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary resize-y"
    />
    {helper && <span className="text-[11px] text-tertiary leading-relaxed">{helper}</span>}
  </div>
);

/** 单条安全规则的容器：图标 + 标题 + 说明 + 开关 + 参数区。 */
const RuleCard: React.FC<{
  icon: React.ReactNode;
  title: string;
  description: string;
  enabled?: boolean;
  onToggle?: (v: boolean) => void;
  badge?: React.ReactNode;
  children?: React.ReactNode;
  disabled?: boolean;
}> = ({ icon, title, description, enabled, onToggle, badge, children, disabled }) => (
  <div className="geist-card p-5 space-y-4">
    <div className="flex items-start justify-between gap-3">
      <div className="flex items-start gap-3 min-w-0">
        <div className="w-8 h-8 rounded-md bg-bg-subtle border border-border flex items-center justify-center text-secondary flex-shrink-0">
          {icon}
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <h3 className="text-sm font-semibold text-primary">{title}</h3>
            {badge}
          </div>
          <p className="text-[11px] text-tertiary mt-0.5 leading-relaxed">{description}</p>
        </div>
      </div>
      {onToggle && (
        <div className="flex-shrink-0 pt-0.5">
          <Switch checked={!!enabled} onChange={onToggle} disabled={disabled} size="sm" />
        </div>
      )}
    </div>
    {children && <div className="space-y-3">{children}</div>}
  </div>
);

export const SecurityPage: React.FC<SecurityPageProps> = ({ domain }) => {
  const { language, t } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert, confirm } = useDialog();

  const [policy, setPolicy] = useState<SecurityPolicy | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [savedMsg, setSavedMsg] = useState('');

  const domainId = domain?.id;

  const load = useCallback(async () => {
    if (!domainId) return;
    try {
      const sec = await api.getSecurity(domainId);
      setPolicy(sec.policy);
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

  // 原先这里有一个 10 秒轮询用于刷新攻击统计。统计已移到「安全日志」页展示，
  // 本页只做规则配置，无需再周期性拉取，省掉一路无用请求。

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
      setSavedMsg(isZh ? '安全策略已保存并同步到全部节点' : 'Policy saved and synced to all nodes');
      setTimeout(() => setSavedMsg(''), 4000);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message || (isZh ? '保存失败' : 'Save failed') });
    } finally {
      setSaving(false);
    }
  };

  // 「清零统计」与「解除封禁」随统计一并移到「安全日志」页，操作与数据放在同一处。

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
        {isZh ? '正在加载安全策略…' : 'Loading security policy…'}
      </div>
    );
  }

  const ruleDisabled = !policy.enabled;

  return (
    // 不加入场动画：本页规则卡片较多，淡入会让整块内容有「展开」的观感
    <div className="space-y-6">
      {/* 页眉：标题 + 总开关 + 保存 */}
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-base font-bold text-primary tracking-tight flex items-center gap-2">
            <Shield className="w-4 h-4 text-blue-500" />
            {t('nav.security')}
          </h2>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? `针对 ${domain.name} 的权威解析入口执行防护，规则在主控与全部边缘节点同时生效。`
              : `Protection for ${domain.name} enforced at the authoritative entry point on the master and every edge node.`}
          </p>
        </div>
        <div className="flex items-center gap-3 flex-shrink-0">
          <div className="flex items-center gap-2">
            <Switch checked={policy.enabled} onChange={(v) => patch('enabled', v)} size="sm" />
            <span className="text-xs text-primary whitespace-nowrap">
              {isZh ? '启用防护' : 'Protection'}
            </span>
          </div>
          <Button
            size="sm"
            variant="primary"
            loading={saving}
            disabled={!dirty}
            onClick={handleSave}
            icon={<Save className="w-3.5 h-3.5" />}
          >
            {isZh ? '保存策略' : 'Save'}
          </Button>
        </div>
      </div>

      {savedMsg && (
        <div className="p-3 rounded-sm bg-green-500/10 border border-green-500/20 text-xs text-green-500 font-medium">
          {savedMsg}
        </div>
      )}
      {dirty && !savedMsg && (
        <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-500">
          {isZh ? '有未保存的修改，点击「保存策略」后才会下发生效。' : 'Unsaved changes — click Save to apply.'}
        </div>
      )}
      {!policy.enabled && (
        <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-xs text-red-500">
          {isZh
            ? '本域名的安全防护总开关已关闭，下面所有规则都不会执行。'
            : 'Protection is disabled for this zone; none of the rules below are enforced.'}
        </div>
      )}

      {/* 攻击统计与各规则拦截次数集中在「安全日志」页展示，本页只负责规则配置。 */}

      {/* 规则配置：一行一条规则，整行铺开更易读 */}
      <div className="space-y-4">
        {/* 1. 查询速率限制 */}
        <RuleCard
          icon={<Gauge className="w-4 h-4" />}
          title={isZh ? '查询速率限制' : 'Query Rate Limit'}
          description={
            isZh
              ? '按「客户端 IP + 本区域」令牌桶限速。UDP 超限按 SLIP 比例回 TC=1 或丢弃，TCP 返回 REFUSED。'
              : 'Token bucket per client IP per zone. Over-limit UDP is slipped to TC=1 or dropped; TCP gets REFUSED.'
          }
          enabled={policy.rate_limit_enabled}
          onToggle={(v) => patch('rate_limit_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Input
              label={isZh ? '每秒查询上限 (QPS)' : 'Queries per second'}
              type="number"
              value={policy.rate_limit_qps}
              onChange={(e) => patch('rate_limit_qps', Number(e.target.value))}
            />
            <Input
              label={isZh ? '突发桶容量' : 'Burst capacity'}
              type="number"
              value={policy.rate_limit_burst}
              onChange={(e) => patch('rate_limit_burst', Number(e.target.value))}
            />
          </div>
        </RuleCard>

        {/* 2. DNS Flood / DDoS 防护 */}
        <RuleCard
          icon={<Waves className="w-4 h-4" />}
          title={isZh ? 'DNS Flood / DDoS 防护' : 'DNS Flood / DDoS Protection'}
          description={
            isZh
              ? '单 IP 在 1 秒窗口内超过阈值即临时封禁，封禁期内的查询一律静默丢弃，不做任何应答。'
              : 'Bans a client IP that exceeds the threshold within a 1s window; queries are silently dropped while banned.'
          }
          enabled={policy.flood_protection_enabled}
          onToggle={(v) => patch('flood_protection_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Input
              label={isZh ? '触发阈值 (QPS)' : 'Threshold (QPS)'}
              type="number"
              value={policy.flood_threshold_qps}
              onChange={(e) => patch('flood_threshold_qps', Number(e.target.value))}
            />
            <Input
              label={isZh ? '封禁时长 (秒)' : 'Ban duration (s)'}
              type="number"
              value={policy.flood_ban_seconds}
              onChange={(e) => patch('flood_ban_seconds', Number(e.target.value))}
            />
          </div>
        </RuleCard>

        {/* 3. RRL */}
        <RuleCard
          icon={<Shield className="w-4 h-4" />}
          title={isZh ? 'RRL 应答速率限制' : 'Response Rate Limiting (RRL)'}
          description={
            isZh
              ? '限制对同一客户端的应答发送速率，抑制伪造源 IP 的反射放大。仅对 UDP 生效。'
              : 'Caps the response rate to a single client to blunt spoofed-source reflection. UDP only.'
          }
          enabled={policy.rrl_enabled}
          onToggle={(v) => patch('rrl_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Input
              label={isZh ? '每秒应答上限' : 'Responses per second'}
              type="number"
              value={policy.rrl_responses_per_sec}
              onChange={(e) => patch('rrl_responses_per_sec', Number(e.target.value))}
            />
            <Input
              label={isZh ? 'SLIP 比例 (每 N 个回 TC=1)' : 'SLIP ratio (1 in N → TC=1)'}
              type="number"
              value={policy.rrl_slip_ratio}
              onChange={(e) => patch('rrl_slip_ratio', Number(e.target.value))}
            />
          </div>
        </RuleCard>

        {/* 9. 单域名 QPS 限制 */}
        <RuleCard
          icon={<Layers className="w-4 h-4" />}
          title={isZh ? '单域名 QPS 限制' : 'Per-Zone QPS Cap'}
          description={
            isZh
              ? '限制本区域的总查询速率（不分客户端），避免单个域名被打爆时拖垮整台服务器。'
              : 'Caps the aggregate query rate for this zone so one hot zone cannot starve the whole server.'
          }
          enabled={policy.zone_qps_limit_enabled}
          onToggle={(v) => patch('zone_qps_limit_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="sm:max-w-xs">
            <Input
              label={isZh ? '整区 QPS 上限' : 'Zone QPS limit'}
              type="number"
              value={policy.zone_qps_limit}
              onChange={(e) => patch('zone_qps_limit', Number(e.target.value))}
            />
          </div>
        </RuleCard>

        {/* 5. AXFR 限制 */}
        <RuleCard
          icon={<KeyRound className="w-4 h-4" />}
          title={isZh ? 'AXFR 区域传送限制' : 'AXFR Transfer Restriction'}
          description={
            isZh
              ? '区域传送会一次性泄露整个区域的全部记录，默认一律拒绝；如需从服务器同步请改为白名单。'
              : 'A zone transfer leaks every record at once. Denied by default; use the allow list for secondaries.'
          }
        >
          <div className="flex items-center gap-2">
            {(['deny', 'allowlist'] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                onClick={() => patch('axfr_policy', mode)}
                className={`px-3 py-1.5 text-xs rounded-sm border transition-colors cursor-pointer ${
                  policy.axfr_policy === mode
                    ? 'bg-primary text-bg border-transparent font-semibold'
                    : 'bg-card text-secondary border-border hover:text-primary'
                }`}
              >
                {mode === 'deny'
                  ? isZh
                    ? '一律拒绝'
                    : 'Deny all'
                  : isZh
                    ? '仅白名单'
                    : 'Allow list'}
              </button>
            ))}
          </div>
          {policy.axfr_policy === 'allowlist' && (
            <div className="lg:max-w-2xl">
              <TextArea
                label={isZh ? '允许传送的从服务器 IP / CIDR' : 'Allowed secondary IPs / CIDRs'}
                value={policy.axfr_allowed_ips}
                onChange={(v) => patch('axfr_allowed_ips', v)}
                placeholder={'203.0.113.53'}
                rows={2}
              />
            </div>
          )}
        </RuleCard>

        {/* 7. 异常 QTYPE 防护 */}
        <RuleCard
          icon={<ListFilter className="w-4 h-4" />}
          title={isZh ? '异常 QTYPE 防护' : 'Abnormal QTYPE Filter'}
          description={
            isZh
              ? '拒绝已废弃或几乎只用于放大攻击的查询类型，减少被利用的面。'
              : 'Refuses obsolete or amplification-prone query types to shrink the attack surface.'
          }
          enabled={policy.qtype_filter_enabled}
          onToggle={(v) => patch('qtype_filter_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="lg:max-w-2xl">
            <Input
              label={isZh ? '拦截的查询类型' : 'Blocked query types'}
              value={policy.blocked_qtypes}
              onChange={(e) => patch('blocked_qtypes', e.target.value)}
              placeholder="MAILA,MAILB,MD,MF,NULL,SPF,WKS"
              helper={isZh ? '逗号分隔的类型名，也支持直接写数字类型码' : 'Comma separated type names; numeric type codes also work'}
            />
          </div>
        </RuleCard>

        {/* 8. ANY 限制 */}
        <RuleCard
          icon={<Waves className="w-4 h-4" />}
          title={isZh ? 'ANY 查询限制' : 'ANY Query Restriction'}
          description={
            isZh
              ? 'ANY 会把该名下所有 RRset 一次性返回，是经典反射放大手法。推荐 RFC 8482 极简应答。'
              : 'ANY returns every RRset at once — a classic amplification vector. RFC 8482 minimal is recommended.'
          }
        >
          <div className="flex flex-wrap items-center gap-2">
            {(['allow', 'minimal', 'refuse'] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                onClick={() => patch('any_policy', mode)}
                className={`px-3 py-1.5 text-xs rounded-sm border transition-colors cursor-pointer ${
                  policy.any_policy === mode
                    ? 'bg-primary text-bg border-transparent font-semibold'
                    : 'bg-card text-secondary border-border hover:text-primary'
                }`}
              >
                {mode === 'allow'
                  ? isZh
                    ? '正常返回'
                    : 'Allow'
                  : mode === 'minimal'
                    ? isZh
                      ? '极简应答 (RFC 8482)'
                      : 'Minimal (RFC 8482)'
                    : isZh
                      ? '直接拒绝'
                      : 'Refuse'}
              </button>
            ))}
          </div>
        </RuleCard>

        {/* 11. NXDOMAIN / 随机子域攻击防护 */}
        <RuleCard
          icon={<AlertTriangle className="w-4 h-4" />}
          title={isZh ? 'NXDOMAIN / 随机子域防护' : 'NXDOMAIN / Random Subdomain Protection'}
          description={
            isZh
              ? '水刑攻击会用伪随机子域制造海量 NXDOMAIN。单 IP 每分钟 NXDOMAIN 超阈值即临时封禁。'
              : 'Water-torture attacks flood pseudo-random subdomains. Bans a client whose NXDOMAIN rate exceeds the threshold.'
          }
          enabled={policy.nx_protection_enabled}
          onToggle={(v) => patch('nx_protection_enabled', v)}
          disabled={ruleDisabled}
        >
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Input
              label={isZh ? '每分钟 NXDOMAIN 阈值' : 'NXDOMAIN per minute'}
              type="number"
              value={policy.nx_threshold_per_min}
              onChange={(e) => patch('nx_threshold_per_min', Number(e.target.value))}
            />
            <Input
              label={isZh ? '封禁时长 (秒)' : 'Ban duration (s)'}
              type="number"
              value={policy.nx_ban_seconds}
              onChange={(e) => patch('nx_ban_seconds', Number(e.target.value))}
            />
          </div>
        </RuleCard>

        {/* 12. 攻击日志 */}
        <RuleCard
          icon={<FileText className="w-4 h-4" />}
          title={isZh ? '攻击日志与统计' : 'Attack Log & Statistics'}
          description={
            isZh
              ? '记录被安全规则拦截的查询，明细见左侧「安全日志」页面；累计拦截次数持久保存。'
              : 'Records queries blocked by security rules. See the Security Logs page for detail; counters are persisted.'
          }
          enabled={policy.attack_log_enabled}
          onToggle={(v) => patch('attack_log_enabled', v)}
          disabled={ruleDisabled}
        />
      </div>
    </div>
  );
};
