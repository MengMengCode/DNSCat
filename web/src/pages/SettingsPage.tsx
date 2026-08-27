import React, { useState } from 'react';
import { Trash2, AlertTriangle } from 'lucide-react';
import { Domain } from '../types';
import { Button } from '../components/GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface SettingsPageProps {
  domain: Domain;
  onRefreshDomain: () => void;
  onDomainDeleted: () => void;
}

export const SettingsPage: React.FC<SettingsPageProps> = ({
  domain,
  onDomainDeleted,
}) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { alert, prompt } = useDialog();
  const [deleting, setDeleting] = useState(false);

  const handleDeleteDomain = async () => {
    const confirmation = await prompt({
      variant: 'danger',
      title: isZh ? '删除域名区域' : 'Delete Zone',
      message: isZh
        ? `此操作不可撤销。请输入 "${domain.name}" 以确认删除该域名区域：`
        : `This action cannot be undone. Type "${domain.name}" to confirm zone deletion:`,
      placeholder: domain.name,
      confirmText: t('common.delete'),
    });
    if (confirmation === domain.name) {
      try {
        setDeleting(true);
        await api.deleteDomain(domain.id);
        onDomainDeleted();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      } finally {
        setDeleting(false);
      }
    }
  };

  return (
    <div className="space-y-8 animate-in fade-in duration-150">
      {/* Danger Zone */}
      <div className="geist-card p-6 border-red-500/30 space-y-4">
        <div className="flex items-center gap-2 text-red-500 font-semibold text-sm">
          <AlertTriangle className="w-4 h-4" />
          {isZh ? '危险操作区' : 'Danger Zone'}
        </div>
        <p className="text-xs text-secondary">
          {isZh
            ? '删除域名将永久清除其所有 DNS 解析记录、DNSSEC 密钥及源站健康探测配置。'
            : 'Deleting a domain will permanently wipe all its DNS records, DNSSEC keys, and origin health checks.'}
        </p>
        <div className="flex justify-start">
          <Button
            variant="error"
            size="sm"
            loading={deleting}
            onClick={handleDeleteDomain}
            icon={<Trash2 className="w-3.5 h-3.5" />}
          >
            {isZh ? `删除域名区域 (${domain.name})` : `Delete Domain Zone (${domain.name})`}
          </Button>
        </div>
      </div>
    </div>
  );
};
