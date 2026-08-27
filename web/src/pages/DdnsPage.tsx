import React, { useState, useEffect } from 'react';
import {
  RefreshCw,
  Plus,
  Trash2,
  Pencil,
  KeyRound,
  ShieldCheck,
  ShieldAlert,
  AlertTriangle,
} from 'lucide-react';
import { Domain, DdnsKey, DdnsKeyPayload } from '../types';
import { Button, Input, Badge, Modal, Switch, CodeBox } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface DdnsPageProps {
  domain: Domain;
}

// 新建密钥表单的初始值。授权主机名默认给 ddns 这个子域，
// 避免用户一上手就把整个域名（@ 或 *）暴露给一把动态更新密钥。
const emptyForm = {
  name: '',
  hostnames: 'ddns',
  allowed_ips: '',
  record_ttl: 60,
  allow_ipv4: true,
  allow_ipv6: false,
  auto_create: true,
  enabled: true,
};

export const DdnsPage: React.FC<DdnsPageProps> = ({ domain }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  const [keys, setKeys] = useState<DdnsKey[]>([]);
  const [loading, setLoading] = useState(true);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<DdnsKey | null>(null);
  const [form, setForm] = useState({ ...emptyForm });
  const [formError, setFormError] = useState('');
  const [formSubmitting, setFormSubmitting] = useState(false);

  // 新签发的明文密钥。只在内存里存在，关闭弹窗即丢弃——服务端也取不回来。
  const [issued, setIssued] = useState<{ key: string; title: string } | null>(null);

  const loadData = async () => {
    try {
      setLoading(true);
      const res = await api.listDdnsKeys(domain.id);
      setKeys(res.ddns_keys || []);
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
    setEditingKey(null);
    setForm({
      ...emptyForm,
      name: isZh ? `${domain.name} 家庭宽带` : `${domain.name} home broadband`,
    });
    setFormError('');
    setIsModalOpen(true);
  };

  const handleOpenEdit = (key: DdnsKey) => {
    setEditingKey(key);
    setForm({
      name: key.name,
      hostnames: key.hostnames,
      allowed_ips: key.allowed_ips,
      record_ttl: key.record_ttl,
      allow_ipv4: key.allow_ipv4,
      allow_ipv6: key.allow_ipv6,
      auto_create: key.auto_create,
      enabled: key.enabled,
    });
    setFormError('');
    setIsModalOpen(true);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError('');
    setFormSubmitting(true);

    const payload: DdnsKeyPayload = {
      name: form.name.trim(),
      hostnames: form.hostnames.trim(),
      allowed_ips: form.allowed_ips.trim(),
      record_ttl: Number(form.record_ttl) || 60,
      allow_ipv4: form.allow_ipv4,
      allow_ipv6: form.allow_ipv6,
      auto_create: form.auto_create,
      enabled: form.enabled,
    };

    try {
      if (editingKey) {
        await api.updateDdnsKey(editingKey.id, payload);
        setIsModalOpen(false);
      } else {
        const res = await api.createDdnsKey(domain.id, payload);
        setIsModalOpen(false);
        setIssued({ key: res.key, title: t('ddns.key_created') });
      }
      await loadData();
    } catch (err: any) {
      setFormError(err.message);
    } finally {
      setFormSubmitting(false);
    }
  };

  const handleRotate = async (key: DdnsKey) => {
    const ok = await confirm({
      variant: 'warning',
      message: t('ddns.rotate_confirm'),
      confirmText: t('ddns.rotate'),
    });
    if (!ok) return;

    try {
      const res = await api.rotateDdnsKey(key.id);
      setIssued({ key: res.key, title: t('ddns.key_rotated') });
      await loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  const handleDelete = async (key: DdnsKey) => {
    const ok = await confirm({
      variant: 'danger',
      message: t('common.confirm_delete'),
      confirmText: t('common.delete'),
    });
    if (!ok) return;

    try {
      await api.deleteDdnsKey(key.id);
      await loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  // 更新地址取当前页面所在源，这样用户复制出来的配置在自建部署下也是对的。
  const updateBase = `${window.location.protocol}//${window.location.host}`;
  const sampleHost = (() => {
    const first = form.hostnames.split(',')[0]?.trim() || 'ddns';
    return first === '@' || first === '*' ? domain.name : `${first}.${domain.name}`;
  })();

  const clientSnippets = (plainKey: string) => [
    {
      label: isZh ? '通用 URL（路由器 / NAS 自定义 DDNS）' : 'Generic URL (routers / NAS custom DDNS)',
      code: `${updateBase}/nic/update?hostname=${sampleHost}&myip=<ip>`,
    },
    {
      label: isZh ? 'curl（用 HTTP Basic 认证，用户名任意）' : 'curl (HTTP Basic auth, username can be anything)',
      code: `curl -u "dnscat:${plainKey}" \\\n  "${updateBase}/nic/update?hostname=${sampleHost}"`,
    },
    {
      label: 'ddclient (/etc/ddclient.conf)',
      code: [
        'protocol=dyndns2',
        `server=${window.location.host}`,
        'login=dnscat',
        `password='${plainKey}'`,
        `ssl=${window.location.protocol === 'https:' ? 'yes' : 'no'}`,
        sampleHost,
      ].join('\n'),
    },
  ];

  const renderAllowlist = (key: DdnsKey) => {
    if (!key.allowed_ips) {
      return (
        <span className="inline-flex items-center gap-1 text-amber-500" title={isZh ? '任意来源都可使用此密钥' : 'Any source may use this key'}>
          <ShieldAlert className="w-3.5 h-3.5 flex-shrink-0" />
          {t('ddns.no_ip_limit')}
        </span>
      );
    }
    return (
      <span className="inline-flex items-center gap-1 text-green-500" title={key.allowed_ips}>
        <ShieldCheck className="w-3.5 h-3.5 flex-shrink-0" />
        <span className="truncate max-w-[14rem]">{key.allowed_ips}</span>
      </span>
    );
  };

  // DDNS 密钥已全量加载到前端，这里做客户端分页，避免密钥较多时表格过长。
  const pager = usePagination(keys, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.ddns')}</h2>
        <Button
          size="sm"
          variant="primary"
          onClick={handleOpenAdd}
          icon={<Plus className="w-3.5 h-3.5" />}
        >
          {t('ddns.add_key')}
        </Button>
      </div>

      <div className="geist-card p-4 text-xs text-secondary font-mono leading-relaxed">
        {t('ddns.subtitle')}
        <div className="mt-2 text-tertiary">
          {isZh ? '更新地址' : 'Update endpoint'}:{' '}
          <span className="text-primary">{updateBase}/nic/update</span>
        </div>
      </div>

      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{t('ddns.col_name')}</th>
                <th className="py-3 px-4 font-medium">{t('ddns.col_hostnames')}</th>
                <th className="py-3 px-4 font-medium">{t('ddns.col_allowed_ips')}</th>
                <th className="py-3 px-4 font-medium">{t('ddns.col_current_ip')}</th>
                <th className="py-3 px-4 font-medium">TTL</th>
                <th className="py-3 px-4 font-medium">{t('ddns.col_last_update')}</th>
                <th className="py-3 px-4 font-medium">{t('common.status')}</th>
                <th className="py-3 px-4 font-medium text-right">{t('common.action')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((key) => (
                <tr key={key.id} className="hover:bg-bg-subtle/50 transition-colors">
                  <td className="py-3 px-4 text-primary font-bold">
                    <div className="flex items-center gap-2">
                      <KeyRound className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
                      <div className="min-w-0">
                        <div className="truncate">{key.name}</div>
                        {/* 只展示前缀，明文密钥服务端也没有保存 */}
                        <div className="text-[11px] text-tertiary font-normal">
                          {key.key_prefix}…
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="py-3 px-4 text-primary">
                    <div className="flex flex-wrap gap-1">
                      {key.hostnames.split(',').map((h) => (
                        <Badge key={h} variant={h === '*' ? 'warning' : 'default'} size="sm">
                          {h === '@' ? domain.name : h === '*' ? `*.${domain.name}` : `${h}.${domain.name}`}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td className="py-3 px-4">{renderAllowlist(key)}</td>
                  <td className="py-3 px-4 text-primary font-semibold">
                    {key.last_ipv4 || key.last_ipv6 ? (
                      <div className="space-y-0.5">
                        {key.last_ipv4 && <div>{key.last_ipv4}</div>}
                        {key.last_ipv6 && (
                          <div className="text-tertiary truncate max-w-[12rem]" title={key.last_ipv6}>
                            {key.last_ipv6}
                          </div>
                        )}
                      </div>
                    ) : (
                      <span className="text-tertiary font-normal">—</span>
                    )}
                  </td>
                  <td className="py-3 px-4 text-tertiary">{key.record_ttl}s</td>
                  <td className="py-3 px-4 text-secondary">
                    {key.last_used_at ? (
                      <div className="space-y-0.5">
                        <div>{new Date(key.last_used_at).toLocaleString()}</div>
                        <div className="text-[11px] text-tertiary">
                          {key.last_client_ip}
                          {key.last_status ? ` · ${key.last_status}` : ''}
                        </div>
                      </div>
                    ) : (
                      <span className="text-tertiary">{t('ddns.never_used')}</span>
                    )}
                  </td>
                  <td className="py-3 px-4">
                    <div className="space-y-1">
                      <Badge variant={key.enabled ? 'success' : 'default'} size="sm">
                        {key.enabled ? t('ddns.enabled') : (isZh ? '已停用' : 'Disabled')}
                      </Badge>
                      {key.reject_count > 0 && (
                        <div
                          className="text-[11px] text-amber-500 flex items-center gap-1"
                          title={isZh ? '存在被拒绝的调用，可能是密钥已泄露或客户端配置有误' : 'Rejected calls detected: the key may be leaking or a client is misconfigured'}
                        >
                          <AlertTriangle className="w-3 h-3 flex-shrink-0" />
                          {key.reject_count} {t('ddns.rejected_hint')}
                        </div>
                      )}
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        type="button"
                        onClick={() => handleOpenEdit(key)}
                        className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                        title={t('common.edit')}
                      >
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleRotate(key)}
                        className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                        title={t('ddns.rotate')}
                      >
                        <RefreshCw className="w-3.5 h-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDelete(key)}
                        className="p-1.5 rounded hover:bg-red-500/10 text-secondary hover:text-red-500 transition-colors cursor-pointer"
                        title={t('common.delete')}
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}

              {keys.length === 0 && !loading && (
                <tr>
                  <td colSpan={8} className="py-8 text-center text-secondary">
                    {t('ddns.empty')}
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

      {/* 新建 / 编辑弹窗 */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editingKey ? t('ddns.edit_key') : t('ddns.add_key')}
        description={domain.name}
        maxWidth="2xl"
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          {formError && (
            <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-xs text-red-500 font-medium">
              {formError}
            </div>
          )}

          <Input
            label={t('ddns.field_name')}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            required
            autoFocus
          />

          <Input
            label={t('ddns.field_hostnames')}
            value={form.hostnames}
            onChange={(e) => setForm({ ...form, hostnames: e.target.value })}
            placeholder="ddns, home, @"
            helper={t('ddns.field_hostnames_helper')}
            required
          />

          <Input
            label={t('ddns.field_allowed_ips')}
            value={form.allowed_ips}
            onChange={(e) => setForm({ ...form, allowed_ips: e.target.value })}
            placeholder="203.0.113.7, 198.51.100.0/24"
            helper={t('ddns.field_allowed_ips_helper')}
          />

          <Input
            label={t('ddns.field_ttl')}
            type="number"
            min={1}
            value={form.record_ttl}
            onChange={(e) => setForm({ ...form, record_ttl: Number(e.target.value) })}
            helper={t('ddns.field_ttl_helper')}
          />

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
            <Switch
              checked={form.allow_ipv4}
              onChange={(v) => setForm({ ...form, allow_ipv4: v })}
              label={t('ddns.allow_ipv4')}
            />
            <Switch
              checked={form.allow_ipv6}
              onChange={(v) => setForm({ ...form, allow_ipv6: v })}
              label={t('ddns.allow_ipv6')}
            />
            <Switch
              checked={form.auto_create}
              onChange={(v) => setForm({ ...form, auto_create: v })}
              label={t('ddns.auto_create')}
            />
            <Switch
              checked={form.enabled}
              onChange={(v) => setForm({ ...form, enabled: v })}
              label={t('ddns.enabled')}
            />
          </div>

          <div className="flex justify-end items-center gap-2 pt-3 border-t border-border">
            <Button type="button" variant="secondary" size="sm" onClick={() => setIsModalOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={formSubmitting}>
              {editingKey ? t('common.save') : t('ddns.add_key')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* 明文密钥一次性展示 */}
      <Modal
        isOpen={issued !== null}
        onClose={() => setIssued(null)}
        title={issued?.title || ''}
        description={domain.name}
        maxWidth="2xl"
      >
        {issued && (
          <div className="space-y-4">
            <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-600 dark:text-amber-500 font-medium flex items-start gap-2">
              <AlertTriangle className="w-4 h-4 flex-shrink-0 mt-0.5" />
              <span>{t('ddns.key_once_warning')}</span>
            </div>

            <CodeBox code={issued.key} />

            <div className="space-y-3 pt-2 border-t border-border">
              <div>
                <div className="text-xs font-semibold text-primary">{t('ddns.client_config')}</div>
                <div className="text-[11px] text-tertiary mt-0.5">{t('ddns.client_config_desc')}</div>
              </div>

              {clientSnippets(issued.key).map((snippet) => (
                <div key={snippet.label} className="space-y-1.5">
                  <div className="text-[11px] text-secondary font-mono">{snippet.label}</div>
                  <CodeBox code={snippet.code} />
                </div>
              ))}
            </div>

            <div className="flex justify-end pt-2 border-t border-border">
              <Button variant="primary" size="sm" onClick={() => setIssued(null)}>
                {isZh ? '我已保存' : 'I have saved it'}
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};
