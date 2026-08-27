import React, { useState, useEffect } from 'react';
import { Server, Plus, Trash2, Edit3, Network } from 'lucide-react';
import { ClusterNode, Nameserver } from '../types';
import { Button, Badge, Modal, Input } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

// 权威 Nameserver 服务器管理：维护全网托管域名所指向的权威 NS 主机名，
// 以及它们与主控 / 边缘集群节点的绑定关系（Glue A/AAAA 胶水记录）。
export const NameserversPage: React.FC = () => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  const [nameservers, setNameservers] = useState<Nameserver[]>([]);
  // 节点列表用于「绑定集群节点」下拉选择，以及表格中展示绑定节点名称。
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loading, setLoading] = useState(true);

  const [isNsModalOpen, setIsNsModalOpen] = useState(false);
  const [editingNs, setEditingNs] = useState<Nameserver | null>(null);
  const [nsHostname, setNsHostname] = useState('');
  const [nsNodeId, setNsNodeId] = useState<string>('');
  const [nsIPv4, setNsIPv4] = useState('');
  const [nsIPv6, setNsIPv6] = useState('');
  const [nsIsActive, setNsIsActive] = useState(true);
  const [nsSubmitting, setNsSubmitting] = useState(false);

  const loadData = async () => {
    try {
      setLoading(true);
      const [nsRes, nodesRes] = await Promise.all([api.listNameservers(), api.listNodes()]);
      setNameservers(nsRes.nameservers || []);
      setNodes(nodesRes.nodes || []);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleOpenAdd = () => {
    setEditingNs(null);
    // 不预填任何主机名/IP：各部署的权威 NS 与出口地址都不同，预填值极易被误提交。
    setNsHostname('');
    setNsNodeId('');
    setNsIPv4('');
    setNsIPv6('');
    setNsIsActive(true);
    setIsNsModalOpen(true);
  };

  const handleOpenEdit = (ns: Nameserver) => {
    setEditingNs(ns);
    setNsHostname(ns.hostname);
    setNsNodeId(ns.node_id ? String(ns.node_id) : '');
    setNsIPv4(ns.ipv4 || '');
    setNsIPv6(ns.ipv6 || '');
    setNsIsActive(ns.is_active);
    setIsNsModalOpen(true);
  };

  // 选择绑定节点时自动带出该节点的 IP，减少手填 Glue 记录出错。
  const handleNodeSelect = (nId: string) => {
    setNsNodeId(nId);
    if (nId === 'master') {
      // 控制台由主控自身提供服务，因此当前访问地址就是主控地址；
      // 仅在它是 IPv4 字面量时自动带出，避免把域名写进 Glue 记录。
      const host = window.location.hostname;
      setNsIPv4(/^\d{1,3}(\.\d{1,3}){3}$/.test(host) ? host : '');
    } else if (nId) {
      const selectedNode = nodes.find((n) => String(n.id) === nId);
      if (selectedNode && selectedNode.ip) {
        setNsIPv4(selectedNode.ip);
      }
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nsHostname.trim()) {
      await alert({
        variant: 'warning',
        message: isZh ? '请输入 Nameserver 主机名' : 'Please input nameserver hostname',
      });
      return;
    }

    try {
      setNsSubmitting(true);
      const parsedNodeId = nsNodeId && nsNodeId !== 'master' ? Number(nsNodeId) : null;
      const payload: Partial<Nameserver> = {
        hostname: nsHostname.trim(),
        node_id: parsedNodeId,
        ipv4: nsIPv4.trim(),
        ipv6: nsIPv6.trim(),
        is_active: nsIsActive,
      };

      if (editingNs) {
        await api.updateNameserver(editingNs.id, payload);
      } else {
        await api.createNameserver(payload);
      }

      setIsNsModalOpen(false);
      await loadData();
    } catch (err: any) {
      await alert({
        variant: 'danger',
        message: err.message || (isZh ? '保存 Nameserver 失败' : 'Failed to save nameserver'),
      });
    } finally {
      setNsSubmitting(false);
    }
  };

  const handleDelete = async (id: number, hostname: string) => {
    const conf = await confirm({
      variant: 'danger',
      message: isZh
        ? `确定要删除权威 Nameserver [${hostname}] 吗？`
        : `Are you sure you want to delete nameserver [${hostname}]?`,
      confirmText: t('common.delete'),
    });
    if (!conf) return;

    try {
      await api.deleteNameserver(id);
      await loadData();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  // 权威 NS 列表已全量加载到前端，这里做客户端分页，配置较多时也能翻页查看。
  const pager = usePagination(nameservers, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.nameservers')}</h2>
        <Button
          size="sm"
          variant="primary"
          onClick={handleOpenAdd}
          icon={<Plus className="w-3.5 h-3.5" />}
        >
          {isZh ? '添加权威 NS 服务器' : 'Add Nameserver'}
        </Button>
      </div>

      {/* Nameservers Table */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{isZh ? '权威 NS 域名' : 'NS Hostname'}</th>
                <th className="py-3 px-4 font-medium">
                  {isZh ? '绑定集群节点 / 角色' : 'Bound Node / Role'}
                </th>
                <th className="py-3 px-4 font-medium">
                  {isZh ? 'IPv4 Glue (A 记录)' : 'IPv4 Glue (A)'}
                </th>
                <th className="py-3 px-4 font-medium">
                  {isZh ? 'IPv6 Glue (AAAA 记录)' : 'IPv6 Glue (AAAA)'}
                </th>
                <th className="py-3 px-4 font-medium">{isZh ? '状态' : 'Status'}</th>
                <th className="py-3 px-4 font-medium text-right">{isZh ? '操作' : 'Actions'}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((ns) => {
                const boundNode = ns.node || nodes.find((n) => n.id === ns.node_id);
                return (
                  <tr key={ns.id} className="hover:bg-bg-subtle/50 transition-colors">
                    <td className="py-3 px-4 font-bold text-primary">{ns.hostname}</td>
                    <td className="py-3 px-4 text-secondary">
                      {boundNode ? (
                        <div className="flex items-center gap-1.5">
                          <Network className="w-3.5 h-3.5 text-blue-400" />
                          <span className="text-primary font-semibold">{boundNode.name}</span>
                          <span className="text-tertiary">({boundNode.region})</span>
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5">
                          <Server className="w-3.5 h-3.5 text-emerald-400" />
                          <span className="text-primary">
                            {isZh ? '主控节点' : 'Master Anycast Node'}
                          </span>
                        </div>
                      )}
                    </td>
                    <td className="py-3 px-4 text-primary">
                      {ns.ipv4 || <span className="text-tertiary">-</span>}
                    </td>
                    <td className="py-3 px-4 text-secondary">
                      {ns.ipv6 || <span className="text-tertiary">-</span>}
                    </td>
                    <td className="py-3 px-4">
                      {ns.is_active ? (
                        <Badge variant="success" size="sm">
                          {isZh ? '活跃就绪' : 'Active'}
                        </Badge>
                      ) : (
                        <Badge variant="default" size="sm">
                          {isZh ? '停用' : 'Disabled'}
                        </Badge>
                      )}
                    </td>
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <button
                          onClick={() => handleOpenEdit(ns)}
                          className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                          title={isZh ? '编辑' : 'Edit'}
                        >
                          <Edit3 className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDelete(ns.id, ns.hostname)}
                          className="p-1.5 rounded hover:bg-red-500/10 border border-border text-secondary hover:text-red-500 transition-colors cursor-pointer"
                          title={isZh ? '删除' : 'Delete'}
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}

              {nameservers.length === 0 && !loading && (
                <tr>
                  <td colSpan={6} className="text-center text-secondary py-8">
                    {isZh
                      ? '暂无配置的权威 NS 服务器。'
                      : 'No authoritative nameservers configured.'}
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
        isOpen={isNsModalOpen}
        onClose={() => setIsNsModalOpen(false)}
        title={
          editingNs
            ? isZh
              ? '编辑权威 Nameserver'
              : 'Edit Nameserver'
            : isZh
              ? '添加权威 Nameserver 服务器'
              : 'Add Nameserver'
        }
      >
        <form onSubmit={handleSave} className="space-y-4 font-mono text-xs">
          <Input
            label={isZh ? '权威 NS 主机名' : 'NS Hostname'}
            value={nsHostname}
            onChange={(e) => setNsHostname(e.target.value)}
            placeholder="ns1.example.com"
            required
            autoFocus
          />

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-secondary block">
              {isZh ? '绑定集群节点 / 角色' : 'Bound Node / Cluster Role'}
            </label>
            <select
              value={nsNodeId}
              onChange={(e) => handleNodeSelect(e.target.value)}
              className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
            >
              <option value="master">
                {isZh ? '主控节点' : 'Master Server'}
              </option>
              {nodes.map((node) => (
                <option key={node.id} value={node.id}>
                  {node.name} ({node.ip} - {node.region})
                </option>
              ))}
            </select>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input
              label={isZh ? 'IPv4 Glue (A 胶水记录)' : 'IPv4 Glue (A Record)'}
              value={nsIPv4}
              onChange={(e) => setNsIPv4(e.target.value)}
              placeholder="203.0.113.10"
            />
            <Input
              label={isZh ? 'IPv6 Glue (AAAA 胶水记录)' : 'IPv6 Glue (AAAA Record)'}
              value={nsIPv6}
              onChange={(e) => setNsIPv6(e.target.value)}
              placeholder="2001:db8::1"
            />
          </div>

          <div className="flex items-center gap-2 pt-2">
            <input
              type="checkbox"
              id="ns_active"
              checked={nsIsActive}
              onChange={(e) => setNsIsActive(e.target.checked)}
              className="rounded border-border text-primary focus:ring-0"
            />
            <label htmlFor="ns_active" className="text-xs text-primary font-medium cursor-pointer">
              {isZh
                ? '启用此权威 NS 服务器并加入域名默认解析列表'
                : 'Enable nameserver and include in default zone delegations'}
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsNsModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={nsSubmitting}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
