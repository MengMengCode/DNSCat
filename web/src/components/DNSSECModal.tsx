import React, { useState, useEffect } from 'react';
import {
  ShieldCheck,
  RotateCw,
  Key,
  Loader2,
} from 'lucide-react';
import { Domain, DNSSECResponse } from '../types';
import { Modal, Button, Badge, CodeBox, Switch } from './GeistUI';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from './DialogProvider';

interface DNSSECModalProps {
  domain: Domain | null;
  isOpen: boolean;
  onClose: () => void;
  onRefreshDomains: () => void;
}

export const DNSSECModal: React.FC<DNSSECModalProps> = ({
  domain,
  isOpen,
  onClose,
  onRefreshDomains,
}) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();
  const [data, setData] = useState<DNSSECResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [toggling, setToggling] = useState(false);
  const [rotating, setRotating] = useState(false);

  const loadDNSSEC = async () => {
    if (!domain) return;
    try {
      setLoading(true);
      const res = await api.getDNSSEC(domain.id);
      setData(res);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen && domain) {
      setData(null);
      loadDNSSEC();
    }
  }, [isOpen, domain?.id]);

  const handleToggle = async () => {
    if (!domain) return;
    if (data?.enabled) {
      const ok = await confirm({
        variant: 'danger',
        message: isZh
          ? `确定要关闭 [${domain.name}] 的 DNSSEC 吗？已生成的 KSK / ZSK 密钥将被删除，并需前往注册商删除对应 DS 记录。`
          : `Disable DNSSEC for [${domain.name}]? The generated KSK / ZSK keys will be deleted and you must remove the DS record at your registrar.`,
      });
      if (!ok) return;
    }
    try {
      setToggling(true);
      if (data?.enabled) {
        await api.disableDNSSEC(domain.id);
      } else {
        await api.enableDNSSEC(domain.id);
      }
      await loadDNSSEC();
      onRefreshDomains();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setToggling(false);
    }
  };

  const handleRotate = async () => {
    if (!domain) return;
    const ok = await confirm({
      variant: 'warning',
      message: isZh
        ? '确定要轮换 KSK 与 ZSK 密钥对吗？轮换后需要在域名注册商处更新 DS 记录。'
        : 'Rotate DNSSEC KSK and ZSK keypairs? You will need to update the DS record at your registrar.',
    });
    if (!ok) return;
    try {
      setRotating(true);
      await api.rotateDNSSECKeys(domain.id);
      await loadDNSSEC();
      onRefreshDomains();
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setRotating(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={`DNSSEC · ${domain?.name ?? ''}`}
      maxWidth="2xl"
    >
      <div className="max-h-[68vh] overflow-y-auto -mx-1 px-1 space-y-5">
        {/* Status + Toggle */}
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <ShieldCheck className="w-5 h-5 text-primary flex-shrink-0" />
            <Switch
              checked={!!data?.enabled}
              onChange={handleToggle}
              disabled={loading || toggling}
              label={data?.enabled ? t('common.enabled') : t('common.disabled')}
            />
          </div>

          {data?.enabled && (
            <Button
              size="sm"
              variant="outline"
              loading={rotating}
              onClick={handleRotate}
              icon={<RotateCw className="w-3.5 h-3.5" />}
            >
              {t('dnssec.rotate_btn')}
            </Button>
          )}
        </div>

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center gap-2 py-10 text-xs text-tertiary font-mono">
            <Loader2 className="w-4 h-4 animate-spin" />
            {t('common.loading')}
          </div>
        )}

        {/* Enabled content */}
        {!loading && data?.enabled && (
          <>
            {/* Cryptographic Keyring */}
            <div className="border border-border rounded-sm overflow-hidden">
              <div className="px-4 py-2.5 border-b border-border bg-bg-subtle">
                <h3 className="text-xs font-semibold text-primary font-mono">
                  {isZh ? '密钥环 (KSK / ZSK)' : 'Cryptographic Keyring'}
                </h3>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs font-mono">
                  <thead className="bg-bg-subtle/60 border-b border-border text-secondary select-none">
                    <tr>
                      <th className="py-2.5 px-3 font-medium">{isZh ? '密钥角色' : 'Key Role'}</th>
                      <th className="py-2.5 px-3 font-medium">Key Tag</th>
                      <th className="py-2.5 px-3 font-medium">{isZh ? '算法' : 'Algorithm'}</th>
                      <th className="py-2.5 px-3 font-medium">Flags</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {data.ksk && (
                      <tr className="hover:bg-bg-subtle/50 transition-colors">
                        <td className="py-2.5 px-3 font-bold text-primary">
                          <div className="flex items-center gap-2">
                            <Key className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
                            <span>KSK</span>
                          </div>
                        </td>
                        <td className="py-2.5 px-3">
                          <Badge variant="default" size="sm">{data.ksk.key_tag}</Badge>
                        </td>
                        <td className="py-2.5 px-3 text-secondary">13 (ECDSA P-256)</td>
                        <td className="py-2.5 px-3 text-tertiary">257 (SEP)</td>
                      </tr>
                    )}
                    {data.zsk && (
                      <tr className="hover:bg-bg-subtle/50 transition-colors">
                        <td className="py-2.5 px-3 font-bold text-primary">
                          <div className="flex items-center gap-2">
                            <Key className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
                            <span>ZSK</span>
                          </div>
                        </td>
                        <td className="py-2.5 px-3">
                          <Badge variant="default" size="sm">{data.zsk.key_tag}</Badge>
                        </td>
                        <td className="py-2.5 px-3 text-secondary">13 (ECDSA P-256)</td>
                        <td className="py-2.5 px-3 text-tertiary">256 (ZSK)</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            {/* DS Record Setup */}
            <div className="space-y-3">
              <h3 className="text-xs font-semibold text-primary font-mono">{t('dnssec.guide_title')}</h3>
              <p className="text-xs text-secondary">{t('dnssec.guide_desc')}</p>

              <div className="border border-border rounded-sm overflow-hidden">
                <table className="w-full text-left text-xs font-mono">
                  <tbody className="divide-y divide-border">
                    <tr>
                      <td className="py-2.5 px-3 font-semibold text-secondary w-32">Key Tag</td>
                      <td className="py-2.5 px-3 font-bold text-primary select-all">{data.ksk?.key_tag}</td>
                    </tr>
                    <tr>
                      <td className="py-2.5 px-3 font-semibold text-secondary">{isZh ? '算法' : 'Algorithm'}</td>
                      <td className="py-2.5 px-3 font-bold text-primary">13 (ECDSA P-256 / SHA-256)</td>
                    </tr>
                    <tr>
                      <td className="py-2.5 px-3 font-semibold text-secondary">Digest Type</td>
                      <td className="py-2.5 px-3 font-bold text-primary">2 (SHA-256)</td>
                    </tr>
                    <tr>
                      <td className="py-2.5 px-3 font-semibold text-secondary align-top">Digest</td>
                      <td className="py-2.5 px-3 font-bold text-primary select-all break-all">{data.ksk?.digest}</td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <div>
                <label className="text-xs font-medium text-secondary mb-1.5 block">
                  {isZh ? '完整 DS 记录 (BIND 格式)' : 'Full DS Record (BIND Format)'}
                </label>
                <CodeBox code={data.ksk?.ds_config || ''} />
              </div>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
};
