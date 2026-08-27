import React, { useState } from 'react';
import { ArrowRight, Languages, Shield, Sun, Moon } from 'lucide-react';
import { Button, Input } from '../components/GeistUI';
import { DnsCatLogo } from '../components/Logo';
import { api } from '../api/client';
import { User } from '../types';
import { useI18n } from '../i18n/I18nContext';

interface LoginPageProps {
  onLoginSuccess: (token: string, user: User) => void;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
}

export const LoginPage: React.FC<LoginPageProps> = ({ onLoginSuccess, theme, onToggleTheme }) => {
  const { language, setLanguage, t } = useI18n();
  const isZh = language === 'zh-CN';
  // 登录表单不预填任何凭据：预填默认口令会把它暴露给任何打开登录页的人。
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const toggleLanguage = () => {
    setLanguage(language === 'zh-CN' ? 'en-US' : 'zh-CN');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const res = await api.login({ username, password });
      onLoginSuccess(res.token, res.user);
    } catch (err: any) {
      setError(err.message || (isZh ? '身份认证失败' : 'Authentication failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-bg flex flex-col justify-center items-center p-4 relative select-none">
      {/* Top Right Controls：语言与明暗主题切换 */}
      <div className="absolute top-4 right-4 flex items-center gap-2">
        <button
          onClick={toggleLanguage}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-sm border border-border bg-card text-xs font-mono text-primary hover:border-primary transition-colors cursor-pointer"
        >
          <Languages className="w-3.5 h-3.5 text-secondary" />
          <span>{isZh ? '中文' : 'English'}</span>
        </button>
        <button
          onClick={onToggleTheme}
          title={theme === 'dark' ? t('header.theme_light') : t('header.theme_dark')}
          aria-label={theme === 'dark' ? t('header.theme_light') : t('header.theme_dark')}
          className="p-2 rounded-sm border border-border bg-card text-secondary hover:text-primary hover:border-primary transition-colors cursor-pointer"
        >
          {theme === 'dark' ? <Sun className="w-3.5 h-3.5" /> : <Moon className="w-3.5 h-3.5" />}
        </button>
      </div>

      <div className="w-full max-w-md space-y-8 animate-in fade-in zoom-in-95 duration-200">
        {/* Brand Header */}
        <div className="text-center space-y-3">
          <DnsCatLogo className="w-12 h-12 mx-auto" />
          <h1 className="text-2xl font-bold tracking-tight text-primary font-mono">{t('brand.name')}</h1>
          <p className="text-xs text-secondary max-w-sm mx-auto font-mono">
            {isZh ? '权威 DNS 解析与智能 Anycast 路由引擎' : 'Authoritative DNS & Smart Anycast Routing Engine'}
          </p>
        </div>

        {/* Card Form */}
        <div className="geist-card p-8 shadow-2xl space-y-5">
          <div className="flex items-center gap-2 pb-2 border-b border-border text-xs font-mono font-bold text-primary">
            <Shield className="w-4 h-4 text-cyan-400" />
            <span>{isZh ? '管理员身份认证' : 'Administrator Authentication'}</span>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4 font-mono text-xs">
            {error && (
              <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-xs text-red-500 font-medium">
                {error}
              </div>
            )}

            <Input
              label={isZh ? '管理员账号' : 'Username'}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder={isZh ? '请输入管理员账号' : 'Enter username'}
              autoComplete="username"
              required
              autoFocus
            />

            <Input
              label={isZh ? '登录密码' : 'Password'}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              required
            />

            <Button
              type="submit"
              variant="primary"
              className="w-full mt-2"
              loading={loading}
              icon={<ArrowRight className="w-4 h-4" />}
            >
              {isZh ? '登录控制台' : 'Sign In'}
            </Button>
          </form>

          <div className="pt-3 border-t border-border text-center text-[11px] font-mono text-tertiary">
            {isZh
              ? '如需重置管理员账号密码，请在服务器执行 dnscat 命令行菜单。'
              : 'To reset admin credentials, run "dnscat" CLI menu on the host.'}
          </div>
        </div>
      </div>
    </div>
  );
};
