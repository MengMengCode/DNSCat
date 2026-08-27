import React, { useState, useEffect, useMemo } from 'react';
import {
  Routes,
  Route,
  Navigate,
  useLocation,
  useNavigate,
  useParams,
} from 'react-router-dom';
import { Sidebar } from './components/Sidebar';
import { TopHeader } from './components/TopHeader';
import { TabType } from './components/NavTabs';
import { OverviewPage } from './pages/OverviewPage';
import { DomainDashboardPage } from './pages/DomainDashboardPage';
import { DomainsPage } from './pages/DomainsPage';
import { RecordsPage } from './pages/RecordsPage';
import { DdnsPage } from './pages/DdnsPage';
import { HealthPage } from './pages/HealthPage';
import { SecurityPage } from './pages/SecurityPage';
import { SecurityLogsPage } from './pages/SecurityLogsPage';
import { AccessControlPage } from './pages/AccessControlPage';
import { CertificatesPage } from './pages/CertificatesPage';
import { NodesPage } from './pages/NodesPage';
import { NodeStatusPage } from './pages/NodeStatusPage';
import { NodeMonitorPage } from './pages/NodeMonitorPage';
import { NameserversPage } from './pages/NameserversPage';
import { RoutingLinesPage } from './pages/RoutingLinesPage';
import { AnalyticsPage } from './pages/AnalyticsPage';
import { SettingsPage } from './pages/SettingsPage';
import { SystemSettingsPage } from './pages/SystemSettingsPage';
import { ApiDocsPage } from './pages/ApiDocsPage';
import { LoginPage } from './pages/LoginPage';
import { Modal, Input, Button, CodeBox } from './components/GeistUI';
import { DialogProvider } from './components/DialogProvider';
import { Domain, User, SummaryStats, ClusterNode } from './types';
import { api } from './api/client';
import { I18nProvider, useI18n } from './i18n/I18nContext';
import { ArrowRight } from 'lucide-react';

// 全局页签与 URL 片段的唯一映射来源。
// 注意 global_settings 的路由是 kebab-case (/global-settings)，与页签 id 的下划线写法不同，
// 因此「页签 → 路径」和「路径 → 页签」两个方向必须共用同一份映射，否则侧边栏高亮会失配。
const GLOBAL_TAB_ROUTES: ReadonlyArray<readonly [TabType, string]> = [
  ['overview', 'overview'],
  ['domains', 'domains'],
  ['certificates', 'certificates'],
  ['nodes', 'nodes'],
  ['node_monitor', 'node-monitor'],
  ['nameservers', 'nameservers'],
  ['routing_lines', 'routing-lines'],
  ['analytics', 'analytics'],
  ['global_settings', 'global-settings'],
  ['api_docs', 'api-docs'],
];

const SEGMENT_BY_GLOBAL_TAB = new Map<TabType, string>(GLOBAL_TAB_ROUTES);

const GLOBAL_TAB_BY_SEGMENT = new Map<string, TabType>(
  GLOBAL_TAB_ROUTES.map(([tab, segment]) => [segment, tab])
);

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

// 进入某个域名详情时的默认落地页签：所有入口（列表点击、侧栏下拉、面包屑、
// 直接访问 /domains/:id、新建域名后）都以此为准，避免各入口行为不一致。
const DOMAIN_DEFAULT_TAB: TabType = 'dashboard';

const pathForTab = (tab: TabType, domainId?: number | string) => {
  if (DOMAIN_TABS.includes(tab) && domainId !== undefined) {
    return `/domains/${domainId}/${tab}`;
  }

  const segment = SEGMENT_BY_GLOBAL_TAB.get(tab);
  return segment ? `/${segment}` : '/overview';
};

// 域名子页面的首屏骨架。刻意做成与真实页面相近的块状结构（标题 + 指标卡 +
// 图表 + 分布卡），让数据到位时是「填充」而不是从一行小字跳变成整页内容。
const DomainPageSkeleton: React.FC = () => (
  <div className="space-y-6 animate-pulse">
    <div className="h-4 w-40 bg-bg-subtle rounded-sm" />
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="geist-card p-5 space-y-3">
          <div className="h-3 w-24 bg-bg-subtle rounded-sm" />
          <div className="h-6 w-20 bg-bg-subtle rounded-sm" />
          <div className="h-2.5 w-32 bg-bg-subtle rounded-sm" />
        </div>
      ))}
    </div>
    <div className="geist-card h-64" />
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <div className="geist-card h-56" />
      <div className="geist-card h-56" />
    </div>
  </div>
);

const MainLayout: React.FC = () => {
  const { t, language, setLanguage } = useI18n();
  const isZh = language === 'zh-CN';
  const location = useLocation();
  const navigate = useNavigate();

  const [user, setUser] = useState<User | null>(null);
  const [authChecked, setAuthChecked] = useState(false);
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');

  // Domains & Cluster State
  const [domains, setDomains] = useState<Domain[]>([]);
  // 域名清单是否已经拉取过一轮。用于区分「还在加载」与「确实不存在这个域名」，
  // 避免刷新域名子页时无限转圈或错误地按全局上下文渲染。
  const [domainsLoaded, setDomainsLoaded] = useState(false);
  const [stats, setStats] = useState<SummaryStats | null>(null);
  const [nodes, setNodes] = useState<ClusterNode[]>([]);

  // Add Domain Modal
  const [isAddDomainModalOpen, setIsAddDomainModalOpen] = useState(false);
  const [newDomainName, setNewDomainName] = useState('');
  const [addDomainError, setAddDomainError] = useState('');
  const [addDomainLoading, setAddDomainLoading] = useState(false);

  // 移动端侧栏抽屉开关（仅 <lg 生效）
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  // 新增域名成功后的 NS 配置指引：告知用户需要在注册商处把 NS 改成什么，
  // 而不是直接跳进域名详情页（此时 NS 尚未生效，解析不会工作）。
  const [nsGuide, setNsGuide] = useState<{ domain: Domain; nsList: string[] } | null>(null);

  // applyTheme 只负责把主题落到 DOM 与本地存储。
  // 本地存储始终写：它既是未登录时的唯一载体，也是登录用户的首屏缓存，
  // 避免下次打开时先按默认主题渲染一帧再被账号偏好纠正造成闪烁。
  const applyTheme = (next: 'dark' | 'light') => {
    setTheme(next);
    localStorage.setItem('dnscat_theme', next);
    if (next === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  };

  // Initialize Theme
  useEffect(() => {
    const savedTheme = (localStorage.getItem('dnscat_theme') as 'dark' | 'light') || 'dark';
    applyTheme(savedTheme);
  }, []);

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
    // 已登录则把偏好写进账号，让它跟着账号走而不是跟着这台浏览器。
    if (user) {
      setUser((prev) => (prev ? { ...prev, theme: next } : prev));
      api.updatePreferences({ theme: next }).catch((err) => console.error(err));
    }
  };

  // Auth Initialization
  const checkAuth = async () => {
    const token = localStorage.getItem('dnscat_token');
    if (!token) {
      setAuthChecked(true);
      return;
    }

    try {
      const u = await api.getMe();
      setUser(u);
    } catch {
      localStorage.removeItem('dnscat_token');
      setUser(null);
    } finally {
      setAuthChecked(true);
    }
  };

  useEffect(() => {
    checkAuth();
    window.addEventListener('dnscat_unauthorized', () => setUser(null));
  }, []);

  // 登录后以账号里的界面偏好为准，覆盖这台浏览器的本地设置。
  // 账号里为空表示还没保存过（老账号或首次登录），此时反过来把当前本地设置
  // 写进账号作为初始值，用户不必先手动切换一次才能让偏好跟随账号。
  useEffect(() => {
    if (!user) return;

    const accountLang = user.language === 'zh-CN' || user.language === 'en-US' ? user.language : '';
    const accountTheme = user.theme === 'dark' || user.theme === 'light' ? user.theme : '';

    if (accountLang && accountLang !== language) {
      setLanguage(accountLang);
    }
    if (accountTheme && accountTheme !== theme) {
      applyTheme(accountTheme);
    }

    const seed: { language?: string; theme?: string } = {};
    if (!accountLang) seed.language = language;
    if (!accountTheme) seed.theme = theme;
    if (seed.language || seed.theme) {
      api
        .updatePreferences(seed)
        .then(() => setUser((prev) => (prev ? { ...prev, ...seed } : prev)))
        .catch((err) => console.error(err));
    }
    // 只在拿到账号信息时对齐一次：依赖里刻意不放 language / theme，
    // 否则用户手动切换后会被这里立刻按账号旧值改回去。
  }, [user?.id]);

  // 语言切换发生在顶栏（直接调 I18n 的 setLanguage），这里统一把结果同步到账号。
  // 以「当前语言与账号记录不一致」为触发条件，因此上面按账号值回填时不会再写回一次。
  useEffect(() => {
    if (!user) return;
    if (!user.language) return;
    if (user.language === language) return;

    setUser((prev) => (prev ? { ...prev, language } : prev));
    api.updatePreferences({ language }).catch((err) => console.error(err));
  }, [language, user?.language, user?.id]);

  // Load Global Data
  const loadGlobalData = async () => {
    if (!user) return;
    try {
      const [domainsRes, statsRes, nodesRes] = await Promise.all([
        api.listDomains(),
        api.getSummary(),
        api.listNodes(),
      ]);
      setDomains(domainsRes.domains);
      setStats(statsRes);
      setNodes(nodesRes.nodes);
    } catch (err) {
      console.error(err);
    } finally {
      // 失败时也置位，否则域名子页会一直停在骨架屏上。
      setDomainsLoaded(true);
    }
  };

  useEffect(() => {
    if (user) {
      loadGlobalData();
    }
  }, [user]);

  // Resolve active tab and selected domain from the URL
  const { activeTab, selectedDomain } = useMemo(() => {
    const parts = location.pathname.split('/').filter(Boolean);
    let tab: TabType = 'overview';
    let domain: Domain | null = null;

    if (parts[0] === 'domains' && parts[1]) {
      const domainId = parts[1];
      domain = domains.find((d) => String(d.id) === domainId) || null;
      const maybeTab = parts[2] as TabType;
      tab = DOMAIN_TABS.includes(maybeTab) ? maybeTab : DOMAIN_DEFAULT_TAB;

      // 导航上下文只由 URL 决定，不受数据加载状态影响：只要路径是 /domains/:id/*，
      // 侧栏与面包屑就始终处于域名上下文。否则清单未返回（或请求失败）时
      // selectedDomain 会是 null，导致先按「全局面板」渲染一帧再切成域名详情，
      // 看起来就像从首页跳进来。
      // 名称在清单到位前留空，由侧栏与面包屑渲染骨架条；若清单已到位仍找不到该 ID，
      // 退化成 #ID 以便识别（主内容区会另行提示未找到）。
      if (!domain) {
        domain = {
          id: Number(domainId),
          name: domainsLoaded ? `#${domainId}` : '',
        } as Domain;
      }
    } else {
      tab = GLOBAL_TAB_BY_SEGMENT.get(parts[0] ?? '') ?? 'overview';
    }

    return { activeTab: tab, selectedDomain: domain };
  }, [location.pathname, domains, domainsLoaded]);

  // 路由变化后自动收起移动端抽屉，避免导航后遮罩残留。
  useEffect(() => {
    setMobileNavOpen(false);
  }, [location.pathname]);

  const handleLoginSuccess = (token: string, u: User) => {
    localStorage.setItem('dnscat_token', token);
    setUser(u);
  };

  const handleLogout = () => {
    localStorage.removeItem('dnscat_token');
    setUser(null);
    navigate('/overview', { replace: true });
  };

  const handleSelectDomain = (d: Domain | null) => {
    if (d) {
      navigate(pathForTab(DOMAIN_DEFAULT_TAB, d.id));
    } else {
      navigate('/overview');
    }
  };

  const handleSelectTab = (tab: TabType) => {
    navigate(pathForTab(tab, selectedDomain?.id));
  };

  const handleCreateDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    setAddDomainError('');
    setAddDomainLoading(true);

    try {
      const created = await api.createDomain({
        name: newDomainName.trim(),
      });
      setIsAddDomainModalOpen(false);
      setNewDomainName('');
      await loadGlobalData();

      // 取该域名实际生成的 NS 记录作为「需要配置的权威 NS」：这是真实来源，
      // 避免把某个默认 NS 主机名硬编码给用户（各部署的 default_ns 不同）。
      let nsList: string[] = [];
      try {
        const res = await api.listRecords(created.id, { type: 'NS' });
        nsList = Array.from(
          new Set(
            (res.records || [])
              .filter((r) => r.name === '@' || r.name === '')
              .map((r) => r.value.replace(/\.$/, ''))
              .filter(Boolean)
          )
        );
      } catch {
        // 记录拉取失败不影响建站结果，下面回退到域名的主 NS。
      }
      if (nsList.length === 0 && created.primary_ns) {
        nsList = [created.primary_ns.replace(/\.$/, '')];
      }

      setNsGuide({ domain: created, nsList });
    } catch (err: any) {
      setAddDomainError(err.message);
    } finally {
      setAddDomainLoading(false);
    }
  };

  // Guard domain-scoped routes: if the domain is not loaded yet, show a loader.
  const DomainRoute: React.FC<{ render: (domain: Domain) => React.ReactNode }> = ({
    render,
  }) => {
    const { domainId } = useParams<{ domainId: string }>();
    const domain = domains.find((d) => String(d.id) === domainId);

    if (domain) {
      return <>{render(domain)}</>;
    }

    // 清单已经拉过一轮却找不到该 ID：域名不存在、已删除或无权访问，
    // 明确告知并给出出口，而不是一直停在加载态。
    if (domainsLoaded) {
      return (
        <div className="geist-card p-10 text-center space-y-4">
          <div className="text-sm text-secondary">
            {isZh
              ? '未找到该域名，它可能已被删除。'
              : 'Domain not found. It may have been deleted.'}
          </div>
          <Button size="sm" variant="secondary" onClick={() => navigate('/domains')}>
            {isZh ? '返回域名列表' : 'Back to domains'}
          </Button>
        </div>
      );
    }

    return <DomainPageSkeleton />;
  };

  if (!authChecked) {
    return (
      <div className="min-h-screen bg-bg flex items-center justify-center font-mono text-xs text-tertiary">
        {isZh ? '正在初始化 DnsCat...' : 'Initializing DnsCat...'}
      </div>
    );
  }

  if (!user) {
    return (
      <LoginPage onLoginSuccess={handleLoginSuccess} theme={theme} onToggleTheme={toggleTheme} />
    );
  }

  return (
    <div className="h-screen w-screen flex bg-bg text-primary overflow-hidden">
      {/* Left Sidebar (Fixed, Never scrolls with page) */}
      <Sidebar
        user={user}
        domains={domains}
        selectedDomain={selectedDomain}
        activeTab={activeTab}
        onSelectDomain={handleSelectDomain}
        onSelectTab={handleSelectTab}
        onOpenAddDomain={() => setIsAddDomainModalOpen(true)}
        onLogout={handleLogout}
        mobileOpen={mobileNavOpen}
        onMobileClose={() => setMobileNavOpen(false)}
      />

      {/* Main Right Area (Independent Scroll Container) */}
      <div className="flex-1 flex flex-col min-w-0 h-screen overflow-y-auto">
        {/* Top Header */}
        <TopHeader
          selectedDomain={selectedDomain}
          activeTab={activeTab}
          theme={theme}
          onToggleTheme={toggleTheme}
          onSelectDomain={handleSelectDomain}
          onSelectTab={handleSelectTab}
          onOpenMobileNav={() => setMobileNavOpen(true)}
        />

        {/* Content Container */}
        <main className="flex-1 max-w-[1400px] w-full mx-auto px-4 py-5 sm:px-6 sm:py-8">
          <Routes>
            <Route path="/" element={<Navigate to="/overview" replace />} />
            <Route
              path="/overview"
              element={
                <OverviewPage
                  stats={stats}
                  domains={domains}
                  nodes={nodes}
                  onSelectDomain={handleSelectDomain}
                  onOpenAddDomain={() => setIsAddDomainModalOpen(true)}
                  onGoToNodes={() => navigate('/nodes')}
                  onGoToAnalytics={() => navigate('/analytics')}
                />
              }
            />
            <Route
              path="/domains"
              element={
                <DomainsPage
                  domains={domains}
                  onSelectDomain={handleSelectDomain}
                  onOpenAddDomain={() => setIsAddDomainModalOpen(true)}
                  onRefreshDomains={loadGlobalData}
                />
              }
            />
            <Route path="/nodes" element={<NodesPage />} />
            <Route path="/nodes/:nodeId" element={<NodeStatusPage />} />
            <Route path="/node-monitor" element={<NodeMonitorPage />} />
            <Route path="/nameservers" element={<NameserversPage />} />
            <Route path="/routing-lines" element={<RoutingLinesPage />} />
            <Route path="/analytics" element={<AnalyticsPage />} />
            <Route
              path="/certificates"
              element={<CertificatesPage domain={null} domains={domains} />}
            />
            <Route
              path="/global-settings"
              element={<SystemSettingsPage user={user} onRefreshUser={checkAuth} />}
            />
            <Route path="/api-docs" element={<ApiDocsPage />} />

            {/* Domain-scoped routes */}
            <Route
              path="/domains/:domainId/dashboard"
              element={<DomainRoute render={(domain) => <DomainDashboardPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/records"
              element={
                <DomainRoute
                  render={(domain) => (
                    <RecordsPage domain={domain} onRefreshDomain={loadGlobalData} />
                  )}
                />
              }
            />
            <Route
              path="/domains/:domainId/ddns"
              element={<DomainRoute render={(domain) => <DdnsPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/health"
              element={<DomainRoute render={(domain) => <HealthPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/security"
              element={<DomainRoute render={(domain) => <SecurityPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/security_logs"
              element={<DomainRoute render={(domain) => <SecurityLogsPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/access_control"
              element={<DomainRoute render={(domain) => <AccessControlPage domain={domain} />} />}
            />
            <Route
              path="/domains/:domainId/settings"
              element={
                <DomainRoute
                  render={(domain) => (
                    <SettingsPage
                      domain={domain}
                      onRefreshDomain={loadGlobalData}
                      onDomainDeleted={() => {
                        navigate('/overview');
                        loadGlobalData();
                      }}
                    />
                  )}
                />
              }
            />
            <Route
              path="/domains/:domainId"
              element={<Navigate to={DOMAIN_DEFAULT_TAB} replace />}
            />
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Routes>
        </main>
      </div>

      {/* Add Domain Wizard Modal */}
      <Modal
        isOpen={isAddDomainModalOpen}
        onClose={() => setIsAddDomainModalOpen(false)}
        title={t('common.add_domain')}
        description={isZh ? '输入您的根域名以配置权威 Nameserver 记录。' : 'Enter your apex domain name to configure authoritative nameserver records.'}
        maxWidth="md"
      >
        <form onSubmit={handleCreateDomain} className="space-y-4">
          {addDomainError && (
            <div className="p-3 rounded-sm bg-red-500/10 border border-red-500/20 text-xs text-red-500 font-medium">
              {addDomainError}
            </div>
          )}

          <div>
            <Input
              label={isZh ? '域名 (Zone Apex)' : 'Domain Name (Zone Apex)'}
              value={newDomainName}
              onChange={(e) => setNewDomainName(e.target.value)}
              placeholder="e.g. mycompany.com or sub.example.org"
              required
              autoFocus
            />
          </div>

          <div className="flex justify-end items-center gap-2 pt-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsAddDomainModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={addDomainLoading}
              icon={<ArrowRight className="w-3.5 h-3.5" />}
            >
              {t('common.add')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* 域名添加成功后的 NS 配置指引 */}
      <Modal
        isOpen={nsGuide !== null}
        onClose={() => setNsGuide(null)}
        title={isZh ? '域名添加成功，请修改 NS 记录' : 'Domain added — update your NS records'}
        description={
          isZh
            ? '请前往您的域名注册商处，把该域名的 Nameserver 改为以下地址，解析才会生效。'
            : 'Go to your domain registrar and point this domain’s nameservers to the addresses below to activate resolution.'
        }
        maxWidth="lg"
      >
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-xs">
            <span className="text-tertiary">{isZh ? '域名' : 'Domain'}:</span>
            <span className="text-primary font-bold">{nsGuide?.domain.name}</span>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <div className="text-xs font-medium text-secondary">
                {isZh ? '需要配置的权威 Nameserver' : 'Authoritative nameservers to configure'}
              </div>
              {nsGuide && nsGuide.nsList.length > 0 && (
                <span className="text-[11px] text-tertiary">
                  {isZh ? `共 ${nsGuide.nsList.length} 台` : `${nsGuide.nsList.length} total`}
                </span>
              )}
            </div>

            {/* 注册商普遍要求至少两条 NS；只有一台权威 NS 时如实告知用户，而不是让其在注册商处卡住 */}
            {nsGuide && nsGuide.nsList.length === 1 && (
              <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-600 dark:text-amber-500">
                {isZh
                  ? '当前系统仅配置了 1 台权威 NS，而多数注册商要求至少填写 2 条 NS 记录。请联系管理员在「权威 NS 服务器」页面补充节点后重试。'
                  : 'Only 1 authoritative nameserver is configured, but most registrars require at least 2 NS records. Ask the administrator to add more on the Nameservers page.'}
              </div>
            )}

            {nsGuide && nsGuide.nsList.length > 0 ? (
              <>
                <div className="border border-border rounded-sm overflow-hidden">
                  <table className="w-full text-left text-xs">
                    <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
                      <tr>
                        <th className="py-2 px-3 font-medium w-16">#</th>
                        <th className="py-2 px-3 font-medium">
                          {isZh ? 'Nameserver 地址' : 'Nameserver'}
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                      {nsGuide.nsList.map((ns, idx) => (
                        <tr key={ns}>
                          <td className="py-2 px-3 text-tertiary">NS{idx + 1}</td>
                          <td className="py-2 px-3 text-primary font-bold select-all">{ns}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {/* 便于一次性复制全部 NS */}
                <CodeBox code={nsGuide.nsList.join('\n')} />
              </>
            ) : (
              <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-600 dark:text-amber-500">
                {isZh
                  ? '未能读取到该域名的 NS 记录，请到「权威 NS 服务器」页面确认本系统的 Nameserver 地址。'
                  : 'Could not read the NS records for this domain. Please check the Nameservers page for this system’s nameserver addresses.'}
              </div>
            )}
          </div>

          <div className="p-3.5 bg-bg-subtle border border-border rounded-sm text-xs space-y-1.5 text-secondary">
            <div className="font-semibold text-primary">{isZh ? '注意事项' : 'Notes'}</div>
            <p>
              {isZh
                ? '· 请在注册商处把上面列出的 NS 全部填上（至少两条），并「替换」原有的全部 NS 记录，而不是追加。'
                : '· Add all nameservers listed above (at least two) at your registrar, replacing the existing NS records rather than appending.'}
            </p>
            <p>
              {isZh
                ? '· NS 变更需要等待全球 DNS 缓存刷新，通常几分钟到 48 小时生效。'
                : '· NS changes require global DNS cache refresh, typically from a few minutes up to 48 hours.'}
            </p>
            <p>
              {isZh
                ? '· 生效前该域名的解析不会由本系统接管，可在域名列表点击校验按钮检查 NS 指向状态。'
                : '· Until it takes effect, this system will not serve the domain. Use the verify button in the domain list to check NS delegation status.'}
            </p>
          </div>

          <div className="flex flex-col sm:flex-row justify-end items-stretch sm:items-center gap-2 pt-2 border-t border-border">
            <Button variant="secondary" size="sm" onClick={() => setNsGuide(null)}>
              {isZh ? '我知道了' : 'Got it'}
            </Button>
            <Button
              variant="primary"
              size="sm"
              icon={<ArrowRight className="w-3.5 h-3.5" />}
              onClick={() => {
                const target = nsGuide?.domain;
                setNsGuide(null);
                if (target) navigate(pathForTab(DOMAIN_DEFAULT_TAB, target.id));
              }}
            >
              {isZh ? '进入域名详情' : 'Go to domain'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export const App: React.FC = () => {
  return (
    <I18nProvider>
      <DialogProvider>
        <MainLayout />
      </DialogProvider>
    </I18nProvider>
  );
};
