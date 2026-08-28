export type RecordType = 
  | 'A' 
  | 'AAAA' 
  | 'CNAME' 
  | 'TXT' 
  | 'MX' 
  | 'NS' 
  | 'SRV' 
  | 'CAA' 
  | 'PTR' 
  | 'SOA' 
  | 'ALIAS' 
  | 'HTTPS' 
  | 'SVCB' 
  | 'TLSA' 
  | 'SSHFP' 
  | 'DS' 
  | 'DNSKEY';

export type GeoLine = string;

export interface DNSRecord {
  id: number;
  domain_id: number;
  name: string;
  type: RecordType;
  value: string;
  ttl: number;
  priority: number;
  weight: number;
  port: number;
  geo_line: GeoLine;
  enabled: boolean;
  comment: string;
  created_at: string;
  updated_at: string;
}

export interface Domain {
  id: number;
  user_id: number;
  name: string;
  status: 'active' | 'pending' | 'paused';
  ns_status: 'verified' | 'pending' | 'unverified';
  primary_ns: string;
  admin_email: string;
  soa_refresh: number;
  soa_retry: number;
  soa_expire: number;
  soa_minimum: number;
  soa_serial: number;
  dnssec_enabled: boolean;
  record_count: number;
  /** 该域名 apex 上实际配置的全部权威 NS（列表接口返回） */
  ns_records?: string[];
  created_at: string;
  updated_at: string;
}

export interface DNSSECKey {
  id: number;
  domain_id: number;
  key_type: 'KSK' | 'ZSK';
  algorithm: number;
  flags: number;
  key_tag: number;
  public_key: string;
  digest_type: number;
  digest: string;
  ds_config: string;
  created_at: string;
}

export interface DNSSECResponse {
  enabled: boolean;
  domain: string;
  ksk: DNSSECKey | null;
  zsk: DNSSECKey | null;
  algorithm: string;
  digest_type: string;
  registrars_guide: {
    key_tag: string;
    algorithm: number;
    digest_type: number;
    digest: string;
    public_key: string;
    ds_record: string;
  };
}

// ACME 账户在 CA 侧的注册状态。申请人必须先成功注册账户才能签发证书。
export type ACMEAccountState = 'unregistered' | 'valid' | 'failed';

export interface CertApplicant {
  id: number;
  user_id: number;
  name: string;
  email: string;
  organization: string;
  provider: string; // "Let's Encrypt" | "ZeroSSL" | "Google PKI" | "Buypass"
  eab_kid: string;
  eab_hmac_key: string;
  is_default: boolean;
  // --- 真实 ACME 账户状态（账户私钥仅存于后端，不下发前端）---
  acme_account_uri: string;
  acme_directory_url: string;
  acme_account_state: ACMEAccountState;
  acme_account_error: string;
  acme_registered_at: string | null;
  created_at: string;
  updated_at: string;
}

// 后端登记的 ACME 服务商及其接入要求，由 /certificates/providers 下发，
// 避免前端硬编码 CA 列表与「哪些服务商必须填 EAB」的规则。
export interface ACMEProvider {
  name: string;
  requires_eab: boolean;
  has_staging: boolean;
  directory_url: string;
}

export interface SSLCertificate {
  id: number;
  uuid: string; // 对外标识，用于下载/续期/删除的 URL，避免暴露自增主键
  domain_id: number;
  applicant_id?: number | null;
  applicant?: CertApplicant;
  name: string;
  domains: string;
  key_type: string;
  cert_pem: string;
  key_pem: string;
  issuer: string;
  valid_from: string | null;
  valid_to: string | null;
  auto_renew: boolean;
  status: 'valid' | 'issuing' | 'expired' | 'failed';
  last_error: string;
  created_at: string;
  updated_at: string;
}

export interface HealthCheck {
  id: number;
  domain_id: number;
  record_id: number;
  record?: DNSRecord;
  name: string;
  protocol: 'HTTP' | 'HTTPS' | 'TCP' | 'PING';
  host: string;
  port: number;
  path: string;
  expected_code: number;
  check_interval_sec: number;
  timeout_sec: number;
  fallback_ip: string;
  status: 'healthy' | 'degraded' | 'down';
  failover_active: boolean;
  consecutive_fails: number;
  last_latency_ms: number;
  last_checked_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ClusterNode {
  id: number;
  node_id: string;
  name: string;
  ip: string;
  region: string;
  secret_token: string;
  is_online: boolean;
  last_heartbeat: string;
  version: string;
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  net_rx_bps: number;
  net_tx_bps: number;
  qps: number; // 最近 60s 滚动窗口的真实瞬时 QPS
  total_queries: number; // 自节点启动以来处理的累计查询数
  latency_ms: number;
  created_at: string;
}

// 单节点性能时序上的一个时间桶。has=false 表示该桶无采样，
// 前端应断开曲线而不是画成 0。
export interface NodeMetricPoint {
  ts: number;
  has: boolean;
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  net_rx_bps: number;
  net_tx_bps: number;
  qps: number;
  latency_ms: number;
  uptime: number;
}

export interface NodeMetricsResponse {
  node: ClusterNode;
  range: TrendRange;
  start: number;
  end: number;
  bucket_seconds: number;
  points: NodeMetricPoint[];
  summary: {
    peak_qps: number;
    peak_latency: number;
    sample_count: number;
    avg_cpu: number;
    avg_memory: number;
  };
}

export interface SummaryStats {
  domain_count: number;
  record_count: number;
  node_count: number;
  online_node_count: number;
  health_check_count: number;
  total_queries: number;
  blocked_queries: number;
  qps: number;
  uptime_percent: number;
}

// 边缘节点在线率监控（Uptime 风格状态条）。
// 时间范围复用 TrendRange：1h/12h/24h/3d/7d/14d/30d。
export type UptimeStatus = 'up' | 'down' | 'partial' | 'none';

// 状态条上的一个时间片。
export interface UptimeBucket {
  ts: number;        // 桶起始时间（Unix 秒）
  status: UptimeStatus;
  uptime: number;    // 该桶在线率 0-100
  up: number;        // 在线采样数
  down: number;      // 离线采样数
  total: number;     // 采样总数
  avg_latency_ms: number;
}

// 单个节点在选定范围内的聚合在线率。
export interface NodeUptime {
  node_id: string;
  name: string;
  ip: string;
  region: string;
  is_online: boolean;
  current_latency_ms: number;
  overall_uptime: number; // 整个范围在线率 0-100
  avg_latency_ms: number;
  sample_count: number;
  buckets: UptimeBucket[];
}

export interface NodeUptimeResponse {
  range: TrendRange;
  start: number;
  end: number;
  bucket_count: number;
  bucket_seconds: number;
  nodes: NodeUptime[];
}

// DDNS 更新凭据。密钥明文不在此结构中：它只在创建与轮换的响应里出现一次，
// 服务端只保存哈希，之后任何接口都取不回来。
export interface DdnsKey {
  id: number;
  uuid: string;
  domain_id: number;
  user_id: number;
  name: string;
  key_prefix: string;
  hostnames: string;
  allowed_ips: string;
  record_ttl: number;
  allow_ipv4: boolean;
  allow_ipv6: boolean;
  auto_create: boolean;
  enabled: boolean;
  last_ipv4: string;
  last_ipv6: string;
  last_client_ip: string;
  last_user_agent: string;
  last_status: string;
  last_used_at: string | null;
  update_count: number;
  reject_count: number;
  created_at: string;
  updated_at: string;
}

export interface DdnsKeyPayload {
  name?: string;
  hostnames?: string;
  allowed_ips?: string;
  record_ttl?: number;
  allow_ipv4?: boolean;
  allow_ipv6?: boolean;
  auto_create?: boolean;
  enabled?: boolean;
}

export interface QPSTrendPoint {
  ts?: number;
  timestamp: string;
  qps: number;
  success: number;
  blocked: number;
}

// 趋势图可选时间区间。
export type TrendRange = '1h' | '12h' | '24h' | '3d' | '7d' | '14d' | '30d';

export interface TypeBreakdown {
  name: string;
  value: number;
}

export interface GeoBreakdown {
  region: string;
  code: string;
  queries: number;
  percent: number;
}

export interface DomainStats {
  domain: {
    id: number;
    name: string;
    status: string;
    ns_status: string;
    dnssec_enabled: boolean;
    created_at: string;
  };
  summary: {
    total_queries: number;
    blocked_queries: number;
    qps: number;
    record_count: number;
    health_check_count: number;
    dnssec_key_count: number;
  };
  points: QPSTrendPoint[];
  types: TypeBreakdown[];
  geo: GeoBreakdown[];
}

export interface RoutingLine {
  id: number;
  key: string;
  name: string;
  description: string;
  continents: string; // 逗号分隔大洲码：AS,EU,NA,SA,AF,OC
  countries: string;  // 逗号分隔 ISO 3166-1 alpha-2 国家码
  asns: string;       // 逗号分隔自治域号
  priority: number;
  enabled: boolean;
  is_builtin: boolean;
  ref_count?: number; // 引用该线路的解析记录数（后端列表接口计算），用于禁止删除在用线路
  created_at: string;
  updated_at: string;
}

export interface Nameserver {
  id: number;
  hostname: string;
  node_id: number | null;
  node?: ClusterNode;
  ipv4: string;
  ipv6: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

// 单条参与智能分线竞争的记录及其判定结果。
export type RoutingCandidateReason =
  | 'selected'
  | 'selected_weighted'
  | 'selected_failover'
  | 'line_not_matched'
  | 'origin_down_no_fallback';

export interface RoutingCandidate {
  record: DNSRecord;
  selected: boolean;
  effective_value: string;
  failover_active: boolean;
  healthy: boolean;
  weight: number;
  share_percent: number;
  reason: RoutingCandidateReason;
}

export interface TestRoutingResult {
  client_ip: string;
  matched_line: string;
  client_line?: string;
  country_code?: string;
  country_name?: string;
  isp?: string;
  source?: string;
  query_name?: string;
  query_type?: string;
  selection_mode?: 'weighted' | 'all' | 'none';
  candidates?: RoutingCandidate[];
  matched_records: DNSRecord[];
  total_matched: number;
  total_candidates?: number;
}

export interface User {
  id: number;
  username: string;
  email: string;
  role: 'admin' | 'user';
  api_key: string;
  // 跟随账号的界面偏好。空字符串表示该账号还没保存过，
  // 此时沿用浏览器本地设置，并在下次切换时写入账号。
  language?: string;
  theme?: string;
  created_at: string;
}




// ---------------------------------------------------------------------------
// 域名安全防护
// ---------------------------------------------------------------------------

/** AXFR 区域传送策略：一律拒绝，或仅放行白名单内的从服务器。 */
export type AXFRPolicy = 'deny' | 'allowlist';

/** ANY 查询策略：正常返回 / RFC 8482 极简应答 / 直接拒绝。 */
export type AnyPolicy = 'allow' | 'minimal' | 'refuse';

/** 单个域名的安全防护策略，与后端 model.DomainSecurityPolicy 一一对应。 */
export interface SecurityPolicy {
  id: number;
  domain_id: number;
  enabled: boolean;

  rate_limit_enabled: boolean;
  rate_limit_qps: number;
  rate_limit_burst: number;

  flood_protection_enabled: boolean;
  flood_threshold_qps: number;
  flood_ban_seconds: number;

  rrl_enabled: boolean;
  rrl_responses_per_sec: number;
  rrl_slip_ratio: number;

  blacklist_enabled: boolean;
  blocked_ips: string;
  blocked_asns: string;
  blocked_countries: string;
  allowed_ips: string;

  axfr_policy: AXFRPolicy;
  axfr_allowed_ips: string;

  qtype_filter_enabled: boolean;
  blocked_qtypes: string;

  any_policy: AnyPolicy;

  zone_qps_limit_enabled: boolean;
  zone_qps_limit: number;

  acl_enabled: boolean;
  acl_allowed_ips: string;

  nx_protection_enabled: boolean;
  nx_threshold_per_min: number;
  nx_ban_seconds: number;

  query_log_enabled: boolean;
  attack_log_enabled: boolean;

  /** 安全日志保留条数上限，事件环按 FIFO 迭代（写满后丢最旧的）。 */
  log_retention_limit: number;

  created_at: string;
  updated_at: string;
}

/** 按域名累计的拦截统计（跨进程重启累计，由后端周期性落库）。 */
export interface SecurityStats {
  domain_id: number;
  total_queries: number;
  blocked_total: number;
  blocked_rate_limit: number;
  blocked_flood: number;
  blocked_rrl: number;
  blocked_blacklist: number;
  blocked_acl: number;
  blocked_axfr: number;
  blocked_qtype: number;
  blocked_any: number;
  blocked_zone_qps: number;
  blocked_nx_abuse: number;
  nxdomain_count: number;
  updated_at: string;
}

/** 一条安全事件：被拦截的查询，或开启查询日志后记录的普通查询。 */
export interface SecurityEvent {
  at: string;
  client_ip: string;
  country: string;
  asn: number;
  qname: string;
  qtype: string;
  rule: string;
  action: string;
  blocked: boolean;
}

/** 安全日志分页响应。total 为命中筛选的条数，stored 为当前实际保留的总条数。 */
export interface SecurityEventsResponse {
  events: SecurityEvent[];
  total: number;
  stored: number;
  page: number;
  page_size: number;
  retention_limit: number;
  rules: string[];
}

export interface SecurityResponse {
  domain: { id: number; name: string };
  policy: SecurityPolicy;
  stats: SecurityStats;
  dnssec_enabled: boolean;
  active_bans: number;
}

/**
 * 运行中服务端二进制的构建信息（GET /api/version）。
 *
 * 值由编译期 ldflags 注入，未注入时 version 为 "dev"。
 * commit 与 date 在本地开发构建下可能为空串。
 */
export interface BuildInfo {
  version: string;
  commit: string;
  date: string;
  go_version: string;
  platform: string;
}
