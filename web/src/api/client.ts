import {
  Domain,
  DNSRecord,
  DNSSECResponse,
  DNSSECKey,
  SSLCertificate,
  CertApplicant,
  ACMEProvider,
  HealthCheck,
  DdnsKey,
  DdnsKeyPayload,
  ClusterNode,
  NodeUptimeResponse,
  NodeMetricsResponse,
  SummaryStats,
  QPSTrendPoint,
  TrendRange,
  TypeBreakdown,
  GeoBreakdown,
  DomainStats,
  Nameserver,
  RoutingLine,
  User,
  RecordType,
  GeoLine,
  TestRoutingResult,
  SecurityPolicy,
  SecurityResponse,
  SecurityEventsResponse,
} from '../types';

const BASE_URL = '/api';

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem('dnscat_token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE_URL}${endpoint}`, {
    ...options,
    headers,
  });

  if (res.status === 401) {
    if (!endpoint.startsWith('/auth/login') && !endpoint.startsWith('/auth/register')) {
      localStorage.removeItem('dnscat_token');
      window.dispatchEvent(new Event('dnscat_unauthorized'));
    }
  }

  if (!res.ok) {
    let errorMsg = `Request failed with status ${res.status}`;
    try {
      const data = await res.json();
      if (data.error) errorMsg = data.error;
    } catch {
      // fallback
    }
    throw new Error(errorMsg);
  }

  return res.json();
}

export const api = {
  // Auth
  login: (data: { username: string; password: string }) =>
    request<{ token: string; user: User }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  register: (data: { username: string; email: string; password: string }) =>
    request<{ token: string; user: User }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  getMe: () => request<User>('/auth/me'),
  regenerateApiKey: () =>
    request<{ api_key: string }>('/auth/regenerate-api-key', { method: 'POST' }),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request<{ message: string }>('/auth/change-password', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  // 保存跟随账号的界面偏好；仅登录状态下调用，未登录时前端只写本地存储。
  updatePreferences: (data: { language?: string; theme?: string }) =>
    request<{ message: string }>('/auth/preferences', {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  // Domains
  listDomains: (search = '') =>
    request<{ domains: Domain[]; total: number }>(`/domains?search=${encodeURIComponent(search)}`),
  createDomain: (data: { name: string; primary_ns?: string; admin_email?: string }) =>
    request<Domain>('/domains', { method: 'POST', body: JSON.stringify(data) }),
  testRouting: (domainId: number, data: { client_ip: string; query_name?: string; query_type?: string }) =>
    request<TestRoutingResult>(
      `/domains/${domainId}/test-routing`,
      { method: 'POST', body: JSON.stringify(data) }
    ),
  getDomain: (id: number) => request<Domain>(`/domains/${id}`),
  updateDomain: (id: number, data: Partial<Domain>) =>
    request<{ message: string }>(`/domains/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteDomain: (id: number) =>
    request<{ message: string }>(`/domains/${id}`, { method: 'DELETE' }),
  verifyNS: (id: number) =>
    request<{ ns_status: string; expected_ns: string[]; found_ns: string[]; verified: boolean }>(
      `/domains/${id}/verify-ns`,
      { method: 'POST' }
    ),

  // Records
  listRecords: (domainId: number, filters?: { type?: string; search?: string; geo_line?: string }) => {
    const params = new URLSearchParams();
    if (filters?.type) params.append('type', filters.type);
    if (filters?.search) params.append('search', filters.search);
    if (filters?.geo_line) params.append('geo_line', filters.geo_line);
    return request<{ records: DNSRecord[]; total: number }>(
      `/domains/${domainId}/records?${params.toString()}`
    );
  },
  createRecord: (
    domainId: number,
    data: {
      name: string;
      type: RecordType;
      value: string;
      ttl?: number;
      priority?: number;
      weight?: number;
      port?: number;
      geo_line?: GeoLine;
      enabled?: boolean;
      comment?: string;
    }
  ) =>
    request<DNSRecord>(`/domains/${domainId}/records`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateRecord: (
    id: number,
    data: {
      name: string;
      type: RecordType;
      value: string;
      ttl?: number;
      priority?: number;
      weight?: number;
      port?: number;
      geo_line?: GeoLine;
      enabled?: boolean;
      comment?: string;
    }
  ) =>
    request<{ message: string }>(`/records/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  deleteRecord: (id: number) =>
    request<{ message: string }>(`/records/${id}`, { method: 'DELETE' }),
  toggleRecord: (id: number) =>
    request<{ enabled: boolean }>(`/records/${id}/toggle`, { method: 'POST' }),
  // 批量操作：删除 / 启用 / 禁用在后端同一事务内完成，避免多次往返产生中间态。
  batchRecords: (
    domainId: number,
    payload: {
      delete_ids?: number[];
      enable_ids?: number[];
      disable_ids?: number[];
    }
  ) =>
    request<{ message: string }>(`/domains/${domainId}/records/batch`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  // DNSSEC
  getDNSSEC: (domainId: number) => request<DNSSECResponse>(`/domains/${domainId}/dnssec`),
  enableDNSSEC: (domainId: number) =>
    request<{ message: string }>(`/domains/${domainId}/dnssec/enable`, { method: 'POST' }),
  disableDNSSEC: (domainId: number) =>
    request<{ message: string }>(`/domains/${domainId}/dnssec/disable`, { method: 'POST' }),
  rotateDNSSECKeys: (domainId: number) =>
    request<{ message: string; keys: DNSSECKey[] }>(`/domains/${domainId}/dnssec/rotate`, {
      method: 'POST',
    }),

  // SSL Certificates
  listCertificates: (domainId?: number) => {
    const url = domainId ? `/certificates?domain_id=${domainId}` : '/certificates';
    return request<{ certificates: SSLCertificate[]; total: number }>(url);
  },
  listACMEProviders: () => request<{ providers: ACMEProvider[] }>('/certificates/providers'),
  issueCertificate: (data: {
    domain_id: number;
    domains?: string[];
    key_type?: string;
    applicant_id?: number;
    provider?: string;
  }) =>
    request<SSLCertificate>('/certificates/issue', { method: 'POST', body: JSON.stringify(data) }),
  renewCertificate: (id: number | string) =>
    request<SSLCertificate>(`/certificates/${id}/renew`, { method: 'POST' }),
  updateCertificateAutoRenew: (id: number | string, autoRenew: boolean) =>
    request<{ message: string; auto_renew: boolean }>(`/certificates/${id}/auto-renew`, {
      method: 'PUT',
      body: JSON.stringify({ auto_renew: autoRenew }),
    }),
  deleteCertificate: (id: number | string) =>
    request<{ message: string }>(`/certificates/${id}`, { method: 'DELETE' }),
  // 证书 / 私钥下载：<a href> 直连不会带 Authorization 头，会被鉴权中间件拦成 401，
  // 因此用 fetch 带上 token 拉取 blob，再由调用方触发浏览器下载。
  downloadCertificate: async (id: number | string, type: 'cert' | 'key') => {
    const token = localStorage.getItem('dnscat_token');
    const res = await fetch(`${BASE_URL}/certificates/${id}/download?type=${type}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) {
      let msg = `Download failed (${res.status})`;
      try {
        const d = await res.json();
        if (d.error) msg = d.error;
      } catch {
        /* keep default message */
      }
      throw new Error(msg);
    }
    const blob = await res.blob();
    const disposition = res.headers.get('Content-Disposition') || '';
    const match = disposition.match(/filename=([^;]+)/i);
    const filename = match
      ? match[1].trim().replace(/["']/g, '')
      : `certificate.${type === 'key' ? 'key' : 'crt'}`;
    return { blob, filename };
  },

  // Certificate Applicant Profiles
  listApplicants: () => request<{ applicants: CertApplicant[] }>('/applicants'),
  createApplicant: (data: Partial<CertApplicant>) =>
    request<CertApplicant>('/applicants', { method: 'POST', body: JSON.stringify(data) }),
  updateApplicant: (id: number, data: Partial<CertApplicant>) =>
    request<CertApplicant>(`/applicants/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteApplicant: (id: number) =>
    request<{ message: string }>(`/applicants/${id}`, { method: 'DELETE' }),

  // DDNS (dynamic DNS update keys)
  listDdnsKeys: (domainId: number) =>
    request<{ ddns_keys: DdnsKey[]; total: number }>(`/domains/${domainId}/ddns-keys`),
  // 明文密钥只在创建与轮换的响应里返回一次，之后无法再取回。
  createDdnsKey: (domainId: number, data: DdnsKeyPayload) =>
    request<{ ddns_key: DdnsKey; key: string; warning: string }>(
      `/domains/${domainId}/ddns-keys`,
      { method: 'POST', body: JSON.stringify(data) }
    ),
  updateDdnsKey: (id: number, data: DdnsKeyPayload) =>
    request<{ message: string }>(`/ddns-keys/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  rotateDdnsKey: (id: number) =>
    request<{ key: string; warning: string }>(`/ddns-keys/${id}/rotate`, { method: 'POST' }),
  deleteDdnsKey: (id: number) =>
    request<{ message: string }>(`/ddns-keys/${id}`, { method: 'DELETE' }),

  // Health Checks
  listHealthChecks: (domainId?: number) => {
    const url = domainId ? `/health-checks?domain_id=${domainId}` : '/health-checks';
    return request<{ health_checks: HealthCheck[]; total: number }>(url);
  },
  createHealthCheck: (data: Partial<HealthCheck>) =>
    request<HealthCheck>('/health-checks', { method: 'POST', body: JSON.stringify(data) }),
  updateHealthCheck: (id: number, data: Partial<HealthCheck>) =>
    request<{ message: string }>(`/health-checks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteHealthCheck: (id: number) =>
    request<{ message: string }>(`/health-checks/${id}`, { method: 'DELETE' }),
  probeHealthCheck: (id: number) =>
    request<{ success: boolean; latency_ms: number; message: string }>(
      `/health-checks/${id}/probe`,
      { method: 'POST' }
    ),

  // Cluster Nodes
  listNodes: () => request<{ nodes: ClusterNode[]; total: number }>('/nodes'),
  createNode: (data: { name: string; ip?: string; region?: string }) =>
    request<ClusterNode>('/nodes', { method: 'POST', body: JSON.stringify(data) }),
  deleteNode: (id: number) =>
    request<{ message: string }>(`/nodes/${id}`, { method: 'DELETE' }),
  getInstallScript: (nodeId: string) =>
    request<{ script: string; master_url: string; node_id: string; token: string }>(
      `/nodes/install-script?node_id=${nodeId}`
    ),
  getNodeUptime: (range?: TrendRange) =>
    request<NodeUptimeResponse>(`/nodes/uptime${range ? `?range=${range}` : ''}`),
  getNodeMetrics: (nodeId: string, range?: TrendRange) =>
    request<NodeMetricsResponse>(
      `/nodes/metrics?node_id=${encodeURIComponent(nodeId)}${range ? `&range=${range}` : ''}`
    ),

  // Nameserver Servers Management
  listNameservers: () => request<{ nameservers: Nameserver[] }>('/nameservers'),
  createNameserver: (data: Partial<Nameserver>) =>
    request<Nameserver>('/nameservers', { method: 'POST', body: JSON.stringify(data) }),
  updateNameserver: (id: number, data: Partial<Nameserver>) =>
    request<Nameserver>(`/nameservers/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteNameserver: (id: number) =>
    request<{ message: string }>(`/nameservers/${id}`, { method: 'DELETE' }),

  // Smart Routing Lines (global geo/continent/ASN line definitions)
  listRoutingLines: () => request<{ lines: RoutingLine[] }>('/routing-lines'),
  createRoutingLine: (data: Partial<RoutingLine>) =>
    request<RoutingLine>('/routing-lines', { method: 'POST', body: JSON.stringify(data) }),
  updateRoutingLine: (id: number, data: Partial<RoutingLine>) =>
    request<RoutingLine>(`/routing-lines/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteRoutingLine: (id: number) =>
    request<{ message: string }>(`/routing-lines/${id}`, { method: 'DELETE' }),

  // System Settings
  getSettings: () => request<{ settings: Record<string, string>; list: any[] }>('/settings'),
  updateSettings: (data: Record<string, string>) =>
    request<{ message: string }>('/settings', { method: 'PUT', body: JSON.stringify(data) }),

  // Stats & Dashboard
  getSummary: () => request<SummaryStats>('/stats/summary'),
  getQPSTrend: (range?: TrendRange) =>
    request<{ points: QPSTrendPoint[]; range: string }>(
      `/stats/qps-trend${range ? `?range=${range}` : ''}`
    ),
  getTypeBreakdown: (range?: TrendRange) =>
    request<{ types: TypeBreakdown[] }>(`/stats/types${range ? `?range=${range}` : ''}`),
  getGeoBreakdown: (range?: TrendRange) =>
    request<{ geo: GeoBreakdown[] }>(`/stats/geo${range ? `?range=${range}` : ''}`),
  getAuditLogs: () => request<{ logs: any[] }>('/stats/audit-logs'),

  // Per-domain Dashboard
  getDomainStats: (domainId: number, range?: TrendRange) =>
    request<DomainStats>(`/domains/${domainId}/stats${range ? `?range=${range}` : ''}`),

  // 域名安全防护
  getSecurity: (domainId: number) =>
    request<SecurityResponse>(`/domains/${domainId}/security`),
  updateSecurity: (domainId: number, data: Partial<SecurityPolicy>) =>
    request<{ message: string; policy: SecurityPolicy }>(`/domains/${domainId}/security`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  // 安全日志：保留上限可达上万条，搜索/筛选/分页全部由服务端完成。
  getSecurityEvents: (
    domainId: number,
    params: {
      page?: number;
      pageSize?: number;
      rule?: string;
      outcome?: '' | 'blocked' | 'allowed';
      search?: string;
    } = {}
  ) => {
    const qs = new URLSearchParams();
    qs.set('page', String(params.page ?? 1));
    qs.set('page_size', String(params.pageSize ?? 20));
    if (params.rule) qs.set('rule', params.rule);
    if (params.outcome) qs.set('outcome', params.outcome);
    if (params.search) qs.set('search', params.search);
    return request<SecurityEventsResponse>(
      `/domains/${domainId}/security/events?${qs.toString()}`
    );
  },
  resetSecurityStats: (domainId: number) =>
    request<{ message: string }>(`/domains/${domainId}/security/reset-stats`, { method: 'POST' }),
  clearSecurityBans: (domainId: number) =>
    request<{ message: string; cleared: number }>(`/domains/${domainId}/security/clear-bans`, {
      method: 'POST',
    }),
};
