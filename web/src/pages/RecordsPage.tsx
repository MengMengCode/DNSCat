import React, { useState, useEffect, useRef } from 'react';
import { 
  Plus, 
  Search, 
  Trash2, 
  Edit3, 
  Copy, 
  Check, 
  ShieldCheck, 
  ExternalLink,
  Layers,
  ArrowRight,
  Globe,
  Info,
  Power,
  PowerOff,
  X
} from 'lucide-react';
import { Domain, DNSRecord, RecordType, RoutingLine } from '../types';
import { Button, Input, Badge, Switch, Modal } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { localizeDualName } from '../lib/format';
import { RoutingLineSelect, RoutingLineBadges } from '../components/RoutingControls';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface RecordsPageProps {
  domain: Domain;
  onRefreshDomain: () => void;
}

const RECORD_TYPES: RecordType[] = [
  'A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA', 'PTR', 
  'SOA', 'ALIAS', 'HTTPS', 'SVCB', 'TLSA', 'SSHFP', 'DS', 'DNSKEY'
];

export const RecordsPage: React.FC<RecordsPageProps> = ({ domain, onRefreshDomain }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  const [records, setRecords] = useState<DNSRecord[]>([]);
  const [routingLines, setRoutingLines] = useState<RoutingLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [lineFilter, setLineFilter] = useState('');

  const [copiedValueId, setCopiedValueId] = useState<number | null>(null);

  // 批量操作选中集合。以记录 ID 为准，跨分页保留选择。
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchLoading, setBatchLoading] = useState(false);
  const selectAllRef = useRef<HTMLInputElement>(null);

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingRecord, setEditingRecord] = useState<DNSRecord | null>(null);

  // Form State
  const [formName, setFormName] = useState('@');
  const [formType, setFormType] = useState<RecordType>('A');
  const [formValue, setFormValue] = useState('');
  const [formTTL, setFormTTL] = useState(300);
  const [formPriority, setFormPriority] = useState(0);
  const [formWeight, setFormWeight] = useState(100);
  const [formPort, setFormPort] = useState(0);
  const [formLine, setFormLine] = useState('default');
  const [formComment, setFormComment] = useState('');
  const [formError, setFormError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const loadData = async () => {
    try {
      setLoading(true);
      const recordsRes = await api.listRecords(domain.id, {
        type: typeFilter,
        search: search,
        geo_line: lineFilter,
      });
      setRecords(recordsRes.records);
    } catch (err: any) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [domain.id, typeFilter, lineFilter]);

  // 加载全局智能分线线路，供记录的 Geo Line 选择器与筛选器使用。
  useEffect(() => {
    api
      .listRoutingLines()
      .then((res) => setRoutingLines(res.lines || []))
      .catch((err) => console.error(err));
  }, []);

  const handleOpenAdd = () => {
    setEditingRecord(null);
    setFormName('@');
    setFormType('A');
    setFormValue('');
    setFormTTL(300);
    setFormPriority(0);
    setFormWeight(100);
    setFormPort(0);
    setFormLine('default');
    setFormComment('');
    setFormError('');
    setIsModalOpen(true);
  };

  const handleOpenEdit = (r: DNSRecord) => {
    setEditingRecord(r);
    setFormName(r.name);
    setFormType(r.type);
    setFormValue(r.value);
    setFormTTL(r.ttl);
    setFormPriority(r.priority || 0);
    setFormWeight(r.weight || 100);
    setFormPort(r.port || 0);
    setFormLine(r.geo_line || 'default');
    setFormComment(r.comment || '');
    setFormError('');
    setIsModalOpen(true);
  };

  const handleSaveRecord = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError('');

    if (!formValue.trim()) {
      setFormError(isZh ? '记录内容 / 目标值为必填项' : 'Record content / target value is required');
      return;
    }

    try {
      setSubmitting(true);
      if (editingRecord) {
        await api.updateRecord(editingRecord.id, {
          name: formName,
          type: formType,
          value: formValue,
          ttl: Number(formTTL),
          priority: Number(formPriority),
          weight: Number(formWeight),
          port: Number(formPort),
          geo_line: formLine,
          comment: formComment,
        });
      } else {
        await api.createRecord(domain.id, {
          name: formName,
          type: formType,
          value: formValue,
          ttl: Number(formTTL),
          priority: Number(formPriority),
          weight: Number(formWeight),
          port: Number(formPort),
          geo_line: formLine,
          comment: formComment,
        });
      }
      setIsModalOpen(false);
      loadData();
      onRefreshDomain();
    } catch (err: any) {
      setFormError(err.message);
    } finally {
      setSubmitting(false);
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
        await api.deleteRecord(id);
        loadData();
        onRefreshDomain();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      }
    }
  };

  const handleToggle = async (id: number) => {
    try {
      await api.toggleRecord(id);
      loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  const handleCopyContent = (val: string, id: number) => {
    navigator.clipboard.writeText(val);
    setCopiedValueId(id);
    setTimeout(() => setCopiedValueId(null), 2000);
  };

  // 记录已在前端全量持有（后端仅按类型 / 线路 / 搜索过滤），此处做客户端分页，
  // 避免记录数很多时一次性渲染成百上千行表格。
  const pager = usePagination(records, 10);

  // 记录集合变化（筛选、刷新、删除）后剔除已不存在的选中项，
  // 避免把陈旧 ID 提交给批量接口。
  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) return prev;
      const alive = new Set(records.map((r) => r.id));
      const next = new Set([...prev].filter((id) => alive.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [records]);

  // 表头复选框作用于「当前页」：全选已是常见预期，跨页全选容易误删。
  const pageIds = pager.pageItems.map((r) => r.id);
  const allPageSelected = pageIds.length > 0 && pageIds.every((id) => selectedIds.has(id));
  const somePageSelected = pageIds.some((id) => selectedIds.has(id));

  // 半选态只能通过 DOM 属性设置，无法用 React 属性表达。
  useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate = !allPageSelected && somePageSelected;
    }
  }, [allPageSelected, somePageSelected]);

  const toggleSelectPage = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (allPageSelected) {
        pageIds.forEach((id) => next.delete(id));
      } else {
        pageIds.forEach((id) => next.add(id));
      }
      return next;
    });
  };

  const toggleSelectOne = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const selectedCount = selectedIds.size;

  // 批量启用 / 禁用 / 删除：一次请求在后端同一事务内完成。
  const runBatch = async (action: 'enable' | 'disable' | 'delete') => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;

    // 删除不可撤销，单独二次确认；启停是可逆操作，直接执行。
    if (action === 'delete') {
      const ok = await confirm({
        variant: 'danger',
        message: isZh
          ? `确定要删除选中的 ${ids.length} 条解析记录吗？此操作不可撤销。`
          : `Delete the ${ids.length} selected record(s)? This cannot be undone.`,
        confirmText: t('common.delete'),
      });
      if (!ok) return;
    }

    try {
      setBatchLoading(true);
      await api.batchRecords(domain.id, {
        ...(action === 'delete' ? { delete_ids: ids } : {}),
        ...(action === 'enable' ? { enable_ids: ids } : {}),
        ...(action === 'disable' ? { disable_ids: ids } : {}),
      });
      setSelectedIds(new Set());
      await loadData();
      // 删除会改变记录总数，需要同步刷新外层域名的记录计数。
      if (action === 'delete') {
        onRefreshDomain();
      }
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setBatchLoading(false);
    }
  };

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 搜索/筛选/操作（右）：行高由控件决定，标题不额外撑高 */}
      <div className="flex flex-col sm:flex-row items-center gap-3">
        <h2 className="text-sm font-semibold text-primary truncate w-full sm:w-auto flex-shrink-0">{t('nav.records')}</h2>
        <div className="relative flex-1 w-full">
          <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-tertiary" />
          <input
            type="text"
            placeholder={t('records.search_placeholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && loadData()}
            className="w-full h-9 bg-card text-primary text-xs rounded-sm border border-border pl-9 pr-3 focus:outline-none focus:border-primary font-mono"
          />
        </div>

        <div className="flex items-center gap-2 w-full sm:w-auto">
          <select
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value)}
            className="h-9 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary"
          >
            <option value="">{t('records.filter_all_types')}</option>
            {RECORD_TYPES.map((tItem) => (
              <option key={tItem} value={tItem}>
                {tItem}
              </option>
            ))}
          </select>

          <select
            value={lineFilter}
            onChange={(e) => setLineFilter(e.target.value)}
            className="h-9 bg-card text-primary text-xs rounded-sm border border-border px-3 font-mono focus:outline-none focus:border-primary"
          >
            <option value="">{isZh ? '全部分线策略' : 'All Geo Lines'}</option>
            <option value="default">{isZh ? '默认线路' : 'Global Default'}</option>
            {routingLines
              .filter((line) => line.key !== 'default')
              .map((line) => (
                <option key={line.id} value={line.key}>
                  {localizeDualName(line.name, isZh) || line.key}
                </option>
              ))}
          </select>
        </div>

        <div className="flex items-center gap-2 w-full sm:w-auto">
          <Button
            size="sm"
            variant="primary"
            onClick={handleOpenAdd}
            icon={<Plus className="w-3.5 h-3.5" />}
          >
            {t('records.add_record')}
          </Button>
        </div>
      </div>

      {/* 批量操作条：仅在有选中项时出现，避免占用常态布局 */}
      {selectedCount > 0 && (
        <div className="geist-card px-4 py-3 flex flex-col sm:flex-row items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-xs w-full sm:w-auto">
            <span className="text-primary font-semibold font-mono">
              {isZh ? `已选中 ${selectedCount} 条记录` : `${selectedCount} record(s) selected`}
            </span>
            <button
              type="button"
              onClick={() => setSelectedIds(new Set())}
              className="inline-flex items-center gap-1 text-tertiary hover:text-primary transition-colors cursor-pointer"
              title={isZh ? '取消选择' : 'Clear selection'}
            >
              <X className="w-3 h-3" />
              <span>{isZh ? '取消选择' : 'Clear'}</span>
            </button>
          </div>

          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Button
              size="sm"
              variant="secondary"
              loading={batchLoading}
              onClick={() => runBatch('enable')}
              icon={<Power className="w-3.5 h-3.5 text-green-500" />}
            >
              {isZh ? '批量启用' : 'Enable'}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              loading={batchLoading}
              onClick={() => runBatch('disable')}
              icon={<PowerOff className="w-3.5 h-3.5 text-amber-500" />}
            >
              {isZh ? '批量禁用' : 'Disable'}
            </Button>
            <Button
              size="sm"
              variant="error"
              loading={batchLoading}
              onClick={() => runBatch('delete')}
              icon={<Trash2 className="w-3.5 h-3.5" />}
            >
              {isZh ? '批量删除' : 'Delete'}
            </Button>
          </div>
        </div>
      )}

      {/* 4. Native HTML Table for DNS Records */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium w-10">
                  <input
                    ref={selectAllRef}
                    type="checkbox"
                    checked={allPageSelected}
                    onChange={toggleSelectPage}
                    disabled={pageIds.length === 0}
                    className="rounded border-border text-primary focus:ring-0 cursor-pointer disabled:cursor-not-allowed"
                    title={isZh ? '选中本页全部记录' : 'Select all on this page'}
                    aria-label={isZh ? '选中本页全部记录' : 'Select all on this page'}
                  />
                </th>
                <th className="py-3 px-4 font-medium">{t('records.col_type')}</th>
                <th className="py-3 px-4 font-medium">{t('records.col_name')}</th>
                <th className="py-3 px-4 font-medium">{t('records.col_content')}</th>
                <th className="py-3 px-4 font-medium">{t('records.col_ttl')}</th>
                <th className="py-3 px-4 font-medium">{t('records.col_line')}</th>
                <th className="py-3 px-4 font-medium">{t('records.col_status')}</th>
                <th className="py-3 px-4 font-medium text-right">{t('records.col_actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((r) => (
                <tr
                  key={r.id}
                  className={`transition-colors ${
                    selectedIds.has(r.id) ? 'bg-primary/5' : 'hover:bg-bg-subtle/50'
                  }`}
                >
                  {/* 行选择 */}
                  <td className="py-3 px-4">
                    <input
                      type="checkbox"
                      checked={selectedIds.has(r.id)}
                      onChange={() => toggleSelectOne(r.id)}
                      className="rounded border-border text-primary focus:ring-0 cursor-pointer"
                      aria-label={isZh ? `选择记录 ${r.name}` : `Select record ${r.name}`}
                    />
                  </td>

                  {/* Type */}
                  <td className="py-3 px-4">
                    <span className="font-bold text-primary px-1.5 py-0.5 rounded bg-bg-subtle border border-border">
                      {r.type}
                    </span>
                  </td>

                  {/* Name */}
                  <td className="py-3 px-4 font-bold text-primary">
                    <span className="hover:underline cursor-pointer" onClick={() => handleOpenEdit(r)}>
                      {r.name}
                    </span>
                  </td>

                  {/* Content / Value */}
                  <td className="py-3 px-4 max-w-xs truncate text-primary group">
                    <div className="flex items-center gap-2">
                      <span className="truncate" title={r.value}>{r.value}</span>
                      <button
                        type="button"
                        onClick={() => handleCopyContent(r.value, r.id)}
                        className="opacity-0 group-hover:opacity-100 hover:text-primary text-secondary transition-opacity cursor-pointer"
                        title={isZh ? '复制内容' : 'Copy content'}
                      >
                        {copiedValueId === r.id ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
                      </button>
                    </div>
                  </td>

                  {/* TTL */}
                  <td className="py-3 px-4 text-secondary">
                    {r.ttl === 1 ? (isZh ? '自动' : 'Auto') : `${r.ttl}s`}
                  </td>

                  {/* Line */}
                  <td className="py-3 px-4">
                    <RoutingLineBadges codeString={r.geo_line || 'default'} lines={routingLines} />
                  </td>

                  {/* Status Toggle */}
                  <td className="py-3 px-4">
                    <Switch
                      checked={r.enabled}
                      onChange={() => handleToggle(r.id)}
                    />
                  </td>

                  {/* Actions */}
                  <td className="py-3 px-4 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <button
                        onClick={() => handleOpenEdit(r)}
                        className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                        title={t('common.edit')}
                      >
                        <Edit3 className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => handleDelete(r.id)}
                        className="p-1.5 rounded hover:bg-red-500/10 border border-border text-secondary hover:text-red-500 transition-colors cursor-pointer"
                        title={t('common.delete')}
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}

              {records.length === 0 && (
                <tr>
                  <td colSpan={8} className="text-center text-secondary py-8">
                    {loading ? t('common.loading') : (isZh ? '暂无匹配的 DNS 解析记录。' : 'No DNS records found.')}
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

      {/* Record Edit/Create Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editingRecord ? t('records.edit_record') : t('records.add_record')}
      >
        <form onSubmit={handleSaveRecord} className="space-y-4 font-mono text-xs">
          {formError && (
            <div className="p-3 rounded bg-red-500/10 border border-red-500/20 text-red-500">
              {formError}
            </div>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div>
              <label className="text-xs font-medium text-secondary block mb-1.5">
                {t('records.form_type')}
              </label>
              <select
                value={formType}
                onChange={(e) => setFormType(e.target.value as RecordType)}
                className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
              >
                {RECORD_TYPES.map((tItem) => (
                  <option key={tItem} value={tItem}>
                    {tItem}
                  </option>
                ))}
              </select>
            </div>

            <div className="sm:col-span-2">
              <Input
                label={t('records.form_name')}
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder={isZh ? '@ 或 www' : '@ or www'}
                required
              />
            </div>
          </div>

          <Input
            label={t('records.form_value')}
            value={formValue}
            onChange={(e) => setFormValue(e.target.value)}
            placeholder={
              formType === 'A'
                ? '192.0.2.1'
                : formType === 'AAAA'
                ? '2001:db8::1'
                : formType === 'CNAME'
                ? 'example.com.'
                : formType === 'MX'
                ? 'mail.example.com.'
                : (isZh ? '目标解析内容' : 'Target content')
            }
            required
          />

          <div className="grid grid-cols-2 gap-3">
            <Input
              label={t('records.form_ttl')}
              type="number"
              value={formTTL}
              onChange={(e) => setFormTTL(Number(e.target.value))}
              placeholder="300"
            />

            {(formType === 'MX' || formType === 'SRV') && (
              <Input
                label={t('records.form_priority')}
                type="number"
                value={formPriority}
                onChange={(e) => setFormPriority(Number(e.target.value))}
                placeholder="10"
              />
            )}

            {formType === 'SRV' && (
              <Input
                label={t('records.form_port')}
                type="number"
                value={formPort}
                onChange={(e) => setFormPort(Number(e.target.value))}
                placeholder="443"
              />
            )}
          </div>

          {/* Smart Routing Line Selector (user-defined lines) */}
          <RoutingLineSelect
            label={t('records.form_line')}
            lines={routingLines}
            value={formLine}
            onChange={(val) => setFormLine(val)}
          />

          <Input
            label={t('records.form_comment')}
            value={formComment}
            onChange={(e) => setFormComment(e.target.value)}
            placeholder={isZh ? '备注信息 (可选)' : 'Optional comment'}
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={submitting}
            >
              {t('common.save')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
