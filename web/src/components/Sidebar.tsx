import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Globe,
  Layers,
  LayoutGrid,
  ListOrdered,
  Share2,
  HeartPulse,
  Lock,
  Server,
  Activity,
  Network,
  Sliders,
  Shield,
  Ban,
  FileText,
  Cog,
  BookOpen,
  ChevronDown,
  LogOut,
  Search,
  ArrowLeft,
  RefreshCw,
  X,
} from 'lucide-react';
import { DnsCatLogo } from './Logo';
import { Domain, User } from '../types';
import { TabType } from './NavTabs';
import { useI18n } from '../i18n/I18nContext';

interface SidebarProps {
  user: User | null;
  domains: Domain[];
  selectedDomain: Domain | null;
  activeTab: TabType;
  onSelectDomain: (d: Domain | null) => void;
  onSelectTab: (tab: TabType) => void;
  onOpenAddDomain: () => void;
  onLogout: () => void;
  // 移动端抽屉状态：由 App 统一管理，宽屏（lg+）下忽略。
  mobileOpen?: boolean;
  onMobileClose?: () => void;
}

// 是否为桌面视口（lg = 1024px）。折叠成图标栏是桌面专属行为，
// 移动端抽屉始终以完整宽度展示，因此需要用它把两种形态区分开。
const useIsDesktop = (): boolean => {
  const query = '(min-width: 1024px)';
  const [isDesktop, setIsDesktop] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : true
  );

  useEffect(() => {
    const mq = window.matchMedia(query);
    const sync = () => setIsDesktop(mq.matches);
    sync();
    mq.addEventListener('change', sync);
    return () => mq.removeEventListener('change', sync);
  }, []);

  return isDesktop;
};

const DOMAIN_TABS: TabType[] = [
  'dashboard',
  'records',
  'ddns',
  'health',
  'security',
  'access_control',
  'security_logs',
  'settings',
];

// 与 App.tsx 保持一致：选中域名后默认进入域名仪表盘，而不是 DNS 解析记录页。
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
    case 'api_docs':
      return '/api-docs';
    default:
      return '/overview';
  }
};

export const Sidebar: React.FC<SidebarProps> = ({
  user,
  domains,
  selectedDomain,
  activeTab,
  onSelectDomain,
  onSelectTab,
  onOpenAddDomain,
  onLogout,
  mobileOpen = false,
  onMobileClose,
}) => {
  const { t, language } = useI18n();
  const isDesktop = useIsDesktop();
  const [isDomainDropdownOpen, setIsDomainDropdownOpen] = useState(false);
  const [domainSearch, setDomainSearch] = useState('');
  const [isCollapsed, setIsCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem('dnscat_sidebar_collapsed') === 'true';
    } catch {
      return false;
    }
  });

  const isZh = language === 'zh-CN';
  // 折叠图标栏只在桌面生效；移动端抽屉永远展示完整内容。
  const collapsed = isDesktop && isCollapsed;

  const handleToggleSidebar = () => {
    const next = !isCollapsed;
    setIsCollapsed(next);
    setIsDomainDropdownOpen(false);
    try {
      window.localStorage.setItem('dnscat_sidebar_collapsed', String(next));
    } catch {
      // Ignore storage failures; the in-memory state still works.
    }
  };

  const filteredDomains = domains.filter((d) =>
    d.name.toLowerCase().includes(domainSearch.toLowerCase())
  );

  // 1. Global Navigation Groups (Shown when NO domain is selected)
  const globalNavGroups = [
    {
      group: isZh ? 'DNS 解析与域名' : 'DNS & Domains',
      items: [
        { id: 'overview' as TabType, label: t('nav.overview'), icon: <Layers className="w-4 h-4" /> },
        { id: 'domains' as TabType, label: t('nav.domains_mgmt'), icon: <LayoutGrid className="w-4 h-4" /> },
        { id: 'certificates' as TabType, label: t('nav.certificates'), icon: <Lock className="w-4 h-4" /> },
      ],
    },
    {
      group: isZh ? '集群与边缘节点' : 'Cluster Nodes',
      items: [
        { id: 'nodes' as TabType, label: t('nav.nodes'), icon: <Server className="w-4 h-4" /> },
        { id: 'node_monitor' as TabType, label: t('nav.node_monitor'), icon: <Activity className="w-4 h-4" /> },
        { id: 'nameservers' as TabType, label: t('nav.nameservers'), icon: <Network className="w-4 h-4" /> },
      ],
    },
    {
      group: isZh ? '流量调度' : 'Traffic Routing',
      items: [
        { id: 'routing_lines' as TabType, label: t('nav.routing_lines'), icon: <Share2 className="w-4 h-4" /> },
      ],
    },
    {
      group: isZh ? '系统配置' : 'System Settings',
      items: [
        { id: 'global_settings' as TabType, label: t('nav.global_settings'), icon: <Cog className="w-4 h-4" /> },
        { id: 'api_docs' as TabType, label: t('nav.api_docs'), icon: <BookOpen className="w-4 h-4" /> },
      ],
    },
  ];

  // 2. Domain-Specific Navigation Groups (Shown ONLY when a domain is selected)
  const domainNavGroups = [
    {
      group: isZh ? '解析与流量控制' : 'DNS & Traffic Control',
      items: [
        { id: 'dashboard' as TabType, label: t('nav.domain_dashboard'), icon: <Layers className="w-4 h-4" /> },
        { id: 'records' as TabType, label: t('nav.records'), icon: <ListOrdered className="w-4 h-4" /> },
        { id: 'ddns' as TabType, label: t('nav.ddns'), icon: <RefreshCw className="w-4 h-4" /> },
        { id: 'health' as TabType, label: t('nav.health'), icon: <HeartPulse className="w-4 h-4" /> },
      ],
    },
    {
      group: isZh ? '安全防护' : 'Security',
      items: [
        { id: 'security_logs' as TabType, label: t('nav.security_logs'), icon: <FileText className="w-4 h-4" /> },
        { id: 'security' as TabType, label: t('nav.security'), icon: <Shield className="w-4 h-4" /> },
        { id: 'access_control' as TabType, label: t('nav.access_control'), icon: <Ban className="w-4 h-4" /> },
      ],
    },
    {
      group: isZh ? '区域配置与维护' : 'Zone Configuration',
      items: [
        { id: 'settings' as TabType, label: t('nav.settings'), icon: <Sliders className="w-4 h-4" /> },
      ],
    },
  ];

  const currentGroups = selectedDomain ? domainNavGroups : globalNavGroups;

  return (
    <>
      {/* 移动端抽屉背景遮罩：点击关闭；宽屏（lg+）隐藏。 */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm lg:hidden"
          onClick={onMobileClose}
          aria-hidden="true"
        />
      )}

      <aside
        className={`fixed inset-y-0 left-0 z-50 w-64 ${mobileOpen ? 'translate-x-0 shadow-2xl' : '-translate-x-full'} lg:static lg:z-30 lg:translate-x-0 lg:shadow-none ${collapsed ? 'lg:w-16' : 'lg:w-64'} bg-card border-r border-border h-screen flex flex-col flex-shrink-0 select-none transition-[transform,width] duration-200 ease-out`}
      >
        {/* Brand Header & Sidebar Toggle */}
        {/* 高度与 TopHeader 的 h-16 保持一致，确保两者的下边框在同一条水平线上 */}
        <div
          className={`${collapsed ? 'px-3 justify-center' : 'px-4 justify-between'} h-16 flex-shrink-0 border-b border-border flex items-center gap-2`}
        >
          {!collapsed && (
            <Link
              to="/overview"
              onClick={() => {
                onSelectDomain(null);
                onSelectTab('overview');
              }}
              className="flex items-center gap-2.5 min-w-0 cursor-pointer hover:opacity-80 transition-opacity"
            >
              <DnsCatLogo className="w-5 h-5 flex-shrink-0 text-primary" />
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="font-bold text-sm text-primary tracking-tight">{t('brand.name')}</span>
                </div>
                <div className="text-[10px] text-tertiary font-mono uppercase tracking-wider whitespace-nowrap">Authoritative DNS</div>
              </div>
            </Link>
          )}

          {/* 移动端：关闭抽屉（宽屏隐藏） */}
          <button
            type="button"
            onClick={onMobileClose}
            aria-label={isZh ? '关闭菜单' : 'Close menu'}
            title={isZh ? '关闭菜单' : 'Close menu'}
            className="lg:hidden p-1.5 rounded-sm border border-transparent hover:border-border hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer flex-shrink-0"
          >
            <X className="w-4 h-4" />
          </button>

          {/* 桌面：折叠 / 展开图标栏（移动端隐藏） */}
          <button
            type="button"
            onClick={handleToggleSidebar}
            aria-label={collapsed ? (isZh ? '展开左侧栏' : 'Expand sidebar') : (isZh ? '收起左侧栏' : 'Collapse sidebar')}
            aria-expanded={!collapsed}
            title={collapsed ? (isZh ? '展开左侧栏' : 'Expand sidebar') : (isZh ? '收起左侧栏' : 'Collapse sidebar')}
            className="hidden lg:flex p-1.5 rounded-sm border border-transparent hover:border-border hover:bg-bg-subtle text-secondary hover:text-primary transition-colors cursor-pointer flex-shrink-0"
          >
            <ArrowLeft className={`w-4 h-4 transition-transform duration-200 ${collapsed ? 'rotate-180' : ''}`} />
          </button>
        </div>

      {/* Return to Main Dashboard Quick Button (Shown when a domain is selected) */}
      {selectedDomain && (
        <div className={`${collapsed ? 'p-2' : 'p-3'} border-b border-border bg-bg-subtle/50 space-y-2`}>
          <Link
            to="/overview"
            onClick={() => {
              onSelectDomain(null);
              onSelectTab('overview');
            }}
            title={collapsed ? t('sidebar.back_to_main') : undefined}
            className={`${collapsed ? 'h-10 justify-center px-0' : 'px-2.5 py-1.5'} w-full flex items-center gap-2 rounded-sm border border-border bg-card hover:border-primary text-xs font-semibold text-primary transition-all cursor-pointer shadow-sm group`}
          >
            <ArrowLeft className="w-3.5 h-3.5 text-secondary group-hover:-translate-x-0.5 transition-transform flex-shrink-0" />
            {!collapsed && <span className="truncate">{t('sidebar.back_to_main')}</span>}
          </Link>

          {!collapsed && (
            <div className="px-1 text-[11px] font-mono text-tertiary truncate">
              {isZh ? '当前托管区域' : 'Current Zone'}:{' '}
              {/* 名称未到位时用骨架条占位，保持这一行的高度稳定 */}
              {selectedDomain.name ? (
                <span className="text-primary font-bold">{selectedDomain.name}</span>
              ) : (
                <span className="inline-block h-2.5 w-24 bg-bg-subtle rounded-sm animate-pulse align-middle" />
              )}
            </div>
          )}
        </div>
      )}

      {/* Domain Zone Switcher Dropdown (Global context only; a selected zone uses the back button above) */}
      {!selectedDomain && (
        <div className={`${collapsed ? 'p-2' : 'p-3'} border-b border-border relative`}>
          {!collapsed && (
            <label className="text-[10px] font-semibold uppercase tracking-wider text-tertiary mb-1.5 block px-1">
              {t('common.select_domain')}
            </label>
          )}
          <button
            type="button"
            onClick={() => setIsDomainDropdownOpen(!isDomainDropdownOpen)}
            title={collapsed ? t('common.select_domain') : undefined}
            aria-expanded={isDomainDropdownOpen}
            className={`${collapsed ? 'h-10 justify-center p-0' : 'justify-between p-2'} w-full flex items-center rounded-sm border border-border bg-bg-subtle hover:border-border-hover text-xs transition-colors cursor-pointer`}
          >
            <div className="flex items-center gap-2 truncate">
              <Globe className="w-3.5 h-3.5 text-secondary flex-shrink-0" />
              {!collapsed && (
                <span className="font-mono text-primary truncate font-medium">
                  {t('sidebar.global_panel')}
                </span>
              )}
            </div>
            {!collapsed && (
              <ChevronDown className={`w-3.5 h-3.5 text-secondary transition-transform ${isDomainDropdownOpen ? 'rotate-180' : ''}`} />
            )}
          </button>

          {/* Dropdown Menu */}
          {isDomainDropdownOpen && (
            <div
              className={`${collapsed ? 'left-full ml-2 top-0 w-64' : 'left-3 right-3 top-16 mt-1'} absolute bg-card border border-border rounded-sm shadow-2xl py-1 z-50 animate-in fade-in zoom-in-95 duration-100`}
            >
              <div className="p-2 border-b border-border">
                <div className="flex items-center gap-1.5 px-2 py-1 bg-bg-subtle rounded border border-border">
                  <Search className="w-3 h-3 text-tertiary" />
                  <input
                    type="text"
                    placeholder={t('common.search') + '...'}
                    value={domainSearch}
                    onChange={(e) => setDomainSearch(e.target.value)}
                    className="w-full bg-transparent text-xs text-primary focus:outline-none font-mono"
                    autoFocus
                  />
                </div>
              </div>

              <div className="max-h-48 overflow-y-auto py-1">
                <Link
                  to="/domains"
                  onClick={() => {
                    onSelectDomain(null);
                    onSelectTab('domains');
                    setIsDomainDropdownOpen(false);
                  }}
                  className="w-full text-left px-3 py-1.5 text-xs flex items-center justify-between hover:bg-bg-subtle transition-colors cursor-pointer block text-secondary"
                >
                  <span>{t('nav.domains_mgmt')}</span>
                </Link>

                {filteredDomains.map((d) => (
                  <Link
                    key={d.id}
                    to={`/domains/${d.id}/${DOMAIN_DEFAULT_TAB}`}
                    onClick={() => {
                      onSelectDomain(d);
                      onSelectTab(DOMAIN_DEFAULT_TAB);
                      setIsDomainDropdownOpen(false);
                    }}
                    className="w-full text-left px-3 py-1.5 text-xs font-mono flex items-center justify-between hover:bg-bg-subtle transition-colors cursor-pointer block text-secondary"
                  >
                    <span className="truncate">{d.name}</span>
                  </Link>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Navigation Links (Strictly Scoped by Context) */}
      <div className={`${collapsed ? 'py-4 px-2 space-y-3' : 'py-4 px-3 space-y-6'} flex-1 overflow-y-auto`}>
        {currentGroups.map((group, gIdx) => (
          <div key={gIdx} className="space-y-1">
            {!collapsed && (
              <div className="text-[10px] font-semibold uppercase tracking-wider text-tertiary px-2 mb-1.5">
                {group.group}
              </div>
            )}
            {group.items.map((item) => {
              const isActive = activeTab === item.id;
              const to = pathForTab(item.id, selectedDomain?.id);

              return (
                <Link
                  key={item.id}
                  to={to}
                  onClick={() => onSelectTab(item.id)}
                  title={collapsed ? item.label : undefined}
                  aria-label={collapsed ? item.label : undefined}
                  className={`${collapsed ? 'h-10 justify-center px-0' : 'gap-2.5 px-2.5 py-2'} w-full flex items-center rounded-sm text-xs font-medium transition-all cursor-pointer ${
                    isActive
                      ? 'bg-primary text-bg font-semibold shadow-sm'
                      : 'text-secondary hover:text-primary hover:bg-bg-subtle'
                  }`}
                >
                  {item.icon}
                  {!collapsed && <span className="truncate">{item.label}</span>}
                </Link>
              );
            })}
          </div>
        ))}
      </div>

      {/* Bottom User Profile Footer */}
      <div className={`${collapsed ? 'p-2' : 'p-3'} border-t border-border bg-card`}>
        <div className={`${collapsed ? 'flex-col gap-2' : 'justify-between px-1'} flex items-center`}>
          <div
            className={`${collapsed ? 'justify-center' : 'gap-2 truncate'} flex items-center`}
            title={collapsed ? user?.username : undefined}
          >
            <div className="w-6 h-6 rounded-full bg-bg-subtle border border-border flex items-center justify-center font-mono text-[10px] font-bold text-primary flex-shrink-0">
              {user?.username?.[0]?.toUpperCase() || 'A'}
            </div>
            {!collapsed && (
              <div className="truncate">
                <div className="text-xs font-semibold text-primary truncate leading-none">{user?.username}</div>
                <div className="text-[10px] text-tertiary uppercase font-mono mt-0.5">{user?.role}</div>
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={onLogout}
            className="p-1.5 rounded hover:bg-red-500/10 text-secondary hover:text-red-500 transition-colors cursor-pointer"
            title={t('header.logout')}
            aria-label={t('header.logout')}
          >
            <LogOut className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
      </aside>
    </>
  );
};
