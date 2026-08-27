import React, { useState } from 'react';
import {
  Globe,
  Plus,
  Search,
  Trash2,
  RefreshCw,
  ShieldCheck,
  Eye
} from 'lucide-react';
import { Domain } from '../types';
import { Button, Badge } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { DNSSECModal } from '../components/DNSSECModal';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface DomainsPageProps {
  domains: Domain[];
  onSelectDomain: (d: Domain) => void;
  onOpenAddDomain: () => void;
  onRefreshDomains: () => void;
}

// 后端返回的状态枚举 → i18n key。未收录的值回退显示原始字符串，避免渲染空白。
const STATUS_LABEL_KEYS: Record<string, string> = {
  active: 'domains.status_active',
  pending: 'domains.status_pending',
  paused: 'domains.status_paused',
};

const NS_STATUS_LABEL_KEYS: Record<string, string> = {
  verified: 'domains.ns_verified',
  pending: 'domains.ns_pending',
  unverified: 'domains.ns_unverified',
};

export const DomainsPage: React.FC<DomainsPageProps> = ({
  domains,
  onSelectDomain,
  onOpenAddDomain,
  onRefreshDomains,
}) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert, prompt } = useDialog();
  const [search, setSearch] = useState('');
  const [verifyingId, setVerifyingId] = useState<number | null>(null);
  const [dnssecDomain, setDnssecDomain] = useState<Domain | null>(null);

  const filtered = domains.filter((d) =>
    d.name.toLowerCase().includes(search.toLowerCase())
  );

  const labelFor = (keys: Record<string, string>, value: string): string => {
    const key = keys[value];
    return key ? t(key) : value.toUpperCase();
  };

  const handleVerifyNS = async (d: Domain, e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      setVerifyingId(d.id);
      const res = await api.verifyNS(d.id);
      if (res.verified) {
        await alert({
          variant: 'success',
          message: isZh
            ? `NS 校验成功！域名 [${d.name}] 已成功指向 DnsCat 权威服务器集群。`
            : `Verified! [${d.name}] is properly pointing to DnsCat nameservers.`,
        });
      } else {
        await alert({
          variant: 'warning',
          message: isZh
            ? `[${d.name}] 尚未检测到公网指向：\n期望权威 NS: ${res.expected_ns?.join(', ')}\n当前公网检测 NS: ${res.found_ns?.length > 0 ? res.found_ns.join(', ') : '未检测到记录'}\n\n若刚在服务商处修改，请等待 5~10 分钟公网 DNS 缓存刷新。`
            : `[${d.name}] Not yet pointing to DnsCat:\nExpected: ${res.expected_ns?.join(', ')}\nFound: ${res.found_ns?.join(', ') || 'None'}\n\nPlease allow a few minutes for global DNS propagation.`,
        });
      }
      onRefreshDomains();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setVerifyingId(null);
    }
  };

  const handleDelete = async (d: Domain, e: React.MouseEvent) => {
    e.stopPropagation();
    const conf = await prompt({
      variant: 'danger',
      title: isZh ? '删除域名区域' : 'Delete Zone',
      message: isZh
        ? `此操作不可撤销。请输入 "${d.name}" 以确认删除该域名区域：`
        : `This action cannot be undone. Type "${d.name}" to confirm zone deletion:`,
      placeholder: d.name,
      confirmText: t('common.delete'),
    });
    if (conf === d.name) {
      try {
        await api.deleteDomain(d.id);
        onRefreshDomains();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      }
    }
  };

  // 域名列表已在前端持有并按搜索过滤，这里做客户端分页，避免域名很多时表格过长。
  const pager = usePagination(filtered, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 搜索/操作（右）：行高由控件决定，标题不额外撑高 */}
      <div className="flex flex-col sm:flex-row items-center gap-3">
        <h2 className="text-sm font-semibold text-primary truncate w-full sm:w-auto flex-shrink-0">{t('nav.domains_mgmt')}</h2>
        <div className="relative flex-1 w-full">
          <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-tertiary" />
          <input
            type="text"
            placeholder={t('common.search') + '...'}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full h-9 bg-card text-primary text-xs rounded-sm border border-border pl-9 pr-3 focus:outline-none focus:border-primary font-mono"
          />
        </div>

        <Button
          size="sm"
          variant="primary"
          onClick={onOpenAddDomain}
          icon={<Plus className="w-3.5 h-3.5" />}
        >
          {t('common.add_domain')}
        </Button>
      </div>

      {/* Domains Data Table */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{t('domains.col_name')}</th>
                <th className="py-3 px-4 font-medium">{t('common.status')}</th>
                <th className="py-3 px-4 font-medium">{isZh ? 'NS 指向状态' : 'Nameserver Status'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '主权威 NS' : 'Primary NS'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '记录数' : 'Records'}</th>
                <th className="py-3 px-4 font-medium">DNSSEC</th>
                <th className="py-3 px-4 font-medium text-right">{t('common.action')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {/* 整行不再可点击：避免点操作按钮或选中文本时误跳转，
                  仅域名文字与操作列的查看按钮进入详情。 */}
              {pager.pageItems.map((d) => (
                <tr key={d.id} className="hover:bg-bg-subtle/50 transition-colors">
                  <td className="py-3 px-4 text-primary font-bold">
                    <div className="flex items-center gap-2">
                      <Globe className="w-4 h-4 text-secondary flex-shrink-0" />
                      <button
                        type="button"
                        onClick={() => onSelectDomain(d)}
                        className="text-primary font-bold hover:underline cursor-pointer text-left"
                        title={isZh ? '进入域名详情' : 'Open domain details'}
                      >
                        {d.name}
                      </button>
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant={d.status === 'active' ? 'success' : 'warning'} size="sm">
                      {labelFor(STATUS_LABEL_KEYS, d.status)}
                    </Badge>
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant={d.ns_status === 'verified' ? 'success' : 'default'} size="sm">
                      {labelFor(NS_STATUS_LABEL_KEYS, d.ns_status)}
                    </Badge>
                  </td>
                  {/* 展示该域名 apex 上实际配置的全部权威 NS；缺失时回退到 SOA 主 NS，
                      不硬编码任何默认 NS 主机名（各部署的权威 NS 不同）。 */}
                  <td className="py-3 px-4 text-secondary">
                    {(() => {
                      const nsList =
                        d.ns_records && d.ns_records.length > 0
                          ? d.ns_records
                          : d.primary_ns
                            ? [d.primary_ns]
                            : [];
                      if (nsList.length === 0) {
                        return <span className="text-tertiary">-</span>;
                      }
                      return (
                        <div className="space-y-0.5" title={nsList.join('\n')}>
                          {nsList.map((ns) => (
                            <div key={ns} className="truncate max-w-[240px]">
                              {ns.replace(/\.$/, '')}
                            </div>
                          ))}
                        </div>
                      );
                    })()}
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold">
                    {d.record_count || 0}
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant={d.dnssec_enabled ? 'success' : 'default'} size="sm">
                      {d.dnssec_enabled ? t('domains.dnssec_on') : t('domains.dnssec_off')}
                    </Badge>
                  </td>
                  <td className="py-3 px-4 text-right space-x-1">
                    <button
                      onClick={() => onSelectDomain(d)}
                      className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                      title={isZh ? '查看域名详情' : 'View domain details'}
                      aria-label={isZh ? '查看域名详情' : 'View domain details'}
                    >
                      <Eye className="w-3.5 h-3.5" />
                    </button>
                    <button
                      onClick={(e) => handleVerifyNS(d, e)}
                      disabled={verifyingId === d.id}
                      className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                      title={isZh ? '校验 NS 指向' : 'Verify Nameservers'}
                    >
                      <RefreshCw className={`w-3.5 h-3.5 ${verifyingId === d.id ? 'animate-spin' : ''}`} />
                    </button>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setDnssecDomain(d);
                      }}
                      className={`p-1.5 rounded hover:bg-bg-subtle transition-colors cursor-pointer ${
                        d.dnssec_enabled ? 'text-primary' : 'text-secondary hover:text-primary'
                      }`}
                      title={isZh ? '配置 DNSSEC 防伪签名' : 'Configure DNSSEC'}
                    >
                      <ShieldCheck className="w-3.5 h-3.5" />
                    </button>
                    <button
                      onClick={(e) => handleDelete(d, e)}
                      className="p-1.5 rounded hover:bg-red-500/10 text-secondary hover:text-red-500 transition-colors cursor-pointer"
                      title={t('common.delete')}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </td>
                </tr>
              ))}

              {filtered.length === 0 && (
                <tr>
                  <td colSpan={7} className="py-8 text-center text-secondary">
                    {isZh
                      ? `暂无域名，点击“${t('common.add_domain')}”注册一个。`
                      : `No domains found. Click "${t('common.add_domain')}" to register one.`}
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

      <DNSSECModal
        domain={dnssecDomain}
        isOpen={!!dnssecDomain}
        onClose={() => setDnssecDomain(null)}
        onRefreshDomains={onRefreshDomains}
      />
    </div>
  );
};
