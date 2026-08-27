import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Server,
  Plus,
  Eye,
  Trash2
} from 'lucide-react';
import { ClusterNode } from '../types';
import { Button, Badge, Modal, Input, CodeBox } from '../components/GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { formatCompact, formatFull } from '../lib/format';
import { useDialog } from '../components/DialogProvider';

export const NodesPage: React.FC = () => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loading, setLoading] = useState(true);

  // Modals
  const [isAddModalOpen, setIsAddModalOpen] = useState(false);
  const [selectedNodeScript, setSelectedNodeScript] = useState<string | null>(null);

  // Form State
  // 不预填示例值：预填内容会被误提交，产生名不副实的节点记录。
  const [formName, setFormName] = useState('');
  const [formIP, setFormIP] = useState('');
  const [formRegion, setFormRegion] = useState('');
  const [creating, setCreating] = useState(false);

  const loadNodes = async (silent = false) => {
    try {
      if (!silent) setLoading(true);
      const res = await api.listNodes();
      setNodes(res.nodes || []);
    } catch (err) {
      console.error(err);
    } finally {
      if (!silent) setLoading(false);
    }
  };

  useEffect(() => {
    loadNodes();
    // 边缘节点性能参数每 3 秒静默刷新一次，让集群表格接近实时（不显示 loading 避免闪烁）。
    const timer = setInterval(() => loadNodes(true), 3000);
    return () => clearInterval(timer);
  }, []);

  const handleCreateNode = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);

    try {
      const created = await api.createNode({
        name: formName,
        ip: formIP,
        region: formRegion,
      });

      const scriptRes = await api.getInstallScript(created.node_id);
      setIsAddModalOpen(false);
      setSelectedNodeScript(scriptRes.script);
      loadNodes();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setCreating(false);
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
        await api.deleteNode(id);
        loadNodes();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      }
    }
  };

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.nodes')}</h2>
        <Button
          size="sm"
          variant="primary"
          onClick={() => setIsAddModalOpen(true)}
          icon={<Plus className="w-3.5 h-3.5" />}
        >
          {t('nodes.add_node')}
        </Button>
      </div>

      {/* Nodes Data Table */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{isZh ? '节点名称与 ID' : 'Node Name & ID'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '公网 IP' : 'Public IP'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '地区' : 'Region'}</th>
                <th className="py-3 px-4 font-medium">{t('common.status')}</th>
                <th className="py-3 px-4 font-medium">CPU</th>
                <th className="py-3 px-4 font-medium">{isZh ? '内存' : 'Memory'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '查询总数' : 'Total Queries'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '实时 QPS' : 'Live QPS'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '延迟' : 'Latency'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '最近心跳' : 'Last Heartbeat'}</th>
                <th className="py-3 px-4 font-medium text-right">{t('common.action')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {nodes.map((node) => (
                <tr key={node.id} className="hover:bg-bg-subtle/50 transition-colors">
                  <td className="py-3 px-4 text-primary font-bold">
                    <div className="flex items-center gap-2">
                      <Server className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
                      <div>
                        <span>{node.name}</span>
                        <span className="text-tertiary text-[11px] block">{node.node_id}</span>
                      </div>
                    </div>
                  </td>
                  <td className="py-3 px-4 text-secondary">{node.ip}</td>
                  <td className="py-3 px-4">
                    <Badge variant="default" size="sm">
                      {node.region}
                    </Badge>
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant={node.is_online ? 'success' : 'default'} size="sm">
                      {node.is_online ? t('common.active') : t('common.pending')}
                    </Badge>
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold">
                    {(node.cpu_usage || 0).toFixed(1)}%
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold">
                    {(node.memory_usage || 0).toFixed(1)}%
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold" title={formatFull(node.total_queries || 0)}>
                    {formatCompact(node.total_queries || 0)}
                  </td>
                  <td className="py-3 px-4 text-primary font-semibold" title={formatFull(node.qps || 0)}>
                    {formatCompact(node.qps || 0)}
                  </td>
                  <td className="py-3 px-4">
                    <span className="text-green-500 font-semibold">
                      {node.latency_ms > 0 ? `${node.latency_ms}ms` : '0ms'}
                    </span>
                  </td>
                  <td className="py-3 px-4 text-tertiary">
                    {node.last_heartbeat ? new Date(node.last_heartbeat).toLocaleTimeString() : (isZh ? '待机' : 'Standby')}
                  </td>
                  <td className="py-3 px-4 text-right space-x-1">
                    <button
                      onClick={() => navigate(`/nodes/${node.node_id}`)}
                      className="p-1.5 rounded hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer"
                      title={isZh ? '查看节点状态与性能' : 'View node status & performance'}
                      aria-label={isZh ? '查看节点状态与性能' : 'View node status & performance'}
                    >
                      <Eye className="w-3.5 h-3.5" />
                    </button>
                    <button
                      onClick={() => handleDelete(node.id)}
                      className="p-1.5 rounded hover:bg-red-500/10 text-secondary hover:text-red-500 transition-colors cursor-pointer"
                      title={t('common.delete')}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </td>
                </tr>
              ))}

              {nodes.length === 0 && !loading && (
                <tr>
                  <td colSpan={11} className="py-8 text-center text-secondary">
                    {isZh
                      ? `暂无边缘节点接入，点击“${t('nodes.add_node')}”部署 Anycast 节点。`
                      : `No edge PoPs connected. Click "${t('nodes.add_node')}" to deploy an Anycast node.`}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Add Node Modal */}
      <Modal
        isOpen={isAddModalOpen}
        onClose={() => setIsAddModalOpen(false)}
        title={t('nodes.add_node')}
        description={isZh ? '注册一个边缘节点以扩展您的 Anycast 集群' : 'Register an edge node to expand your Anycast cluster'}
      >
        <form onSubmit={handleCreateNode} className="space-y-4">
          <Input
            label={isZh ? '节点名称' : 'Node Name'}
            value={formName}
            onChange={(e) => setFormName(e.target.value)}
            placeholder={isZh ? '如：Edge PoP (Frankfurt)' : 'e.g. Edge PoP (Frankfurt)'}
            required
          />

          <div className="grid grid-cols-2 gap-3">
            <Input
              label={isZh ? '公网 IP' : 'Public IP'}
              value={formIP}
              onChange={(e) => setFormIP(e.target.value)}
              placeholder="203.0.113.20"
              required
            />
            <Input
              label={isZh ? '地区 / 数据中心' : 'Region / Datacenter'}
              value={formRegion}
              onChange={(e) => setFormRegion(e.target.value)}
              placeholder={isZh ? '如：EU-Central' : 'e.g. EU-Central'}
              required
            />
          </div>

          <div className="flex justify-end items-center gap-2 pt-3 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsAddModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={creating}>
              {t('nodes.add_node')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* View Deploy Script Modal */}
      <Modal
        isOpen={selectedNodeScript !== null}
        onClose={() => setSelectedNodeScript(null)}
        title={t('nodes.deploy_cmd')}
        description={isZh ? '在您的 VPS / 边缘服务器上运行此命令以接入集群' : 'Run this command on your VPS / edge server to connect to this cluster'}
        maxWidth="lg"
      >
        <div className="space-y-4">
          <CodeBox code={selectedNodeScript || ''} />
          <div className="flex justify-end pt-2">
            <Button variant="primary" size="sm" onClick={() => setSelectedNodeScript(null)}>
              {t('common.cancel')}
            </Button>
          </div>
        </div>
      </Modal>

    </div>
  );
};
