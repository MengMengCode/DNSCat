import React, { useState } from 'react';
import { 
  Globe, 
  Sun, 
  Moon, 
  Key, 
  LogOut, 
  Plus, 
  ChevronDown, 
  Server, 
  ShieldCheck, 
  Activity,
  Search,
  ExternalLink
} from 'lucide-react';
import { Domain, User } from '../types';
import { Button, Badge, Modal, Input, CodeBox } from './GeistUI';
import { api } from '../api/client';
import { useDialog } from './DialogProvider';

interface HeaderProps {
  user: User | null;
  domains: Domain[];
  selectedDomain: Domain | null;
  onSelectDomain: (d: Domain | null) => void;
  onOpenAddDomain: () => void;
  onLogout: () => void;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
}

export const Header: React.FC<HeaderProps> = ({
  user,
  domains,
  selectedDomain,
  onSelectDomain,
  onOpenAddDomain,
  onLogout,
  theme,
  onToggleTheme,
}) => {
  const { alert } = useDialog();
  const [showDomainDropdown, setShowDomainDropdown] = useState(false);
  const [showUserDropdown, setShowUserDropdown] = useState(false);
  const [showApiKeyModal, setShowApiKeyModal] = useState(false);
  const [currentApiKey, setCurrentApiKey] = useState(user?.api_key || '');
  const [searchFilter, setSearchFilter] = useState('');

  const filteredDomains = domains.filter((d) =>
    d.name.toLowerCase().includes(searchFilter.toLowerCase())
  );

  const handleRegenerateApiKey = async () => {
    try {
      const res = await api.regenerateApiKey();
      setCurrentApiKey(res.api_key);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  return (
    <>
      <header className="sticky top-0 z-40 w-full bg-card/80 backdrop-blur-md border-b border-border">
        <div className="max-w-[1400px] mx-auto px-4 sm:px-6 h-16 flex items-center justify-between gap-4">
          {/* Left: Brand & Domain Switcher */}
          <div className="flex items-center gap-3">
            <div
              onClick={() => onSelectDomain(null)}
              className="flex items-center gap-2 cursor-pointer group select-none"
            >
              <div className="w-8 h-8 rounded-sm bg-primary text-bg flex items-center justify-center font-mono font-bold text-sm tracking-tighter shadow-sm group-hover:scale-105 transition-transform">
                <Globe className="w-4 h-4" />
              </div>
              <div className="flex flex-col">
                <span className="font-bold text-sm tracking-tight text-primary leading-none flex items-center gap-1.5">
                  DnsCat
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-500 font-mono font-normal">
                    v1.0
                  </span>
                </span>
                <span className="text-[11px] text-tertiary font-mono leading-none mt-0.5">
                  Authoritative DNS
                </span>
              </div>
            </div>

            <div className="h-4 w-[1px] bg-border mx-1" />

            {/* Domain Dropdown Selector */}
            <div className="relative">
              <button
                onClick={() => setShowDomainDropdown(!showDomainDropdown)}
                className="flex items-center gap-2 h-9 px-3 rounded-sm border border-border bg-bg-subtle text-primary hover:border-border-hover transition-colors text-xs font-medium"
              >
                {selectedDomain ? (
                  <>
                    <span className="font-mono text-sm font-semibold">{selectedDomain.name}</span>
                    <Badge
                      variant={selectedDomain.ns_status === 'verified' ? 'success' : 'warning'}
                      size="sm"
                    >
                      {selectedDomain.ns_status === 'verified' ? 'Active' : 'Pending NS'}
                    </Badge>
                  </>
                ) : (
                  <span className="text-secondary font-mono">Overview Dashboard</span>
                )}
                <ChevronDown className="w-3.5 h-3.5 text-tertiary ml-1" />
              </button>

              {showDomainDropdown && (
                <>
                  <div
                    className="fixed inset-0 z-20"
                    onClick={() => setShowDomainDropdown(false)}
                  />
                  <div className="absolute left-0 mt-1.5 w-72 bg-card border border-border rounded-md shadow-popover p-2 z-30 animate-in fade-in zoom-in-95 duration-100">
                    <div className="px-2 py-1.5 border-b border-border mb-1.5">
                      <div className="relative">
                        <Search className="w-3.5 h-3.5 absolute left-2.5 top-2.5 text-tertiary" />
                        <input
                          type="text"
                          placeholder="Search domain..."
                          value={searchFilter}
                          onChange={(e) => setSearchFilter(e.target.value)}
                          className="w-full h-8 pl-8 pr-2 text-xs bg-bg-subtle border border-border rounded-sm text-primary placeholder:text-tertiary focus:outline-none focus:border-primary font-mono"
                        />
                      </div>
                    </div>

                    <div className="max-h-60 overflow-y-auto space-y-0.5">
                      <button
                        onClick={() => {
                          onSelectDomain(null);
                          setShowDomainDropdown(false);
                        }}
                        className="w-full text-left px-3 py-2 rounded-sm text-xs text-secondary hover:text-primary hover:bg-bg-subtle flex items-center justify-between transition-colors"
                      >
                        <span className="font-medium">Overview Dashboard</span>
                        <Activity className="w-3.5 h-3.5 text-tertiary" />
                      </button>

                      {filteredDomains.map((d) => (
                        <button
                          key={d.id}
                          onClick={() => {
                            onSelectDomain(d);
                            setShowDomainDropdown(false);
                          }}
                          className={`w-full text-left px-3 py-2 rounded-sm text-xs flex items-center justify-between transition-colors ${
                            selectedDomain?.id === d.id
                              ? 'bg-primary/10 text-primary font-semibold'
                              : 'text-secondary hover:text-primary hover:bg-bg-subtle'
                          }`}
                        >
                          <span className="font-mono">{d.name}</span>
                          <span className="text-[10px] text-tertiary">
                            {d.record_count || 0} records
                          </span>
                        </button>
                      ))}

                      {filteredDomains.length === 0 && (
                        <div className="p-3 text-center text-xs text-tertiary">
                          No domains found
                        </div>
                      )}
                    </div>

                    <div className="mt-2 pt-2 border-t border-border">
                      <Button
                        size="sm"
                        variant="primary"
                        className="w-full"
                        onClick={() => {
                          setShowDomainDropdown(false);
                          onOpenAddDomain();
                        }}
                        icon={<Plus className="w-3.5 h-3.5" />}
                      >
                        Add New Domain
                      </Button>
                    </div>
                  </div>
                </>
              )}
            </div>
          </div>

          {/* Right: Actions & User */}
          <div className="flex items-center gap-2">
            <button
              onClick={onToggleTheme}
              className="w-9 h-9 flex items-center justify-center rounded-sm border border-border bg-card text-secondary hover:text-primary hover:border-border-hover transition-colors"
              title="Toggle theme"
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>

            <Button
              size="sm"
              variant="secondary"
              onClick={() => setShowApiKeyModal(true)}
              icon={<Key className="w-3.5 h-3.5" />}
              className="hidden sm:inline-flex"
            >
              API Key
            </Button>

            <Button
              size="sm"
              variant="primary"
              onClick={onOpenAddDomain}
              icon={<Plus className="w-3.5 h-3.5" />}
            >
              Add Domain
            </Button>

            {/* User Avatar & Menu */}
            <div className="relative ml-1">
              <button
                onClick={() => setShowUserDropdown(!showUserDropdown)}
                className="w-9 h-9 rounded-full bg-bg-subtle border border-border flex items-center justify-center font-mono font-semibold text-xs text-primary hover:border-border-hover transition-colors"
              >
                {user?.username?.[0]?.toUpperCase() || 'A'}
              </button>

              {showUserDropdown && (
                <>
                  <div className="fixed inset-0 z-20" onClick={() => setShowUserDropdown(false)} />
                  <div className="absolute right-0 mt-1.5 w-56 bg-card border border-border rounded-md shadow-popover p-2 z-30">
                    <div className="px-3 py-2 border-b border-border mb-1">
                      <div className="text-xs font-semibold text-primary">{user?.username}</div>
                      <div className="text-[11px] text-tertiary font-mono truncate">{user?.email}</div>
                      <div className="mt-1">
                        <Badge size="sm" variant="info">
                          {user?.role?.toUpperCase()}
                        </Badge>
                      </div>
                    </div>

                    <button
                      onClick={() => {
                        setShowUserDropdown(false);
                        setShowApiKeyModal(true);
                      }}
                      className="w-full text-left px-3 py-2 rounded-sm text-xs text-secondary hover:text-primary hover:bg-bg-subtle flex items-center gap-2"
                    >
                      <Key className="w-3.5 h-3.5 text-tertiary" />
                      API Keys & Automation
                    </button>

                    <button
                      onClick={onLogout}
                      className="w-full text-left px-3 py-2 rounded-sm text-xs text-red-500 hover:bg-red-500/10 flex items-center gap-2 mt-1"
                    >
                      <LogOut className="w-3.5 h-3.5" />
                      Sign Out
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </header>

      {/* API Key Modal */}
      <Modal
        isOpen={showApiKeyModal}
        onClose={() => setShowApiKeyModal(false)}
        title="DnsCat API Token"
        description="Use this token to authenticate REST API calls or automate DNS updates with Terraform/Certbot."
        maxWidth="lg"
      >
        <div className="space-y-4">
          <div>
            <label className="text-xs font-medium text-secondary mb-1.5 block">Your API Key</label>
            <CodeBox code={currentApiKey || user?.api_key || ''} />
          </div>

          <div className="p-3 bg-bg-subtle rounded-sm border border-border text-xs space-y-2">
            <div className="font-medium text-primary">Example Usage with curl:</div>
            <CodeBox
              code={`curl -H "X-API-Key: ${currentApiKey || user?.api_key}" http://localhost:8080/api/domains`}
            />
          </div>

          <div className="flex justify-between items-center pt-2">
            <Button variant="error" size="sm" onClick={handleRegenerateApiKey}>
              Regenerate API Key
            </Button>
            <Button variant="secondary" size="sm" onClick={() => setShowApiKeyModal(false)}>
              Close
            </Button>
          </div>
        </div>
      </Modal>
    </>
  );
};
