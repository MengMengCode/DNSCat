import React, { useState } from 'react';
import { CheckCircle2, ArrowRight, Zap, XCircle, ShieldAlert } from 'lucide-react';
import { Domain, TestRoutingResult, RoutingCandidateReason } from '../types';
import { Button, Input, Badge } from '../components/GeistUI';
import { FlagRegionBadge, getRegionOption } from '../components/FlagRegionSelect';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';

interface RoutingPageProps {
  domain: Domain;
}

const REASON_I18N_KEY: Record<RoutingCandidateReason, string> = {
  selected: 'routing.reason_selected',
  selected_weighted: 'routing.reason_selected_weighted',
  selected_failover: 'routing.reason_selected_failover',
  line_not_matched: 'routing.reason_line_not_matched',
  origin_down_no_fallback: 'routing.reason_origin_down_no_fallback',
};

const SELECTION_MODE_I18N_KEY: Record<string, string> = {
  weighted: 'routing.mode_weighted',
  all: 'routing.mode_all',
  none: 'routing.mode_none',
};

export const RoutingPage: React.FC<RoutingPageProps> = ({ domain }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const [testIP, setTestIP] = useState('');
  const [queryName, setQueryName] = useState('@');
  const [queryType, setQueryType] = useState('A');
  const [simulating, setSimulating] = useState(false);
  const [result, setResult] = useState<TestRoutingResult | null>(null);
  const [errorMsg, setErrorMsg] = useState('');

  // 基于国家代码动态生成预设测试 IP，标签随语言切换（中/英文名称）。
  // 说明：这里必须用真实可归属的公网地址（多为各地公共解析器），因为模拟器要对该 IP
  // 做 GeoIP/ASN 查询；RFC 5737 文档保留段在 GeoIP 库中无国家归属，会让每个预设都无匹配。
  const PRESET_IPS: { code: string; ip: string }[] = [
    { code: 'us', ip: '8.8.8.8' },
    { code: 'cn', ip: '114.114.114.114' },
    { code: 'hk', ip: '203.80.96.10' },
    { code: 'jp', ip: '210.140.10.1' },
    { code: 'sg', ip: '165.225.0.1' },
    { code: 'de', ip: '193.0.0.1' },
    { code: 'gb', ip: '194.0.0.1' },
    { code: 'au', ip: '139.130.4.5' },
    { code: 'fr', ip: '195.154.120.1' },
    { code: 'br', ip: '200.221.11.100' },
  ];

  const presetRegions = PRESET_IPS.map((preset) => {
    const opt = getRegionOption(preset.code);
    const name = isZh ? opt.nameZh.split(' ')[0] : opt.nameEn;
    return {
      ip: preset.ip,
      label: `${opt.flag} ${name} (${preset.ip})`,
    };
  });

  const handleTestRouting = async (ipToTest: string) => {
    try {
      setSimulating(true);
      setErrorMsg('');
      const res = await api.testRouting(domain.id, {
        client_ip: ipToTest.trim(),
        query_name: queryName.trim(),
        query_type: queryType,
      });
      setResult(res);
    } catch (err: any) {
      setErrorMsg(err.message || (isZh ? '路由测试失败' : 'Routing test failed'));
    } finally {
      setSimulating(false);
    }
  };

  const matchedOpt = result ? getRegionOption(result.matched_line) : null;

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* Simulator Card */}
      <div className="geist-card p-6 space-y-6">
        <div>
          <h3 className="text-sm font-semibold text-primary mb-1 flex items-center gap-2">
            <Zap className="w-4 h-4 text-amber-500" />
            {isZh ? '实时客户端 IP 与 GeoDNS 解析检测器' : 'Live Client IP & GeoDNS Resolver Inspector'}
          </h3>
          <p className="text-xs text-secondary">
            {isZh
              ? '实时测试 DnsCat 如何解析来自任意公网客户端 IP / EDNS 客户端子网 (ECS) 的查询请求。'
              : 'Test how DnsCat resolves queries from any public client IP / EDNS Client Subnet (ECS) in real time.'}
          </p>
        </div>

        {errorMsg && (
          <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-xs text-red-500 font-medium">
            {errorMsg}
          </div>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div className="sm:col-span-2">
            <Input
              label={t('routing.sim_label')}
              value={testIP}
              onChange={(e) => setTestIP(e.target.value)}
              placeholder={isZh ? '填入真实公网 IP，如 8.8.8.8' : 'A real public IP, e.g. 8.8.8.8'}
            />
          </div>

          <div>
            <label className="text-xs font-medium text-secondary mb-1.5 block">
              {isZh ? '记录类型' : 'Record Type'}
            </label>
            <select
              value={queryType}
              onChange={(e) => setQueryType(e.target.value)}
              className="w-full h-10 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary"
            >
              <option value="A">A (IPv4)</option>
              <option value="AAAA">AAAA (IPv6)</option>
              <option value="CNAME">CNAME</option>
              <option value="TXT">TXT</option>
              <option value="ANY">ANY</option>
            </select>
          </div>
        </div>

        <div className="flex justify-end">
          <Button
            size="sm"
            variant="primary"
            onClick={() => handleTestRouting(testIP)}
            loading={simulating}
            icon={<ArrowRight className="w-3.5 h-3.5" />}
          >
            {t('routing.sim_button')}
          </Button>
        </div>

        {/* Quick Carrier / Country Presets */}
        <div>
          <label className="text-xs font-medium text-secondary mb-2 block">
            {isZh ? '快速国家 / 地区测试 IP 预设' : 'Quick Country & Regional IP Presets'}
          </label>
          <div className="flex flex-wrap gap-2">
            {presetRegions.map((preset) => (
              <button
                key={preset.ip}
                type="button"
                onClick={() => {
                  setTestIP(preset.ip);
                  handleTestRouting(preset.ip);
                }}
                className="px-2.5 py-1 rounded bg-bg-subtle border border-border hover:border-primary text-xs font-mono text-secondary hover:text-primary transition-all cursor-pointer"
              >
                {preset.label}
              </button>
            ))}
          </div>
        </div>

        {/* Real Backend Decision Results */}
        {result && (
          <div className="p-5 bg-bg-subtle border border-border rounded-sm space-y-4 animate-in fade-in duration-200">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-xs font-bold text-green-500">
                <CheckCircle2 className="w-4 h-4" />
                <span>{isZh ? '已由 DnsCat Geo 引擎计算完成' : 'Decision Computed via DnsCat Geo Engine'}</span>
              </div>
              <span className="text-xs text-tertiary font-mono">
                {isZh
                  ? `${result.total_matched} 条入选 / 共 ${result.total_candidates ?? result.total_matched} 条候选`
                  : `${result.total_matched} selected / ${result.total_candidates ?? result.total_matched} candidate(s)`}
              </span>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs font-mono">
              <div className="p-3 bg-card border border-border rounded-sm">
                <div className="text-[10px] text-tertiary">{t('routing.matched_line')}</div>
                <div className="text-primary font-bold mt-1 text-sm flex items-center gap-1.5 truncate">
                  <span className="text-base">{matchedOpt?.flag}</span>
                  <span className="truncate">{language === 'zh-CN' ? matchedOpt?.nameZh : matchedOpt?.nameEn}</span>
                </div>
                {/* 客户端被判定的线路与最终生效分线不一致时，说明已回退到默认线路 */}
                {result.client_line && result.client_line !== result.matched_line && (
                  <div className="text-[10px] text-tertiary mt-1 truncate">
                    {t('routing.client_line')}:{' '}
                    <span className="text-secondary font-bold">
                      {getRegionOption(result.client_line).flag}{' '}
                      {isZh
                        ? getRegionOption(result.client_line).nameZh
                        : getRegionOption(result.client_line).nameEn}
                    </span>
                  </div>
                )}
              </div>

              <div className="p-3 bg-card border border-border rounded-sm">
                <div className="text-[10px] text-tertiary">{isZh ? '客户端 IP 与 ISP 运营商' : 'Client IP & ISP Carrier'}</div>
                <div className="text-primary font-bold mt-1 text-sm font-mono truncate">
                  {result.client_ip}
                </div>
                {result.isp && (
                  <div className="text-[11px] text-tertiary mt-0.5 truncate">
                    {result.isp}
                  </div>
                )}
              </div>

              <div className="p-3 bg-card border border-border rounded-sm">
                <div className="text-[10px] text-tertiary">{isZh ? 'IP 判定引擎 / 离线库' : 'IP Lookup Engine / Database'}</div>
                <div className="text-cyan-400 font-bold mt-1 text-xs flex items-center gap-1">
                  <span>💾</span>
                  <span>
                    {result.source === 'mmdb'
                      ? (isZh ? 'MaxMind MMDB 离线库' : 'MaxMind MMDB Offline DB')
                      : (isZh ? '内置高精度离线 IP 库' : 'Built-in High-Precision Offline DB')}
                  </span>
                </div>
                <div className="text-[10px] text-tertiary mt-0.5">
                  {isZh ? '100% 离线高并发解析' : '100% Offline, High-Concurrency Lookup'}
                </div>
              </div>
            </div>

            {/* Routing Decision Trace: mirrors the authoritative engine */}
            <div>
              <div className="flex items-baseline justify-between gap-3 mb-1.5 flex-wrap">
                <div className="text-xs font-medium text-secondary">
                  {t('routing.decision_title')}
                </div>
                {result.selection_mode && (
                  <span className="text-[10px] font-mono text-tertiary">
                    {t('routing.selection_mode')}:{' '}
                    <span className="text-primary font-bold">
                      {t(SELECTION_MODE_I18N_KEY[result.selection_mode] || 'routing.mode_all')}
                    </span>
                  </span>
                )}
              </div>
              <p className="text-[11px] text-tertiary mb-2">{t('routing.decision_desc')}</p>

              <div className="bg-card border border-border rounded-sm overflow-hidden">
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs font-mono">
                    <thead className="bg-bg-subtle border-b border-border text-secondary">
                      <tr>
                        <th className="py-2.5 px-3">{t('routing.col_result')}</th>
                        <th className="py-2.5 px-3">{isZh ? '类型' : 'Type'}</th>
                        <th className="py-2.5 px-3">{isZh ? '主机' : 'Host'}</th>
                        <th className="py-2.5 px-3">{t('routing.col_effective')}</th>
                        <th className="py-2.5 px-3">{isZh ? '分线策略' : 'Routing Policy'}</th>
                        <th className="py-2.5 px-3">{t('routing.col_share')}</th>
                        <th className="py-2.5 px-3">TTL</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                      {(result.candidates || []).map((cand) => (
                        <tr
                          key={cand.record.id}
                          className={cand.selected ? 'bg-green-500/5' : 'opacity-60'}
                        >
                          <td className="py-2.5 px-3">
                            <div className="flex items-center gap-1.5">
                              {cand.selected ? (
                                <CheckCircle2 className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
                              ) : (
                                <XCircle className="w-3.5 h-3.5 text-tertiary flex-shrink-0" />
                              )}
                              <span
                                className={
                                  cand.selected
                                    ? 'text-green-500 font-bold'
                                    : 'text-tertiary'
                                }
                              >
                                {cand.selected ? t('routing.selected') : t('routing.excluded')}
                              </span>
                            </div>
                            <div className="text-[10px] text-tertiary mt-0.5 whitespace-normal">
                              {t(REASON_I18N_KEY[cand.reason] || 'routing.reason_selected')}
                            </div>
                          </td>
                          <td className="py-2.5 px-3 font-bold">
                            <Badge variant="default" size="sm">
                              {cand.record.type}
                            </Badge>
                          </td>
                          <td className="py-2.5 px-3 text-primary font-semibold">
                            {cand.record.name === '@' ? domain.name : `${cand.record.name}.${domain.name}`}
                          </td>
                          <td className="py-2.5 px-3">
                            <div className="flex items-center gap-1.5">
                              {cand.failover_active && (
                                <span
                                  title={t('routing.reason_selected_failover')}
                                  className="flex-shrink-0 inline-flex"
                                >
                                  <ShieldAlert className="w-3.5 h-3.5 text-amber-500" />
                                </span>
                              )}
                              <span className="text-primary font-mono">{cand.effective_value}</span>
                            </div>
                            {cand.failover_active && (
                              <div className="text-[10px] text-amber-500 mt-0.5">
                                {isZh ? `原源站 ${cand.record.value} 已异常` : `origin ${cand.record.value} is down`}
                              </div>
                            )}
                          </td>
                          <td className="py-2.5 px-3">
                            <FlagRegionBadge codeString={cand.record.geo_line} />
                          </td>
                          <td className="py-2.5 px-3">
                            {cand.selected ? (
                              <span className="text-primary font-bold">
                                {Math.round(cand.share_percent)}%
                              </span>
                            ) : (
                              <span className="text-tertiary">0%</span>
                            )}
                          </td>
                          <td className="py-2.5 px-3 text-tertiary">{cand.record.ttl}s</td>
                        </tr>
                      ))}

                      {(result.candidates || []).length === 0 && (
                        <tr>
                          <td colSpan={7} className="py-6 text-center text-secondary">
                            {isZh
                              ? '该名称与类型下未匹配到任何解析记录。'
                              : 'No DNS record matched this name and type.'}
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
