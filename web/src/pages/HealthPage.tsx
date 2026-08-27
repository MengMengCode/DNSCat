import React, { useState, useEffect } from 'react';
import { HeartPulse, Plus, Trash2, RefreshCw } from 'lucide-react';
import { Domain, HealthCheck, DNSRecord } from '../types';
import { Button, Input, Badge, Modal } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface HealthPageProps {
  domain: Domain;
}

export const HealthPage: React.FC<HealthPageProps> = ({ domain }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();
  const [checks, setChecks] = useState<HealthCheck[]>([]);
  const [records, setRecords] = useState<DNSRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [probingId, setProbingId] = useState<number | null>(null);

  // Modal
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [formRecordId, setFormRecordId] = useState<number>(0);
  const [formName, setFormName] = useState('');
  const [formProtocol, setFormProtocol] = useState<'HTTP' | 'HTTPS' | 'TCP' | 'PING'>('HTTP');
  const [formHost, setFormHost] = useState('');
  const [formPort, setFormPort] = useState(80);
  const [formPath, setFormPath] = useState('/');
  const [formFallbackIP, setFormFallbackIP] = useState('');
  const [formInterval, setFormInterval] = useState(15);
  const [formSubmitting, setFormSubmitting] = useState(false);

  const loadData = async () => {
    try {
      setLoading(true);
      const [checksRes, recordsRes] = await Promise.all([
        api.listHealthChecks(domain.id),
        api.listRecords(domain.id),
      ]);
      setChecks(checksRes.health_checks || []);
      const aRecords = (recordsRes.records || []).filter((r) => r.type === 'A' || r.type === 'AAAA' || r.type === 'CNAME');
      setRecords(aRecords);
      if (aRecords.length > 0 && !formHost) {
        setFormRecordId(aRecords[0].id);
        setFormHost(aRecords[0].value);
      }
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [domain.id]);

  const handleOpenAdd = () => {
    if (records.length > 0) {
      setFormRecordId(records[0].id);
      setFormHost(records[0].value);
      setFormName(isZh ? `${records[0].name}.${domain.name} 的探测器` : `Monitor for ${records[0].name}.${domain.name}`);
    } else {
      // 没有可选记录时留空由用户填写，不塞入无意义的示例地址。
      setFormHost('');
      setFormName('');
    }
    setIsModalOpen(true);
  };

  const handleRecordSelect = (recId: number) => {
    setFormRecordId(recId);
    const sel = records.find((r) => r.id === recId);
    if (sel) {
      setFormHost(sel.value);
      setFormName(isZh ? `${sel.name}.${domain.name} 的探测器` : `Monitor for ${sel.name}.${domain.name}`);
    }
  };

  const handleProbeNow = async (id: number) => {
    try {
      setProbingId(id);
      const res = await api.probeHealthCheck(id);
      await alert({
        variant: res.success ? 'success' : 'danger',
        title: isZh ? '探测结果' : 'Probe Result',
        message: isZh
          ? `是否成功: ${res.success}\n延迟: ${res.latency_ms}ms\n消息: ${res.message}`
          : `Success: ${res.success}\nLatency: ${res.latency_ms}ms\nMessage: ${res.message}`,
      });
      loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setProbingId(null);
    }
  };

  const handleDelete = async (id: number) => {
    const ok = await confirm({
      variant: 'danger',
      message: t('common.confirm_delete'),
      confirmText: t('common.delete'),
    });
    if (ok) {
      try {
        await api.deleteHealthCheck(id);
        loadData();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      }
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormSubmitting(true);

    try {
      await api.createHealthCheck({
        domain_id: domain.id,
        record_id: formRecordId || (records[0]?.id || 1),
        name: formName,
        protocol: formProtocol,
        host: formHost.trim(),
        port: Number(formPort),
        path: formPath,
        fallback_ip: formFallbackIP.trim(),
        check_interval_sec: Number(formInterval),
      });
      setIsModalOpen(false);
      loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setFormSubmitting(false);
    }
  };

  // 健康探测器已全量加载到前端，这里做客户端分页，避免探测器较多时表格过长。
  const pager = usePagination(checks, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.health')}</h2>
        <Button
          size="sm"
          variant="primary"
          onClick={handleOpenAdd}
          icon={<Plus className="w-3.5 h-3.5" />}
        >
          {t('health.add_check')}
        </Button>
      </div>

      {/* Health Probers Table */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{isZh ? '探测器名称' : 'Probe Target Name'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '协议与端口' : 'Protocol & Port'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '源站目标' : 'Origin Target Host'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '备用切换 IP' : 'Backup Fallback IP'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '探测周期' : 'Interval'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '延迟' : 'Latency'}</th>
                <th className="py-3 px-4 font-medium">{t('common.status')}</th>
                <th className="py-3 px-4 font-medium text-right">{t('common.action')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((check) => (
                <tr key={check.id} className="hover:bg-bg-subtle/50 transition-colors">
                  <td className="py-3 px-4 text-primary font-bold">
                    <div className="flex items-center gap-2">
                      <HeartPulse className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
                      <span>{check.name}</span>
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant="default" size="sm">
                      {check.protocol === 'PING'
                        ? 'ICMP Echo'
                        : `${check.protocol}:${check.port}`}
                    </Badge>
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold truncate max-w-xs" title={check.host}>
                    {check.host}
                    {(check.protocol === 'HTTP' || check.protocol === 'HTTPS') && check.path}
                  </td>
                  <td className="py-3 px-4 text-secondary">
                    {check.failover_active ? (
                      <Badge variant="warning" size="sm">
                        {isZh ? `已切换 → ${check.fallback_ip}` : `Failed over → ${check.fallback_ip}`}
                      </Badge>
                    ) : (
                      check.fallback_ip || (isZh ? '无' : 'None')
                    )}
                  </td>
                  <td className="py-3 px-4 text-tertiary">
                    {check.check_interval_sec}s
                  </td>
                  <td className="py-3 px-4">
                    <span className={`font-semibold ${
                      check.status === 'healthy'
                        ? 'text-green-500'
                        : check.status === 'degraded'
                          ? 'text-amber-500'
                          : 'text-red-500'
                    }`}>
                      {check.last_latency_ms || 0}ms
                    </span>
                  </td>
                  <td className="py-3 px-4">
                    <Badge
                      variant={
                        check.failover_active
                          ? 'warning'
                          : check.status === 'healthy'
                            ? 'success'
                            : check.status === 'degraded'
                              ? 'warning'
                              : 'error'
                      }
                      size="sm"
                    >
                      {check.failover_active
                        ? (isZh ? '容灾已生效' : 'Failover Active')
                        : check.status === 'healthy'
                          ? t('common.healthy')
                          : check.status === 'degraded'
                            ? (isZh ? '异常确认中' : 'Degraded')
                            : t('common.down')}
                    </Badge>
                  </td>
                  <td className="py-3 px-4 text-right space-x-1">
                    <button
                      onClick={() => handleProbeNow(check.id)}
                      disabled={probingId === check.id}
                      className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                      title={isZh ? '立即探测' : 'Probe Now'}
                    >
                      <RefreshCw className={`w-3.5 h-3.5 ${probingId === check.id ? 'animate-spin' : ''}`} />
                    </button>
                    <button
                      onClick={() => handleDelete(check.id)}
                      className="p-1.5 rounded hover:bg-red-500/10 text-secondary hover:text-red-500 transition-colors cursor-pointer"
                      title={t('common.delete')}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </td>
                </tr>
              ))}

              {checks.length === 0 && !loading && (
                <tr>
                  <td colSpan={8} className="py-8 text-center text-secondary">
                    {isZh
                      ? `${domain.name} 尚未配置源站容灾探测器，点击“${t('health.add_check')}”创建一个。`
                      : `No origin failover probes configured for ${domain.name}. Click "${t('health.add_check')}" to create one.`}
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
        />
      </div>

      {/* Add Prober Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={t('health.add_check')}
        description={isZh ? `${domain.name} 的容灾切换配置` : `Origin failover configuration for ${domain.name}`}
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="text-xs font-medium text-secondary mb-1.5 block">
              {isZh ? '目标 DNS 记录' : 'Target DNS Record'}
            </label>
            <select
              value={formRecordId}
              onChange={(e) => handleRecordSelect(Number(e.target.value))}
              className="w-full h-10 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary"
            >
              {records.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name === '@' ? domain.name : `${r.name}.${domain.name}`} ({r.type} &rarr; {r.value})
                </option>
              ))}
              {records.length === 0 && (
                <option value={0}>{isZh ? '自定义 IP 目标' : 'Custom IP Target'}</option>
              )}
            </select>
          </div>

          <Input
            label={isZh ? '探测器名称' : 'Probe Name'}
            value={formName}
            onChange={(e) => setFormName(e.target.value)}
            placeholder={isZh ? '如：源站 HTTP 探测' : 'e.g. Origin HTTP probe'}
            required
          />

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-secondary mb-1.5 block">
                {isZh ? '协议' : 'Protocol'}
              </label>
              <select
                value={formProtocol}
                onChange={(e) => {
                  const protocol = e.target.value as 'HTTP' | 'HTTPS' | 'TCP' | 'PING';
                  setFormProtocol(protocol);
                  if (protocol === 'HTTP') setFormPort(80);
                  if (protocol === 'HTTPS') setFormPort(443);
                  if (protocol === 'PING') setFormPort(0);
                }}
                className="w-full h-10 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary"
              >
                <option value="HTTP">{isZh ? 'HTTP (状态码 200)' : 'HTTP (Status 200)'}</option>
                <option value="HTTPS">{isZh ? 'HTTPS (状态码 200)' : 'HTTPS (Status 200)'}</option>
                <option value="TCP">{isZh ? 'TCP 端口连通' : 'TCP Port Connect'}</option>
                <option value="PING">{isZh ? 'ICMP Echo (真实 Ping)' : 'ICMP Echo (Real Ping)'}</option>
              </select>
            </div>

            {formProtocol === 'PING' ? (
              <div>
                <label className="text-xs font-medium text-secondary mb-1.5 block">
                  {isZh ? '探测方式' : 'Probe Mode'}
                </label>
                <div className="w-full h-10 bg-bg-subtle text-primary text-xs rounded-sm border border-border px-3 font-mono flex items-center">
                  {isZh ? 'ICMP Echo Request / Reply（无端口）' : 'ICMP Echo Request / Reply (no port)'}
                </div>
              </div>
            ) : (
              <Input
                label={isZh ? '端口' : 'Port'}
                type="number"
                value={formPort}
                onChange={(e) => setFormPort(Number(e.target.value))}
              />
            )}
          </div>

          <Input
            label={isZh ? '源站主机 / IP 地址' : 'Origin Host / IP Address'}
            value={formHost}
            onChange={(e) => setFormHost(e.target.value)}
            required
          />

          <Input
            label={t('health.fallback_ip')}
            value={formFallbackIP}
            onChange={(e) => setFormFallbackIP(e.target.value)}
            placeholder="e.g. 198.51.100.2"
            helper={isZh ? '若探测失败，指向该目标的 DNS 记录将切换到此备用 IP' : 'If probe fails, DNS records pointing to this target will switch to this backup IP'}
          />

          <div className="flex justify-end items-center gap-2 pt-3 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={formSubmitting}>
              {t('health.add_check')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
