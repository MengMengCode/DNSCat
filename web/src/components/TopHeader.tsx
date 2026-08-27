import React from 'react';
import { Link } from 'react-router-dom';
import {
  Sun,
  Moon,
  Languages,
  ChevronRight,
  Menu
} from 'lucide-react';
import { DnsCatLogo } from './Logo';
import { Domain } from '../types';
import { TabType } from './NavTabs';
import { useI18n } from '../i18n/I18nContext';

interface TopHeaderProps {
  selectedDomain: Domain | null;
  activeTab: TabType;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
  onSelectDomain: (d: Domain | null) => void;
  onSelectTab: (tab: TabType) => void;
  // 移动端点击汉堡按钮打开侧栏抽屉（宽屏隐藏该按钮）。
  onOpenMobileNav?: () => void;
}

// 与 Sidebar.tsx / NavTabs.tsx 保持一致的“页签 -> 路径”映射，
// 供面包屑各级点击跳转使用。
const DOMAIN_TABS: TabType[] = [
  'dashboard',
  'records',
  'ddns',
  'health',
  'certificates',
  'security',
  'access_control',
  'security_logs',
  'settings',
];

// 与 App.tsx 保持一致：面包屑中的域名一级指向域名仪表盘。
const DOMAIN_DEFAULT_TAB: TabType = 'dashboard';

const pathForTab = (tab: TabType, domainId?: number | string): string => {
  if (domainId !== undefined && DOMAIN_TABS.includes(tab)) {
    return `/domains/${domainId}/${tab}`;
  }
  switch (tab) {
    case 'overview':
      return '/overview';
    case 'domains':
      return '/domains';
    case 'certificates':
      return '/certificates';
    case 'nodes':
      return '/nodes';
    case 'node_monitor':
      return '/node-monitor';
    case 'nameservers':
      return '/nameservers';
    case 'routing_lines':
      return '/routing-lines';
    case 'analytics':
      return '/analytics';
    case 'global_settings':
      return '/global-settings';
    default:
      return '/overview';
  }
};

export const TopHeader: React.FC<TopHeaderProps> = ({
  selectedDomain,
  activeTab,
  theme,
  onToggleTheme,
  onSelectDomain,
  onSelectTab,
  onOpenMobileNav,
}) => {
  const { language, setLanguage, t } = useI18n();

  const getTabTitle = () => {
    switch (activeTab) {
      case 'overview': return t('nav.overview');
      case 'domains': return t('nav.domains_mgmt');
      case 'dashboard': return t('nav.domain_dashboard');
      case 'records': return t('nav.records');
      case 'ddns': return t('nav.ddns');
      case 'health': return t('nav.health');
      case 'security': return t('nav.security');
      case 'security_logs': return t('nav.security_logs');
      case 'access_control': return t('nav.access_control');
      case 'certificates': return t('nav.certificates');
      case 'nodes': return t('nav.nodes');
      case 'node_monitor': return t('nav.node_monitor');
      case 'nameservers': return t('nav.nameservers');
      case 'routing_lines': return t('nav.routing_lines');
      case 'analytics': return t('nav.analytics');
      case 'settings': return t('nav.settings');
      case 'global_settings': return t('nav.global_settings');
      default: return '';
    }
  };

  const toggleLanguage = () => {
    const next = language === 'zh-CN' ? 'en-US' : 'zh-CN';
    setLanguage(next);
  };

  return (
    <header className="sticky top-0 z-40 w-full bg-card/80 backdrop-blur-md border-b border-border">
      <div className="max-w-[1400px] mx-auto px-4 sm:px-6 h-16 flex items-center justify-between gap-3">
        {/* Left: Mobile nav toggle + Breadcrumbs (Clickable for quick navigation) */}
        <div className="flex items-center gap-2 text-xs font-mono min-w-0">
          {/* 汉堡按钮：仅移动端显示，打开侧栏抽屉 */}
          <button
            type="button"
            onClick={onOpenMobileNav}
            aria-label={language === 'zh-CN' ? '打开菜单' : 'Open menu'}
            className="lg:hidden -ml-1 p-1.5 rounded-sm border border-transparent hover:border-border hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer flex-shrink-0"
          >
            <Menu className="w-4 h-4" />
          </button>
          <Link
            to="/overview"
            onClick={() => {
              onSelectDomain(null);
              onSelectTab('overview');
            }}
            className="text-secondary hover:text-primary transition-colors hidden sm:flex items-center gap-1.5 cursor-pointer shrink-0"
          >
            <DnsCatLogo className="w-4 h-4 text-primary flex-shrink-0" />
            {t('brand.name')}
          </Link>
          <ChevronRight className="w-3 h-3 text-tertiary shrink-0 hidden sm:block" />
          <Link
            to={
              selectedDomain
                ? pathForTab(DOMAIN_DEFAULT_TAB, selectedDomain.id)
                : pathForTab('overview')
            }
            onClick={() => {
              if (selectedDomain) {
                onSelectTab(DOMAIN_DEFAULT_TAB);
              } else {
                onSelectDomain(null);
                onSelectTab('overview');
              }
            }}
            className="text-primary font-bold truncate hover:opacity-80 transition-opacity cursor-pointer"
          >
            {/* 域名清单还在路上时 name 为空，用骨架条占位，
                避免面包屑先显示「全局面板」再跳成域名造成闪跳。 */}
            {selectedDomain ? (
              selectedDomain.name || (
                <span className="inline-block h-3 w-28 bg-bg-subtle rounded-sm animate-pulse align-middle" />
              )
            ) : (
              t('header.global_context')
            )}
          </Link>
          <ChevronRight className="w-3 h-3 text-tertiary shrink-0" />
          <Link
            to={pathForTab(activeTab, selectedDomain?.id)}
            onClick={() => onSelectTab(activeTab)}
            className="text-secondary font-medium truncate hover:text-primary transition-colors cursor-pointer"
          >
            {getTabTitle()}
          </Link>
        </div>

        {/* Right: Switchers */}
        <div className="flex items-center gap-2.5 shrink-0">
          {/* Language Switcher Button (No flags, clean text) */}
          <button
            onClick={toggleLanguage}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm border border-border bg-bg-subtle hover:border-border-hover text-xs font-medium text-primary transition-colors cursor-pointer"
            title="Switch Language / 切换语言"
          >
            <Languages className="w-3.5 h-3.5 text-secondary" />
            <span className="font-mono text-[11px]">
              {language === 'zh-CN' ? '中文' : 'English'}
            </span>
          </button>

          {/* Theme Toggle Button */}
          <button
            onClick={onToggleTheme}
            className="p-1.5 rounded-sm border border-border bg-bg-subtle hover:border-border-hover text-secondary hover:text-primary transition-colors cursor-pointer"
            title={theme === 'dark' ? t('header.theme_light') : t('header.theme_dark')}
          >
            {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
          </button>
        </div>
      </div>
    </header>
  );
};
