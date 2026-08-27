import React, { useState, useEffect } from 'react';
import {
  Save,
  Key,
  RotateCw,
  Copy,
  Check,
  Eye,
  EyeOff,
  Lock
} from 'lucide-react';
import { Button, Input } from '../components/GeistUI';
import { api } from '../api/client';
import { User } from '../types';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface SystemSettingsPageProps {
  user: User | null;
  onRefreshUser?: () => void;
}

export const SystemSettingsPage: React.FC<SystemSettingsPageProps> = ({ user, onRefreshUser }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  // System Settings State
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [statusMsg, setStatusMsg] = useState('');

  // API Key State
  const [currentApiKey, setCurrentApiKey] = useState(user?.api_key || '');
  const [showApiKey, setShowApiKey] = useState(false);
  const [copiedKey, setCopiedKey] = useState(false);
  const [regenerating, setRegenerating] = useState(false);

  // Admin Password Change State
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [changingPassword, setChangingPassword] = useState(false);

  const loadAllData = async () => {
    try {
      setLoading(true);
      const settingsRes = await api.getSettings();
      setSettings(settingsRes.settings || {});
    } catch (err: any) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAllData();
    if (user?.api_key) {
      setCurrentApiKey(user.api_key);
    }
  }, [user]);

  const handleChange = (key: string, val: string) => {
    setSettings((prev) => ({ ...prev, [key]: val }));
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatusMsg('');

    try {
      // 仅提交本页管理的 DNS 引擎参数。ACME 相关设置已迁移至「SSL 证书管理」页，
      // 这里不再触碰，避免两处入口互相覆盖。
      await api.updateSettings({
        default_ttl: settings.default_ttl || '300',
        rate_limit: settings.rate_limit || '1000',
      });
      setStatusMsg(t('common.success'));
      setTimeout(() => setStatusMsg(''), 3000);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setSaving(false);
    }
  };

  const handleCopyKey = () => {
    if (currentApiKey) {
      navigator.clipboard.writeText(currentApiKey);
      setCopiedKey(true);
      setTimeout(() => setCopiedKey(false), 2000);
    }
  };

  const handleRegenerateKey = async () => {
    const conf = await confirm({
      variant: 'danger',
      message: isZh
        ? '确定要重新生成全局 API 密钥吗？原 API 密钥将立即失效，所有依赖原密钥的脚本需同步更新。'
        : 'Are you sure you want to regenerate the API key? The existing key will immediately become invalid.',
    });
    if (!conf) return;

    try {
      setRegenerating(true);
      const res = await api.regenerateApiKey();
      setCurrentApiKey(res.api_key);
      if (onRefreshUser) onRefreshUser();
      await alert({
        variant: 'success',
        message: isZh ? 'API 密钥已成功更新！' : 'API Key successfully regenerated!',
      });
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setRegenerating(false);
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (newPassword.length < 6) {
      await alert({
        variant: 'warning',
        message: isZh ? '新密码至少需要 6 位字符。' : 'New password must be at least 6 characters.',
      });
      return;
    }
    if (newPassword !== confirmPassword) {
      await alert({
        variant: 'warning',
        message: isZh ? '两次输入的新密码不一致。' : 'The new passwords do not match.',
      });
      return;
    }

    try {
      setChangingPassword(true);
      await api.changePassword({ old_password: oldPassword, new_password: newPassword });
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
      await alert({
        variant: 'success',
        message: isZh ? '管理员密码已成功修改！' : 'Administrator password changed successfully!',
      });
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setChangingPassword(false);
    }
  };

  return (
    <div className="space-y-8 animate-in fade-in duration-150">
      {statusMsg && (
        <div className="p-3.5 rounded-sm bg-green-500/10 border border-green-500/20 text-xs text-green-500 font-medium">
          {statusMsg}
        </div>
      )}

      {/* 1. Global API Key & Authentication Credentials Card */}
      <div className="geist-card p-6 space-y-5">
        <div>
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Key className="w-4 h-4 text-primary" />
            {isZh ? '全局 API 访问密钥与鉴权' : 'Global API Access Token & Authentication'}
          </h3>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? '使用此 API 密钥可通过 HTTP Header (X-API-Key) 免登录操控全部 DNS 解析、SSL 证书及集群运维。'
              : 'Authenticate external scripts and CI/CD pipelines via HTTP header (X-API-Key) to control DNS, SSL, and nodes.'}
          </p>
        </div>

        <div className="p-4 bg-bg-subtle border border-border rounded-sm space-y-3 font-mono text-xs">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div className="flex items-center gap-2 flex-1 min-w-0">
              <span className="text-tertiary select-none">API Key:</span>
              <span className="text-primary font-bold truncate select-all">
                {showApiKey
                  ? currentApiKey || 'dnscat_live_key_production'
                  : currentApiKey
                  ? currentApiKey.replace(/.(?=.{4})/g, '*')
                  : '********************************'}
              </span>
            </div>

            <div className="flex items-center gap-2 flex-shrink-0">
              <button
                type="button"
                onClick={() => setShowApiKey(!showApiKey)}
                className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                title={showApiKey ? (isZh ? '隐藏' : 'Hide') : (isZh ? '显示' : 'Reveal')}
              >
                {showApiKey ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
              </button>

              <button
                type="button"
                onClick={handleCopyKey}
                className="flex items-center gap-1 px-2.5 py-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
              >
                {copiedKey ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
                <span>{copiedKey ? (isZh ? '已复制' : 'Copied') : (isZh ? '复制密钥' : 'Copy Key')}</span>
              </button>

              <Button
                type="button"
                variant="outline"
                size="sm"
                loading={regenerating}
                onClick={handleRegenerateKey}
                icon={<RotateCw className="w-3.5 h-3.5" />}
              >
                {isZh ? '重新生成密钥' : 'Regenerate'}
              </Button>
            </div>
          </div>
        </div>
      </div>

      {/* Administrator Password Change Card */}
      <div className="geist-card p-6 space-y-5">
        <div>
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Lock className="w-4 h-4 text-primary" />
            {isZh ? '管理员账号密码修改' : 'Administrator Password'}
          </h3>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? `修改当前登录管理员 (${user?.username || 'admin'}) 的登录密码，修改后请使用新密码重新登录。`
              : `Change the login password for the current administrator (${user?.username || 'admin'}).`}
          </p>
        </div>

        <form onSubmit={handleChangePassword} className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Input
              label={isZh ? '当前密码' : 'Current Password'}
              type="password"
              value={oldPassword}
              onChange={(e) => setOldPassword(e.target.value)}
              placeholder="••••••••"
              autoComplete="current-password"
              required
            />
            <Input
              label={isZh ? '新密码' : 'New Password'}
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="••••••••"
              autoComplete="new-password"
              helper={isZh ? '至少 6 位字符' : 'At least 6 characters'}
              required
            />
            <Input
              label={isZh ? '确认新密码' : 'Confirm New Password'}
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="••••••••"
              autoComplete="new-password"
              required
            />
          </div>
          <div className="flex justify-end">
            <Button
              type="submit"
              variant="primary"
              loading={changingPassword}
              icon={<Key className="w-4 h-4" />}
            >
              {isZh ? '修改密码' : 'Change Password'}
            </Button>
          </div>
        </form>
      </div>

      {/* 2. System Engine Defaults Form */}
      <div className="geist-card p-6 space-y-6">
        <form onSubmit={handleSave} className="space-y-6">
          <div>
            <h3 className="text-sm font-semibold text-primary mb-3">
              {isZh ? 'DNS 权威引擎运行参数' : 'DNS Authoritative Engine Defaults'}
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Input
                label={isZh ? '默认记录 TTL (秒)' : 'Default Record TTL (Seconds)'}
                type="number"
                value={settings.default_ttl || '300'}
                onChange={(e) => handleChange('default_ttl', e.target.value)}
                placeholder="300"
              />
              <Input
                label={isZh ? 'RRL 速率限制阈值 (QPS/IP)' : 'Response Rate Limiting (RRL QPS/IP)'}
                type="number"
                value={settings.rate_limit || '1000'}
                onChange={(e) => handleChange('rate_limit', e.target.value)}
                placeholder="1000"
                helper={isZh ? '防止针对 DNS 放大反射攻击的单 IP 频次限制' : 'Mitigates DNS amplification attacks'}
              />
            </div>
          </div>

          <div className="flex justify-end pt-4 border-t border-border">
            <Button
              type="submit"
              variant="primary"
              loading={saving}
              icon={<Save className="w-4 h-4" />}
            >
              {t('settings.save_btn')}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};
