import React from 'react';
import { Link } from 'react-router-dom';
import {
  ListOrdered,
  Share2,
  HeartPulse,
  Server,
  Network,
  BarChart3,
  Sliders,
  Layers,
  Lock,
  Cog,
  LayoutGrid,
} from 'lucide-react';
import { Domain } from '../types';

export type TabType =
  | 'overview'
  | 'domains'
  | 'dashboard'
  | 'records'
  | 'routing'
  | 'ddns'
  | 'health'
  | 'certificates'
  | 'nodes'
  | 'node_monitor'
  | 'nameservers'
  | 'routing_lines'
  | 'analytics'
  | 'security'
  | 'access_control'
  | 'security_logs'
  | 'settings'
  | 'global_settings'
  | 'api_docs';

interface NavTabsProps {
  selectedDomain: Domain | null;
  activeTab: TabType;
  onSelectTab: (tab: TabType) => void;
}

const DOMAIN_TABS: TabType[] = [
  'dashboard',
  'records',
  'routing',
  'health',
  'settings',
];

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

export const NavTabs: React.FC<NavTabsProps> = ({
  selectedDomain,
  activeTab,
  onSelectTab,
}) => {
  const tabs: { id: TabType; label: string; icon: React.ReactNode; domainOnly?: boolean }[] = [
    { id: 'overview', label: 'Dashboard', icon: <Layers className="w-4 h-4" /> },
    { id: 'domains', label: 'Domains', icon: <LayoutGrid className="w-4 h-4" /> },
    { id: 'records', label: 'DNS Records', icon: <ListOrdered className="w-4 h-4" />, domainOnly: true },
    { id: 'routing', label: 'Smart Routing', icon: <Share2 className="w-4 h-4" />, domainOnly: true },
    { id: 'health', label: 'Health & Failover', icon: <HeartPulse className="w-4 h-4" />, domainOnly: true },
    { id: 'certificates', label: 'SSL Certificates', icon: <Lock className="w-4 h-4" /> },
    { id: 'nodes', label: 'Edge Nodes', icon: <Server className="w-4 h-4" /> },
    { id: 'nameservers', label: 'Nameservers', icon: <Network className="w-4 h-4" /> },
    { id: 'analytics', label: 'Analytics', icon: <BarChart3 className="w-4 h-4" /> },
    { id: 'settings', label: 'Zone Settings', icon: <Sliders className="w-4 h-4" />, domainOnly: true },
    { id: 'global_settings', label: 'System Settings', icon: <Cog className="w-4 h-4" /> },
  ];

  return (
    <div className="w-full bg-card border-b border-border">
      <div className="max-w-[1400px] mx-auto px-4 sm:px-6 flex items-center gap-1 overflow-x-auto no-scrollbar">
        {tabs.map((tab) => {
          if (tab.domainOnly && !selectedDomain) return null;
          const isActive = activeTab === tab.id;
          const to = pathForTab(tab.id, selectedDomain?.id);

          return (
            <Link
              key={tab.id}
              to={to}
              onClick={() => onSelectTab(tab.id)}
              className={`flex items-center gap-2 py-3.5 px-3 text-xs font-medium border-b-2 transition-all whitespace-nowrap cursor-pointer ${
                isActive
                  ? 'border-primary text-primary font-semibold'
                  : 'border-transparent text-secondary hover:text-primary hover:border-border-hover'
              }`}
            >
              {tab.icon}
              <span>{tab.label}</span>
            </Link>
          );
        })}
      </div>
    </div>
  );
};
