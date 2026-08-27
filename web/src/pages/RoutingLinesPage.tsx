import React, { useState, useEffect } from 'react';
import { Plus, Edit3, Trash2, Globe, Server, Lock, Layers, Zap, SlidersHorizontal } from 'lucide-react';
import { RoutingLine } from '../types';
import { Button, Input, Modal, Switch } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import {
  CountryMultiSelect,
  getContinentLabel,
  getContinentFlag,
} from '../components/RoutingControls';
import { getRegionOption } from '../components/FlagRegionSelect';
import { localizeDualName } from '../lib/format';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

const splitCsv = (csv: string): string[] =>
  (csv || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

export const RoutingLinesPage: React.FC = () => {
  const { language, t } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  const [lines, setLines] = useState<RoutingLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editing, setEditing] = useState<RoutingLine | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Form state
  const [formName, setFormName] = useState('');
  const [formKey, setFormKey] = useState('');
  const [formDescription, setFormDescription] = useState('');
  // 大洲维度不在表单中暴露：七大洲已由内置线路覆盖，用户自建线路只按国家 / ASN 分流。
  // 仍保留该 state 以承载「编辑内置线路时的原大洲值」，避免保存时把内置线路的大洲定义清空。
  const [formContinents, setFormContinents] = useState('');
  const [formCountries, setFormCountries] = useState('');
  const [formAsns, setFormAsns] = useState('');
  const [formPriority, setFormPriority] = useState(100);
  const [formEnabled, setFormEnabled] = useState(true);

  const loadLines = async () => {
    try {
      setLoading(true);
      const res = await api.listRoutingLines();
      setLines(res.lines || []);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadLines();
  }, []);

  const openCreate = () => {
    setEditing(null);
    setFormName('');
    setFormKey('');
    setFormDescription('');
    setFormContinents('');
    setFormCountries('');
    setFormAsns('');
    setFormPriority(100);
    setFormEnabled(true);
    setIsModalOpen(true);
  };

  const openEdit = (line: RoutingLine) => {
    setEditing(line);
    setFormName(line.name);
    setFormKey(line.key);
    setFormDescription(line.description || '');
    setFormContinents(line.continents || '');
    setFormCountries(line.countries || '');
    setFormAsns(line.asns || '');
    setFormPriority(line.priority);
    setFormEnabled(line.enabled);
    setIsModalOpen(true);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formName.trim()) {
      await alert(isZh ? '请填写线路名称' : 'Please enter a line name');
      return;
    }
    if (!formContinents && !formCountries && !formAsns) {
      await alert(
        isZh
          ? '请至少配置一个匹配维度（国家 / ASN）'
          : 'Configure at least one dimension (country / ASN)'
      );
      return;
    }

    const payload: Partial<RoutingLine> = {
      name: formName.trim(),
      key: formKey.trim(),
      description: formDescription.trim(),
      continents: formContinents,
      countries: formCountries,
      asns: formAsns,
      priority: Number(formPriority) || 100,
      enabled: formEnabled,
    };

    try {
      setSubmitting(true);
      if (editing) {
        await api.updateRoutingLine(editing.id, payload);
      } else {
        await api.createRoutingLine(payload);
      }
      setIsModalOpen(false);
      await loadLines();
    } catch (err: any) {
      await alert(err.message || (isZh ? '保存线路失败' : 'Failed to save routing line'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (line: RoutingLine) => {
    const ok = await confirm({
      variant: 'danger',
      message: isZh
        ? `确定要删除智能分线线路「${localizeDualName(line.name, true)}」吗？引用该线路的解析记录将回退到默认线路。`
        : `Delete routing line "${localizeDualName(line.name, false)}"? Records referencing it will fall back to the default line.`,
    });
    if (!ok) return;
    try {
      await api.deleteRoutingLine(line.id);
      await loadLines();
    } catch (err: any) {
      await alert(err.message || (isZh ? '删除失败' : 'Failed to delete'));
    }
  };

  // 线路图标磁贴：单一大洲的线路（含内置七大洲）用该大洲对应的 emoji 且中性底色；
  // 按国家 / ASN 或跨多大洲分流的线路用蓝色调 Globe；无维度的兜底线路用灰调。
  const renderLineTile = (line: RoutingLine) => {
    const continents = splitCsv(line.continents);
    const countries = splitCsv(line.countries);
    const asns = splitCsv(line.asns);
    const hasAny = continents.length || countries.length || asns.length;

    if (continents.length === 1) {
      return (
        <div className="w-8 h-8 rounded-md flex items-center justify-center text-sm bg-bg-subtle border border-border flex-shrink-0">
          <span className="leading-none" title={getContinentLabel(continents[0], isZh)}>
            {getContinentFlag(continents[0])}
          </span>
        </div>
      );
    }
    if (!hasAny) {
      return (
        <div className="w-8 h-8 rounded-md flex items-center justify-center bg-bg-subtle border border-border text-tertiary flex-shrink-0">
          <Server className="w-3.5 h-3.5" />
        </div>
      );
    }
    return (
      <div className="w-8 h-8 rounded-md flex items-center justify-center bg-blue-500/10 border border-blue-500/20 text-blue-500 flex-shrink-0">
        <Globe className="w-3.5 h-3.5" />
      </div>
    );
  };

  const renderDimensions = (line: RoutingLine) => {
    const continents = splitCsv(line.continents);
    const countries = splitCsv(line.countries);
    const asns = splitCsv(line.asns);

    if (continents.length === 0 && countries.length === 0 && asns.length === 0) {
      return (
        <span className="text-[11px] text-tertiary italic">
          {isZh ? '兜底默认线路' : 'Fallback default line'}
        </span>
      );
    }

    return (
      <div className="flex flex-wrap items-center gap-1.5">
        {continents.map((c) => (
          <span
            key={`c-${c}`}
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-blue-500/10 border border-blue-500/20 text-[11px] text-blue-500"
          >
            <span className="leading-none">{getContinentFlag(c)}</span>
            <span>{getContinentLabel(c, isZh)}</span>
          </span>
        ))}
        {countries.map((c) => {
          const opt = getRegionOption(c);
          return (
            <span
              key={`co-${c}`}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-bg-subtle border border-border text-[11px] text-secondary"
            >
              <span>{opt.flag}</span>
              <span>{isZh ? opt.nameZh.split(' ')[0] : opt.nameEn}</span>
            </span>
          );
        })}
        {asns.map((a) => (
          <span
            key={`a-${a}`}
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-purple-500/10 border border-purple-500/20 text-[11px] text-purple-500"
          >
            AS{a}
          </span>
        ))}
      </div>
    );
  };

  // 顶部概览指标
  const stats = {
    total: lines.length,
    enabled: lines.filter((l) => l.enabled).length,
    builtin: lines.filter((l) => l.is_builtin).length,
    custom: lines.filter((l) => !l.is_builtin).length,
  };

  // 分线线路已全量加载到前端，这里做客户端分页。
  const pager = usePagination(lines, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 页眉：标题 + 副标题（左） / 新建（右） */}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-base font-bold text-primary tracking-tight truncate">
            {t('nav.routing_lines')}
          </h2>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? '按大洲 / 国家 / 运营商 (ASN) 将解析分流到最优节点，命中最具体线路优先。'
              : 'Split resolution to the optimal node by continent / country / ASN — most specific line wins.'}
          </p>
        </div>
        <Button size="sm" variant="primary" onClick={openCreate} icon={<Plus className="w-3.5 h-3.5" />}>
          {isZh ? '新建线路' : 'New Line'}
        </Button>
      </div>

      {/* 概览指标条 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {[
          { label: isZh ? '线路总数' : 'Total Lines', value: stats.total, icon: <Layers className="w-4 h-4" />, tint: 'text-secondary' },
          { label: isZh ? '启用中' : 'Enabled', value: stats.enabled, icon: <Zap className="w-4 h-4" />, tint: 'text-green-500' },
          { label: isZh ? '内置线路' : 'Built-in', value: stats.builtin, icon: <Lock className="w-4 h-4" />, tint: 'text-secondary' },
          { label: isZh ? '自建线路' : 'Custom', value: stats.custom, icon: <SlidersHorizontal className="w-4 h-4" />, tint: 'text-blue-500' },
        ].map((s) => (
          <div key={s.label} className="geist-card p-3.5 flex items-center justify-between">
            <div className="flex flex-col gap-0.5 min-w-0">
              <span className="text-[11px] text-tertiary truncate">{s.label}</span>
              <span className="text-lg font-bold tracking-tight text-primary">{s.value}</span>
            </div>
            <span className={s.tint}>{s.icon}</span>
          </div>
        ))}
      </div>

      {/* 线路表格 */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-semibold uppercase tracking-wide">
                  {isZh ? '线路名称 / 标识' : 'Line Name / Key'}
                </th>
                <th className="py-3 px-4 font-semibold uppercase tracking-wide">
                  {isZh ? '匹配维度' : 'Match Dimensions'}
                </th>
                <th className="py-3 px-4 font-semibold uppercase tracking-wide">
                  {isZh ? '优先级' : 'Priority'}
                </th>
                <th className="py-3 px-4 font-semibold uppercase tracking-wide">
                  {isZh ? '状态' : 'Status'}
                </th>
                <th className="py-3 px-4 font-semibold uppercase tracking-wide text-right">
                  {isZh ? '操作' : 'Actions'}
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((line) => {
                const locked = line.is_builtin || (line.ref_count ?? 0) > 0;
                return (
                  <tr key={line.id} className="hover:bg-bg-subtle/50 transition-colors group">
                    {/* 名称 / 标识 */}
                    <td className="py-3 px-4">
                      <div className="flex items-center gap-3">
                        {renderLineTile(line)}
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className="text-primary font-bold truncate">{localizeDualName(line.name, isZh)}</span>
                            {line.is_builtin && (
                              <span title={isZh ? '内置线路' : 'Built-in'}>
                                <Lock className="w-3 h-3 text-tertiary flex-shrink-0" />
                              </span>
                            )}
                          </div>
                          <div className="text-tertiary text-[11px] font-mono truncate">{line.key}</div>
                        </div>
                      </div>
                    </td>

                    {/* 匹配维度 */}
                    <td className="py-3 px-4">{renderDimensions(line)}</td>

                    {/* 优先级 */}
                    <td className="py-3 px-4">
                      <span
                        className="text-[11px] font-mono text-secondary bg-bg-subtle border border-border rounded-full px-2 py-0.5"
                        title={isZh ? '匹配优先级（越小越优先）' : 'Priority (lower wins)'}
                      >
                        P{line.priority}
                      </span>
                    </td>

                    {/* 状态 */}
                    <td className="py-3 px-4">
                      <div className="flex items-center gap-2">
                        <span className="inline-flex items-center gap-1.5">
                          <span
                            className={`w-1.5 h-1.5 rounded-full ${line.enabled ? 'bg-green-500' : 'bg-gray-500'}`}
                          />
                          <span className="text-[11px] text-secondary">
                            {line.enabled ? (isZh ? '启用' : 'Enabled') : (isZh ? '停用' : 'Disabled')}
                          </span>
                        </span>
                        {(line.ref_count ?? 0) > 0 && (
                          <span
                            className="text-[11px] text-tertiary"
                            title={isZh ? '被解析记录引用中，不可删除' : 'Referenced by records, cannot be deleted'}
                          >
                            {isZh ? `· ${line.ref_count} 条在用` : `· ${line.ref_count} in use`}
                          </span>
                        )}
                      </div>
                    </td>

                    {/* 操作 */}
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5 opacity-70 group-hover:opacity-100 transition-opacity">
                        <button
                          onClick={() => openEdit(line)}
                          className="p-1.5 rounded-sm border border-border text-secondary hover:text-primary hover:bg-bg-subtle transition-colors cursor-pointer"
                          title={isZh ? '编辑' : 'Edit'}
                        >
                          <Edit3 className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDelete(line)}
                          disabled={locked}
                          className="p-1.5 rounded-sm border border-border text-secondary transition-colors disabled:opacity-40 disabled:cursor-not-allowed enabled:hover:bg-red-500/10 enabled:hover:text-red-500 enabled:hover:border-red-500/20 cursor-pointer"
                          title={
                            line.is_builtin
                              ? (isZh ? '内置线路不可删除' : 'Built-in line cannot be deleted')
                              : (line.ref_count ?? 0) > 0
                                ? (isZh ? `有 ${line.ref_count} 条解析记录在使用，不可删除` : `In use by ${line.ref_count} record(s), cannot delete`)
                                : (isZh ? '删除' : 'Delete')
                          }
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}

              {lines.length === 0 && !loading && (
                <tr>
                  <td colSpan={5} className="text-center text-secondary py-10">
                    <div className="flex flex-col items-center justify-center gap-3">
                      <div className="w-11 h-11 rounded-lg bg-bg-subtle border border-border flex items-center justify-center text-tertiary">
                        <Globe className="w-5 h-5" />
                      </div>
                      <div className="text-sm text-primary font-medium">
                        {isZh ? '暂无智能分线线路' : 'No routing lines yet'}
                      </div>
                      <div className="text-xs text-tertiary max-w-sm">
                        {isZh
                          ? '创建线路后，可在解析记录中为不同地区 / 运营商指定不同的应答结果。'
                          : 'Once created, records can return different answers per region / carrier.'}
                      </div>
                      <Button size="sm" variant="primary" onClick={openCreate} icon={<Plus className="w-3.5 h-3.5" />}>
                        {isZh ? '新建线路' : 'New Line'}
                      </Button>
                    </div>
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

      {/* Add / Edit Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editing ? (isZh ? '编辑智能分线线路' : 'Edit Routing Line') : (isZh ? '新建智能分线线路' : 'New Routing Line')}
        maxWidth="lg"
      >
        <form onSubmit={handleSave} className="space-y-4 font-mono text-xs">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input
              label={isZh ? '线路名称' : 'Line Name'}
              value={formName}
              onChange={(e) => setFormName(e.target.value)}
              placeholder={isZh ? '如：亚太加速线路' : 'e.g. APAC Acceleration'}
              required
              autoFocus
            />
            <Input
              label={isZh ? '线路标识（留空自动生成）' : 'Line Key (auto if blank)'}
              value={formKey}
              onChange={(e) => setFormKey(e.target.value)}
              placeholder="apac-accel"
              disabled={!!editing?.is_builtin}
              helper={editing?.is_builtin ? (isZh ? '内置线路标识不可修改' : 'Built-in key is locked') : undefined}
            />
          </div>

          <Input
            label={isZh ? '描述 (可选)' : 'Description (optional)'}
            value={formDescription}
            onChange={(e) => setFormDescription(e.target.value)}
            placeholder={isZh ? '线路用途说明' : 'What this line targets'}
          />

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-secondary block">
              {isZh ? '国家 / 地区维度' : 'Country / Region Dimension'}
            </label>
            <CountryMultiSelect value={formCountries} onChange={setFormCountries} />
          </div>

          <Input
            label={isZh ? 'ASN 维度 (逗号分隔，如 4134,4837)' : 'ASN Dimension (comma separated)'}
            value={formAsns}
            onChange={(e) => setFormAsns(e.target.value)}
            placeholder="4134,4837,9808"
            helper={isZh ? '自治域号，可带或不带 AS 前缀' : 'Autonomous System numbers, with or without AS prefix'}
          />

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 items-center">
            <Input
              label={isZh ? '匹配优先级 (数字越小越优先)' : 'Priority (lower = higher)'}
              type="number"
              value={formPriority}
              onChange={(e) => setFormPriority(Number(e.target.value))}
              placeholder="100"
            />
            <div className="flex items-center gap-2 pt-5">
              <Switch checked={formEnabled} onChange={setFormEnabled} />
              <span className="text-xs text-primary">
                {isZh ? '启用此线路' : 'Enable this line'}
              </span>
            </div>
          </div>

          <div className="p-3 rounded-sm bg-bg-subtle border border-border text-[11px] text-tertiary flex items-start gap-2">
            <Server className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
            <span>
              {isZh
                ? '国家与 ASN 维度之间为「或」关系：客户端的国家或 ASN 命中任一维度即视为匹配该线路。大洲级分流已由内置的七大洲线路覆盖。保存后自动同步到全部边缘节点。'
                : 'The country and ASN dimensions are OR-combined: a client matches if its country or ASN hits either dimension. Continent-level routing is covered by the built-in seven-continent lines. Changes sync to all edge nodes automatically.'}
            </span>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button type="button" variant="secondary" size="sm" onClick={() => setIsModalOpen(false)}>
              {isZh ? '取消' : 'Cancel'}
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={submitting}>
              {isZh ? '保存' : 'Save'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
