import React, { useMemo, useState } from 'react';
import { BookOpen, Key, ShieldAlert, Search, Terminal } from 'lucide-react';
import { CodeBox, Modal } from '../components/GeistUI';
import { useI18n } from '../i18n/I18nContext';

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

interface Endpoint {
  method: HttpMethod;
  path: string;
  descZh: string;
  descEn: string;
  /** 主要请求参数 / 请求体字段说明（可选）。 */
  paramsZh?: string;
  paramsEn?: string;
}

interface EndpointGroup {
  titleZh: string;
  titleEn: string;
  descZh: string;
  descEn: string;
  endpoints: Endpoint[];
}

// 方法徽章配色：与 REST 语义对应（读=蓝、写=绿、改=琥珀、删=红）。
const METHOD_STYLES: Record<HttpMethod, string> = {
  GET: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
  POST: 'bg-green-500/10 text-green-500 border-green-500/20',
  PUT: 'bg-amber-500/10 text-amber-600 dark:text-amber-500 border-amber-500/20',
  DELETE: 'bg-red-500/10 text-red-500 border-red-500/20',
};

// 文档示例统一使用保留用途的占位主机名与占位密钥，不引用任何真实部署地址。
// 示例里的 IP 取自 RFC 5737 / RFC 3849 的文档专用地址段，域名取自 RFC 2606 保留域名。
const EXAMPLE_HOST = 'https://dns.example.com';
const KEY_PLACEHOLDER = 'YOUR_API_KEY';

// 路径参数的示例取值：按参数名给出可读的样例，未命中则用 1。
const samplePathValue = (paramName: string): string => {
  const n = paramName.toLowerCase();
  if (n.includes('uuid')) return 'b7f3c1a2-4d5e-4f60-9a12-8c3d5e7f0a1b';
  if (n.includes('node')) return 'edge-01';
  return '1';
};

/** 把 /api/domains/:id/records 这类路径里的占位参数替换成示例值。 */
const resolveExamplePath = (path: string): string =>
  path.replace(/:([A-Za-z_]+)/g, (_m, name: string) => samplePathValue(name));

// 需要请求体的端点示例（键为「方法 空格 路径」）。全部使用文档专用地址与虚构名称。
const BODY_EXAMPLES: Record<string, string> = {
  'POST /api/auth/change-password': '{"old_password":"CurrentPassw0rd","new_password":"N3wStr0ngPassw0rd"}',
  'POST /api/domains': '{"name":"example.com","primary_ns":"ns1.example.net.","admin_email":"admin.example.com."}',
  'PUT /api/domains/:id': '{"status":"active","admin_email":"admin.example.com."}',
  'POST /api/domains/:id/test-routing':
    '{"client_ip":"203.0.113.10","query_name":"www.example.com","query_type":"A"}',
  'POST /api/domains/:id/records':
    '{"name":"www","type":"A","value":"192.0.2.10","ttl":300,"geo_line":"default","weight":100,"enabled":true}',
  'PUT /api/records/:id': '{"value":"192.0.2.20","ttl":600,"enabled":true}',
  'POST /api/domains/:id/records/batch':
    '{"delete_ids":[12,13],"create":[{"name":"api","type":"A","value":"192.0.2.30","ttl":300}]}',
  'POST /api/routing-lines':
    '{"name":"APAC Acceleration","key":"apac-accel","countries":"jp,sg,kr","asns":"4134","priority":100,"enabled":true}',
  'PUT /api/routing-lines/:id': '{"priority":50,"enabled":true}',
  'POST /api/routing-test': '{"client_ip":"198.51.100.25"}',
  'POST /api/certificates/issue':
    '{"domain_id":1,"domains":["example.com","www.example.com"],"key_type":"ECDSAP256","provider":"letsencrypt"}',
  'PUT /api/certificates/:id/auto-renew': '{"auto_renew":true}',
  'POST /api/applicants':
    '{"name":"Ops Team","email":"ops@example.com","organization":"Example Inc.","provider":"letsencrypt","is_default":true}',
  'PUT /api/applicants/:id': '{"email":"ops@example.com","is_default":true}',
  'POST /api/domains/:id/ddns-keys':
    '{"name":"home-router","hostnames":"home","record_ttl":60,"allow_ipv4":true,"allow_ipv6":false,"auto_create":true,"enabled":true}',
  'PUT /api/ddns-keys/:id': '{"hostnames":"home,nas","enabled":true}',
  'POST /api/health-checks':
    '{"domain_id":1,"record_id":10,"name":"origin-http","protocol":"HTTPS","host":"192.0.2.10","port":443,"path":"/healthz","fallback_ip":"192.0.2.11","check_interval_sec":30}',
  'PUT /api/health-checks/:id': '{"enabled":true,"check_interval_sec":60}',
  'POST /api/nodes': '{"name":"Edge Tokyo","ip":"203.0.113.53","region":"AP-Northeast"}',
  'POST /api/nameservers':
    '{"hostname":"ns1.example.net","node_id":1,"ipv4":"203.0.113.53","ipv6":"2001:db8::53","is_active":true}',
  'PUT /api/nameservers/:id': '{"ipv4":"203.0.113.54","is_active":true}',
  'PUT /api/settings': '{"default_ttl":"300","rate_limit":"1000"}',
  'PUT /api/domains/:id/security':
    '{"enabled":true,"rate_limit_enabled":true,"rate_limit_qps":50,"blacklist_enabled":true,"blocked_ips":"198.51.100.0/24","any_policy":"minimal","log_retention_limit":10000}',
};

// GET 端点的示例查询串，便于直接看出可用的过滤参数。
const QUERY_EXAMPLES: Record<string, string> = {
  'GET /api/domains': '?search=example',
  'GET /api/domains/:id/records': '?type=A&search=www',
  'GET /api/certificates': '?domain_id=1',
  'GET /api/certificates/:id/download': '?type=cert',
  'GET /api/nodes/install-script': '?node_id=edge-01',
  'GET /api/nodes/uptime': '?range=24h',
  'GET /api/nodes/metrics': '?node_id=edge-01&range=24h',
  'GET /api/health-checks': '?domain_id=1',
  'GET /api/stats/qps-trend': '?range=24h',
  'GET /api/stats/types': '?range=24h',
  'GET /api/stats/geo': '?range=24h',
  'GET /api/domains/:id/stats': '?range=24h',
  'GET /api/domains/:id/security/events': '?page=1&page_size=20&outcome=blocked',
};

/** 生成某端点的示例 URL、curl 与 PowerShell 调用片段。 */
const buildExample = (ep: Endpoint) => {
  const key = `${ep.method} ${ep.path}`;
  const body = BODY_EXAMPLES[key];
  const query = QUERY_EXAMPLES[key] ?? '';
  const url = `${EXAMPLE_HOST}${resolveExamplePath(ep.path)}${query}`;

  const curlLines = [`curl -X ${ep.method} \\`, `  -H "X-API-Key: ${KEY_PLACEHOLDER}" \\`];
  if (body) {
    curlLines.push('  -H "Content-Type: application/json" \\');
    curlLines.push(`  -d '${body}' \\`);
  }
  curlLines.push(`  "${url}"`);

  const psLines = [`Invoke-RestMethod -Method ${ep.method} \``, `  -Uri "${url}" \``];
  if (body) {
    psLines.push(`  -ContentType "application/json" \``);
    psLines.push(`  -Body '${body}' \``);
  }
  psLines.push(`  -Headers @{ "X-API-Key" = "${KEY_PLACEHOLDER}" }`);

  return { url, body, query, curl: curlLines.join('\n'), powershell: psLines.join('\n') };
};

// 端点清单严格对照后端 cmd/server/main.go 的实际路由注册，
// 全部位于 AuthMiddleware 保护的 protected 组内，因此都支持 X-API-Key 鉴权。
const ENDPOINT_GROUPS: EndpointGroup[] = [
  {
    titleZh: '账户与密钥',
    titleEn: 'Account & Credentials',
    descZh: '查询当前身份、重置 API 密钥与修改登录密码。',
    descEn: 'Inspect the current identity, rotate the API key, and change the password.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/auth/me',
        descZh: '获取当前密钥所属账户信息（用户名、角色、API Key）',
        descEn: 'Return the account owning the current credential',
      },
      {
        method: 'POST',
        path: '/api/auth/regenerate-api-key',
        descZh: '重新生成 API 密钥。旧密钥立即失效，响应中返回新密钥',
        descEn: 'Regenerate the API key. The old key is revoked immediately',
      },
      {
        method: 'PUT',
        path: '/api/auth/preferences',
        descZh: '保存跟随账号的界面偏好（语言 / 主题）。只传需要修改的一项即可',
        descEn: 'Save account-scoped UI preferences (language / theme); send only the field to change',
        paramsZh: 'language=zh-CN|en-US，theme=dark|light（均可选）',
        paramsEn: 'language=zh-CN|en-US, theme=dark|light (both optional)',
      },
      {
        method: 'POST',
        path: '/api/auth/change-password',
        descZh: '修改当前账户登录密码',
        descEn: 'Change the account login password',
        paramsZh: 'old_password, new_password',
        paramsEn: 'old_password, new_password',
      },
    ],
  },
  {
    titleZh: '域名区域',
    titleEn: 'Domain Zones',
    descZh: '托管域名的增删改查、NS 委派校验与解析路由测试。',
    descEn: 'Manage hosted zones, verify NS delegation, and test resolution routing.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/domains',
        descZh: '列出全部托管域名',
        descEn: 'List all hosted domains',
        paramsZh: 'search（可选，按域名模糊搜索）',
        paramsEn: 'search (optional, fuzzy match)',
      },
      {
        method: 'POST',
        path: '/api/domains',
        descZh: '添加托管域名，自动生成 apex 权威 NS 记录与 SOA',
        descEn: 'Add a zone; apex NS records and SOA are generated automatically',
        paramsZh: 'name（必填），primary_ns, admin_email（可选）',
        paramsEn: 'name (required), primary_ns, admin_email (optional)',
      },
      {
        method: 'GET',
        path: '/api/domains/:id',
        descZh: '获取单个域名详情（含解析记录与 DNSSEC 密钥）',
        descEn: 'Get one zone with its records and DNSSEC keys',
      },
      {
        method: 'PUT',
        path: '/api/domains/:id',
        descZh: '更新域名配置',
        descEn: 'Update zone configuration',
      },
      {
        method: 'DELETE',
        path: '/api/domains/:id',
        descZh: '删除域名区域及其全部解析记录、DNSSEC 密钥与探测配置',
        descEn: 'Delete the zone and all of its records, keys, and probes',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/verify-ns',
        descZh: '校验该域名在公网的 NS 指向是否已委派到本系统',
        descEn: 'Verify public NS delegation for this zone',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/test-routing',
        descZh: '按客户端 IP 模拟解析，返回命中的分线与最终应答',
        descEn: 'Simulate resolution for a client IP and return the matched line',
        paramsZh: 'client_ip（必填），query_name, query_type（可选）',
        paramsEn: 'client_ip (required), query_name, query_type (optional)',
      },
    ],
  },
  {
    titleZh: 'DNS 解析记录',
    titleEn: 'DNS Records',
    descZh: '解析记录的增删改查、启停切换与批量操作。支持全部记录类型。',
    descEn: 'Create, update, toggle, and batch-manage records of every supported type.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/domains/:id/records',
        descZh: '列出某域名的解析记录（不含系统自动维护的 apex NS）',
        descEn: 'List records of a zone (system apex NS records excluded)',
        paramsZh: 'type, search, geo_line（均可选，用于过滤）',
        paramsEn: 'type, search, geo_line (all optional filters)',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/records',
        descZh: '新增一条解析记录',
        descEn: 'Create a record',
        paramsZh: 'name, type, value（必填）；ttl, priority, weight, port, geo_line, enabled, comment（可选）',
        paramsEn: 'name, type, value (required); ttl, priority, weight, port, geo_line, enabled, comment (optional)',
      },
      {
        method: 'PUT',
        path: '/api/records/:id',
        descZh: '更新解析记录',
        descEn: 'Update a record',
      },
      {
        method: 'DELETE',
        path: '/api/records/:id',
        descZh: '删除解析记录',
        descEn: 'Delete a record',
      },
      {
        method: 'POST',
        path: '/api/records/:id/toggle',
        descZh: '启用 / 停用解析记录，返回切换后的状态',
        descEn: 'Enable or disable a record; returns the new state',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/records/batch',
        descZh: '批量操作：在一个事务内先删除后新增，失败整体回滚',
        descEn: 'Batch delete-then-create inside a single transaction',
        paramsZh: 'delete_ids（ID 数组），create（记录数组）',
        paramsEn: 'delete_ids (array of IDs), create (array of records)',
      },
    ],
  },
  {
    titleZh: '智能路由分线',
    titleEn: 'Smart Routing Lines',
    descZh: '按大洲 / 国家 / ASN 维度定义分线策略，解析记录通过 geo_line 引用线路 key。',
    descEn: 'Define lines by continent / country / ASN; records reference them via geo_line.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/routing-lines',
        descZh: '列出全部分线线路，含每条线路的引用记录数 ref_count',
        descEn: 'List all lines including ref_count (referencing records)',
      },
      {
        method: 'POST',
        path: '/api/routing-lines',
        descZh: '新建分线线路',
        descEn: 'Create a routing line',
        paramsZh: 'name（必填）；key, description, continents, countries, asns, priority, enabled（可选）',
        paramsEn: 'name (required); key, description, continents, countries, asns, priority, enabled (optional)',
      },
      {
        method: 'PUT',
        path: '/api/routing-lines/:id',
        descZh: '更新分线线路（内置线路的 key 不可修改）',
        descEn: 'Update a line (built-in line keys are locked)',
      },
      {
        method: 'DELETE',
        path: '/api/routing-lines/:id',
        descZh: '删除分线线路。内置线路或仍被解析记录引用的线路会被拒绝',
        descEn: 'Delete a line. Built-in or still-referenced lines are rejected',
      },
      {
        method: 'POST',
        path: '/api/routing-test',
        descZh: '全局路由测试：输入客户端 IP，返回地理定位与命中的线路列表',
        descEn: 'Global routing test: geolocate a client IP and list matched lines',
        paramsZh: 'client_ip（必填）',
        paramsEn: 'client_ip (required)',
      },
    ],
  },
  {
    titleZh: 'DNSSEC 签名',
    titleEn: 'DNSSEC',
    descZh: '按区域启停 DNSSEC、轮换 KSK/ZSK 并获取 DS 记录参数。',
    descEn: 'Toggle DNSSEC per zone, rotate KSK/ZSK, and fetch DS parameters.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/domains/:id/dnssec',
        descZh: '获取该域名的 DNSSEC 状态、密钥与 DS 记录配置',
        descEn: 'Get DNSSEC status, keys, and DS record configuration',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/dnssec/enable',
        descZh: '启用 DNSSEC，自动生成 KSK 与 ZSK',
        descEn: 'Enable DNSSEC and generate KSK/ZSK',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/dnssec/disable',
        descZh: '关闭 DNSSEC',
        descEn: 'Disable DNSSEC',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/dnssec/rotate',
        descZh: '轮换签名密钥',
        descEn: 'Rotate signing keys',
      },
    ],
  },
  {
    titleZh: 'SSL 证书',
    titleEn: 'SSL Certificates',
    descZh: 'ACME 自动签发与续期。签发为异步流程，受理后返回 202,需轮询状态。',
    descEn: 'ACME issuance and renewal. Issuance is async (202 accepted); poll for status.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/certificates',
        descZh: '列出证书',
        descEn: 'List certificates',
        paramsZh: 'domain_id（可选，按域名过滤）',
        paramsEn: 'domain_id (optional filter)',
      },
      {
        method: 'GET',
        path: '/api/certificates/providers',
        descZh: '列出可用 ACME 服务商及其是否强制 EAB',
        descEn: 'List ACME providers and whether each requires EAB',
      },
      {
        method: 'POST',
        path: '/api/certificates/issue',
        descZh: '申请签发证书（异步受理）',
        descEn: 'Request certificate issuance (accepted asynchronously)',
        paramsZh: 'domain_id（必填），domains（SAN 数组），key_type, applicant_id, provider',
        paramsEn: 'domain_id (required), domains (SAN array), key_type, applicant_id, provider',
      },
      {
        method: 'POST',
        path: '/api/certificates/:id/renew',
        descZh: '手动触发续期 / 重试签发',
        descEn: 'Trigger renewal or retry issuance',
      },
      {
        method: 'PUT',
        path: '/api/certificates/:id/auto-renew',
        descZh: '开关该证书的到期自动续期',
        descEn: 'Toggle auto-renewal for this certificate',
        paramsZh: 'auto_renew（布尔）',
        paramsEn: 'auto_renew (boolean)',
      },
      {
        method: 'GET',
        path: '/api/certificates/:id/download',
        descZh: '下载证书或私钥文件',
        descEn: 'Download the certificate or private key',
        paramsZh: 'type=cert 或 type=key',
        paramsEn: 'type=cert or type=key',
      },
      {
        method: 'DELETE',
        path: '/api/certificates/:id',
        descZh: '删除证书',
        descEn: 'Delete a certificate',
      },
      {
        method: 'GET',
        path: '/api/applicants',
        descZh: '列出证书申请人 Profile（含 ACME 账户注册状态）',
        descEn: 'List applicant profiles with ACME account state',
      },
      {
        method: 'POST',
        path: '/api/applicants',
        descZh: '新建申请人 Profile',
        descEn: 'Create an applicant profile',
        paramsZh: 'name, email（必填）；organization, provider, eab_kid, eab_hmac_key, is_default',
        paramsEn: 'name, email (required); organization, provider, eab_kid, eab_hmac_key, is_default',
      },
      {
        method: 'PUT',
        path: '/api/applicants/:id',
        descZh: '更新申请人 Profile',
        descEn: 'Update an applicant profile',
      },
      {
        method: 'DELETE',
        path: '/api/applicants/:id',
        descZh: '删除申请人 Profile',
        descEn: 'Delete an applicant profile',
      },
    ],
  },
  {
    titleZh: '动态 DNS（DDNS）',
    titleEn: 'Dynamic DNS (DDNS)',
    descZh: '管理动态更新凭据。凭据明文只在创建与轮换的响应里返回一次。',
    descEn: 'Manage DDNS credentials. The plaintext key is returned only once.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/domains/:id/ddns-keys',
        descZh: '列出该域名的 DDNS 密钥（仅返回前缀，不含明文）',
        descEn: 'List DDNS keys for a zone (prefix only, no plaintext)',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/ddns-keys',
        descZh: '创建 DDNS 密钥，响应中一次性返回明文密钥',
        descEn: 'Create a DDNS key; the plaintext is returned once',
        paramsZh: 'name, hostnames（必填）；allowed_ips, record_ttl, allow_ipv4, allow_ipv6, auto_create, enabled',
        paramsEn: 'name, hostnames (required); allowed_ips, record_ttl, allow_ipv4, allow_ipv6, auto_create, enabled',
      },
      {
        method: 'PUT',
        path: '/api/ddns-keys/:id',
        descZh: '更新 DDNS 密钥的作用域与开关',
        descEn: 'Update a DDNS key scope and switches',
      },
      {
        method: 'POST',
        path: '/api/ddns-keys/:id/rotate',
        descZh: '轮换密钥，旧密钥立即失效',
        descEn: 'Rotate the key; the old one is revoked immediately',
      },
      {
        method: 'DELETE',
        path: '/api/ddns-keys/:id',
        descZh: '删除 DDNS 密钥',
        descEn: 'Delete a DDNS key',
      },
    ],
  },
  {
    titleZh: '源站健康探测',
    titleEn: 'Health Checks',
    descZh: 'HTTP/HTTPS/TCP/PING 探测与故障自动切换到备用 IP。',
    descEn: 'HTTP/HTTPS/TCP/PING probing with automatic failover to a backup IP.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/health-checks',
        descZh: '列出探测器',
        descEn: 'List probes',
        paramsZh: 'domain_id（可选）',
        paramsEn: 'domain_id (optional)',
      },
      {
        method: 'POST',
        path: '/api/health-checks',
        descZh: '创建探测器',
        descEn: 'Create a probe',
        paramsZh: 'domain_id, record_id, name, protocol, host（必填）；port, path, fallback_ip, check_interval_sec',
        paramsEn: 'domain_id, record_id, name, protocol, host (required); port, path, fallback_ip, check_interval_sec',
      },
      {
        method: 'PUT',
        path: '/api/health-checks/:id',
        descZh: '更新探测器配置',
        descEn: 'Update a probe',
      },
      {
        method: 'DELETE',
        path: '/api/health-checks/:id',
        descZh: '删除探测器',
        descEn: 'Delete a probe',
      },
      {
        method: 'POST',
        path: '/api/health-checks/:id/probe',
        descZh: '立即执行一次探测并返回结果与延迟',
        descEn: 'Run a probe immediately and return the result and latency',
      },
    ],
  },
  {
    titleZh: '边缘节点与权威 NS',
    titleEn: 'Edge Nodes & Nameservers',
    descZh: '集群节点运维、性能时序查询，以及权威 NS 主机名与 Glue 记录管理。',
    descEn: 'Cluster node operations, metrics time series, and nameserver / glue management.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/nodes',
        descZh: '列出边缘节点及其实时指标（qps 为瞬时速率，total_queries 为累计查询数）',
        descEn: 'List edge nodes with live metrics (qps is instantaneous; total_queries cumulative)',
      },
      {
        method: 'POST',
        path: '/api/nodes',
        descZh: '注册边缘节点，返回节点 ID 与接入令牌',
        descEn: 'Register an edge node; returns the node ID and token',
        paramsZh: 'name（必填）；ip, region（可选）',
        paramsEn: 'name (required); ip, region (optional)',
      },
      {
        method: 'DELETE',
        path: '/api/nodes/:id',
        descZh: '移除边缘节点',
        descEn: 'Remove an edge node',
      },
      {
        method: 'GET',
        path: '/api/nodes/install-script',
        descZh: '获取该节点的一键部署脚本',
        descEn: 'Get the one-click install script for a node',
        paramsZh: 'node_id（必填）',
        paramsEn: 'node_id (required)',
      },
      {
        method: 'GET',
        path: '/api/nodes/uptime',
        descZh: '各节点分桶在线率，用于 Uptime 状态条',
        descEn: 'Bucketed uptime per node for status bars',
        paramsZh: 'range=1h|12h|24h|3d|7d|14d|30d',
        paramsEn: 'range=1h|12h|24h|3d|7d|14d|30d',
      },
      {
        method: 'GET',
        path: '/api/nodes/metrics',
        descZh: '单节点性能时序（CPU / 内存 / 磁盘 / 网络 / QPS / 延迟）',
        descEn: 'Per-node metrics time series (CPU / memory / disk / network / QPS / latency)',
        paramsZh: 'node_id（必填），range（可选）',
        paramsEn: 'node_id (required), range (optional)',
      },
      {
        method: 'GET',
        path: '/api/nameservers',
        descZh: '列出权威 NS 主机名及其节点绑定与 Glue 记录',
        descEn: 'List nameservers with node bindings and glue records',
      },
      {
        method: 'POST',
        path: '/api/nameservers',
        descZh: '新增权威 NS',
        descEn: 'Create a nameserver',
        paramsZh: 'hostname（必填）；node_id, ipv4, ipv6, is_active',
        paramsEn: 'hostname (required); node_id, ipv4, ipv6, is_active',
      },
      {
        method: 'PUT',
        path: '/api/nameservers/:id',
        descZh: '更新权威 NS',
        descEn: 'Update a nameserver',
      },
      {
        method: 'DELETE',
        path: '/api/nameservers/:id',
        descZh: '删除权威 NS',
        descEn: 'Delete a nameserver',
      },
    ],
  },
  {
    titleZh: '域名安全防护',
    titleEn: 'Zone Security',
    descZh: '按域名配置速率限制、Flood/RRL、黑名单与 ACL 等防护规则，并查询拦截统计与安全日志。',
    descEn: 'Per-zone rate limiting, flood/RRL, blacklist and ACL rules, plus block statistics and logs.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/domains/:id/security',
        descZh: '获取该域名的安全策略、累计拦截统计与当前封禁数',
        descEn: 'Get the security policy, cumulative block statistics, and active ban count',
      },
      {
        method: 'PUT',
        path: '/api/domains/:id/security',
        descZh: '更新安全策略。只传需要修改的字段，保存后立即下发到全部节点',
        descEn: 'Update the policy. Send only the fields to change; it syncs to all nodes',
        paramsZh:
          'enabled, rate_limit_enabled, rate_limit_qps, flood_threshold_qps, blacklist_enabled, blocked_ips, blocked_asns, blocked_countries, allowed_ips, acl_enabled, axfr_policy, any_policy, log_retention_limit 等',
        paramsEn:
          'enabled, rate_limit_enabled, rate_limit_qps, flood_threshold_qps, blacklist_enabled, blocked_ips, blocked_asns, blocked_countries, allowed_ips, acl_enabled, axfr_policy, any_policy, log_retention_limit, etc.',
      },
      {
        method: 'GET',
        path: '/api/domains/:id/security/events',
        descZh: '分页查询安全日志，支持按规则、拦截结果与关键词筛选',
        descEn: 'Paginated security log with rule, outcome, and keyword filters',
        paramsZh: 'page, page_size, rule, outcome=blocked|allowed, search（均可选）',
        paramsEn: 'page, page_size, rule, outcome=blocked|allowed, search (all optional)',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/security/reset-stats',
        descZh: '清零该域名的攻击统计与安全日志',
        descEn: 'Reset attack statistics and the security log for this zone',
      },
      {
        method: 'POST',
        path: '/api/domains/:id/security/clear-bans',
        descZh: '解除该域名当前的全部临时封禁',
        descEn: 'Clear all active temporary bans for this zone',
      },
    ],
  },
  {
    titleZh: '系统设置与统计',
    titleEn: 'Settings & Analytics',
    descZh: '读写系统设置，查询全局与单域名的解析统计、趋势与审计日志。',
    descEn: 'Read/write system settings and query global or per-zone analytics and audit logs.',
    endpoints: [
      {
        method: 'GET',
        path: '/api/settings',
        descZh: '读取系统设置',
        descEn: 'Read system settings',
      },
      {
        method: 'PUT',
        path: '/api/settings',
        descZh: '更新系统设置（键值对形式提交）',
        descEn: 'Update system settings (submit key-value pairs)',
      },
      {
        method: 'GET',
        path: '/api/stats/summary',
        descZh: '全局汇总：域名数、节点在线数、累计与拦截查询、QPS、在线率',
        descEn: 'Global summary: zones, online nodes, queries, QPS, uptime',
      },
      {
        method: 'GET',
        path: '/api/stats/qps-trend',
        descZh: 'QPS 趋势序列',
        descEn: 'QPS trend series',
        paramsZh: 'range=1h|12h|24h|3d|7d|14d|30d',
        paramsEn: 'range=1h|12h|24h|3d|7d|14d|30d',
      },
      {
        method: 'GET',
        path: '/api/stats/types',
        descZh: '查询类型分布',
        descEn: 'Query type breakdown',
        paramsZh: 'range（可选）',
        paramsEn: 'range (optional)',
      },
      {
        method: 'GET',
        path: '/api/stats/geo',
        descZh: '国家 / 地区流量分布',
        descEn: 'Country / region traffic breakdown',
        paramsZh: 'range（可选）',
        paramsEn: 'range (optional)',
      },
      {
        method: 'GET',
        path: '/api/domains/:id/stats',
        descZh: '单域名统计：趋势、类型与地理分布',
        descEn: 'Per-zone stats: trend, types, and geo breakdown',
        paramsZh: 'range（可选）',
        paramsEn: 'range (optional)',
      },
      {
        method: 'GET',
        path: '/api/stats/carriers',
        descZh: '运营商 / ASN 维度的流量分布',
        descEn: 'Traffic breakdown by carrier / ASN',
        paramsZh: 'range（可选）',
        paramsEn: 'range (optional)',
      },
      {
        method: 'GET',
        path: '/api/stats/audit-logs',
        descZh: '操作审计日志',
        descEn: 'Operation audit logs',
      },
    ],
  },
];

export const ApiDocsPage: React.FC = () => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';

  const [search, setSearch] = useState('');
  // 点击接口路径后展开的详情（调用示例）。
  const [detail, setDetail] = useState<Endpoint | null>(null);

  // 「接口基地址」展示当前部署的真实地址，方便直接复制使用。
  const baseUrl = `${window.location.protocol}//${window.location.host}`;
  // 代码示例一律使用占位主机名与占位密钥，不写入任何真实部署地址或凭据。
  const keyPlaceholder = KEY_PLACEHOLDER;
  const exampleHost = EXAMPLE_HOST;

  // 按路径或描述过滤端点，方便在长清单里快速定位。
  const filteredGroups = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return ENDPOINT_GROUPS;
    return ENDPOINT_GROUPS.map((g) => ({
      ...g,
      endpoints: g.endpoints.filter((e) =>
        `${e.method} ${e.path} ${e.descZh} ${e.descEn}`.toLowerCase().includes(q)
      ),
    })).filter((g) => g.endpoints.length > 0);
  }, [search]);

  const totalEndpoints = ENDPOINT_GROUPS.reduce((sum, g) => sum + g.endpoints.length, 0);
  const matchedEndpoints = filteredGroups.reduce((sum, g) => sum + g.endpoints.length, 0);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* 子页面标题（左）+ 端点计数（右） */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h2 className="text-sm font-semibold text-primary truncate flex items-center gap-2">
          <BookOpen className="w-4 h-4 text-secondary flex-shrink-0" />
          {t('nav.api_docs')}
        </h2>
        <span className="text-[11px] text-tertiary font-mono">
          {isZh ? `共 ${totalEndpoints} 个接口` : `${totalEndpoints} endpoints`}
        </span>
      </div>

      {/* 1. 鉴权说明 */}
      <div className="geist-card p-6 space-y-5">
        <div>
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Key className="w-4 h-4 text-primary" />
            {isZh ? '鉴权方式' : 'Authentication'}
          </h3>
          <p className="text-xs text-secondary mt-1 leading-relaxed">
            {isZh
              ? '在请求头携带 X-API-Key 即可免登录调用下方全部接口，权限与密钥所属账户一致。密钥的查看与重置请前往「系统全局设置」，重置后旧密钥立即失效。'
              : 'Send the X-API-Key header to call every endpoint below without logging in. The credential carries the same permissions as its owning account. View or regenerate the key in System Settings.'}
          </p>
        </div>

        {/* 基础信息 */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs">
          <div className="space-y-1">
            <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
              {isZh ? '接口基地址' : 'Base URL'}
            </div>
            <div className="text-primary font-mono break-all">{baseUrl}/api</div>
          </div>
          <div className="space-y-1">
            <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
              {isZh ? '鉴权方式' : 'Authentication'}
            </div>
            <div className="text-primary font-mono">X-API-Key {isZh ? '请求头' : 'header'}</div>
          </div>
          <div className="space-y-1">
            <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
              {isZh ? '数据格式' : 'Content Type'}
            </div>
            <div className="text-primary font-mono">application/json</div>
          </div>
        </div>

        {/* 安全提示 */}
        <div className="p-3 rounded-sm bg-amber-500/10 border border-amber-500/20 text-xs text-amber-600 dark:text-amber-500 flex items-start gap-2">
          <ShieldAlert className="w-4 h-4 flex-shrink-0 mt-0.5" />
          <span className="leading-relaxed">
            {isZh
              ? 'API 密钥等同于账户口令，拥有该账户的全部操作权限。请仅在服务端或 CI/CD 的加密变量中保存，切勿提交到代码仓库或前端代码；一旦泄露请立即重置。'
              : 'The API key grants the full permissions of its account. Store it only in server-side or CI/CD secrets, never in a repository or frontend code. Regenerate it immediately if exposed.'}
          </span>
        </div>
      </div>

      {/* 2. 调用示例 */}
      <div className="geist-card p-6 space-y-4">
        <div>
          <h3 className="text-sm font-semibold text-primary flex items-center gap-2">
            <Terminal className="w-4 h-4 text-primary" />
            {isZh ? '调用示例' : 'Usage Examples'}
          </h3>
          <p className="text-xs text-secondary mt-0.5">
            {isZh
              ? `示例中的 ${exampleHost} 与 ${keyPlaceholder} 为占位值，请替换为上方的接口基地址和你的真实密钥。`
              : `${exampleHost} and ${keyPlaceholder} are placeholders — replace them with your base URL and real key.`}
          </p>
        </div>

        <div className="space-y-3">
          <div className="space-y-1.5">
            <div className="text-[11px] text-secondary font-mono">
              {isZh ? '列出全部托管域名（curl）' : 'List all hosted zones (curl)'}
            </div>
            <CodeBox
              code={`curl -H "X-API-Key: ${keyPlaceholder}" \\\n  "${exampleHost}/api/domains"`}
            />
          </div>

          <div className="space-y-1.5">
            <div className="text-[11px] text-secondary font-mono">
              {isZh ? '新增一条 A 记录（curl）' : 'Create an A record (curl)'}
            </div>
            <CodeBox
              code={`curl -X POST \\\n  -H "X-API-Key: ${keyPlaceholder}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"name":"www","type":"A","value":"192.0.2.10","ttl":300}' \\\n  "${exampleHost}/api/domains/1/records"`}
            />
          </div>

          <div className="space-y-1.5">
            <div className="text-[11px] text-secondary font-mono">
              {isZh ? '查询解析统计（PowerShell）' : 'Query analytics (PowerShell)'}
            </div>
            <CodeBox
              code={`Invoke-RestMethod -Uri "${exampleHost}/api/stats/summary" \`\n  -Headers @{ "X-API-Key" = "${keyPlaceholder}" }`}
            />
          </div>
        </div>
      </div>

      {/* 3. 接口清单 */}
      <div className="space-y-4">
        <div className="flex flex-col sm:flex-row items-center gap-3">
          <h3 className="text-sm font-semibold text-primary w-full sm:w-auto flex-shrink-0">
            {isZh ? '接口清单' : 'Endpoint Reference'}
          </h3>
          <div className="relative flex-1 w-full">
            <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-tertiary" />
            <input
              type="text"
              placeholder={isZh ? '按路径或功能搜索接口...' : 'Search endpoints by path or description...'}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full h-9 bg-card text-primary text-xs rounded-sm border border-border pl-9 pr-3 focus:outline-none focus:border-primary font-mono"
            />
          </div>
          {search && (
            <span className="text-[11px] text-tertiary font-mono flex-shrink-0">
              {isZh ? `匹配 ${matchedEndpoints} 个` : `${matchedEndpoints} matched`}
            </span>
          )}
        </div>

        {filteredGroups.map((group) => (
          <div key={group.titleEn} className="geist-card overflow-hidden">
            <div className="px-4 py-3 border-b border-border bg-bg-subtle">
              <div className="text-xs font-semibold text-primary">
                {isZh ? group.titleZh : group.titleEn}
              </div>
              <div className="text-[11px] text-tertiary mt-0.5">
                {isZh ? group.descZh : group.descEn}
              </div>
            </div>

            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs font-mono">
                <thead className="border-b border-border text-secondary select-none">
                  <tr>
                    <th className="py-2.5 px-4 font-medium w-20">{isZh ? '方法' : 'Method'}</th>
                    <th className="py-2.5 px-4 font-medium">
                      {isZh ? '路径（点击看示例）' : 'Path (click for example)'}
                    </th>
                    <th className="py-2.5 px-4 font-medium">{isZh ? '说明' : 'Description'}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {group.endpoints.map((ep) => (
                    <tr key={`${ep.method}-${ep.path}`} className="hover:bg-bg-subtle/50 transition-colors">
                      <td className="py-2.5 px-4 align-top">
                        <span
                          className={`inline-block px-1.5 py-0.5 rounded border text-[10px] font-bold ${METHOD_STYLES[ep.method]}`}
                        >
                          {ep.method}
                        </span>
                      </td>
                      <td className="py-2.5 px-4 align-top">
                        {/* 点击路径查看该接口的调用示例 */}
                        <button
                          type="button"
                          onClick={() => setDetail(ep)}
                          title={isZh ? '查看调用示例' : 'View call example'}
                          className="text-left text-primary break-all hover:text-blue-500 hover:underline decoration-dotted underline-offset-2 transition-colors cursor-pointer"
                        >
                          {ep.path}
                        </button>
                      </td>
                      <td className="py-2.5 px-4 align-top text-secondary">
                        <div>{isZh ? ep.descZh : ep.descEn}</div>
                        {(isZh ? ep.paramsZh : ep.paramsEn) && (
                          <div className="text-[10px] text-tertiary mt-1">
                            {isZh ? '参数' : 'Params'}: {isZh ? ep.paramsZh : ep.paramsEn}
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ))}

        {filteredGroups.length === 0 && (
          <div className="geist-card p-8 text-center text-xs text-secondary">
            {isZh ? '没有匹配的接口。' : 'No matching endpoints.'}
          </div>
        )}
      </div>

      {/* 单个接口的调用示例 */}
      <Modal
        isOpen={!!detail}
        onClose={() => setDetail(null)}
        title={isZh ? '调用示例' : 'Call Example'}
        description={detail ? `${detail.method} ${detail.path}` : undefined}
        maxWidth="2xl"
      >
        {detail && (
          <div className="space-y-4 text-xs">
            {/* 方法 + 说明 */}
            <div className="flex items-start gap-2">
              <span
                className={`inline-block px-1.5 py-0.5 rounded border text-[10px] font-bold font-mono flex-shrink-0 ${METHOD_STYLES[detail.method]}`}
              >
                {detail.method}
              </span>
              <p className="text-secondary leading-relaxed">
                {isZh ? detail.descZh : detail.descEn}
              </p>
            </div>

            {/* 参数说明 */}
            {(isZh ? detail.paramsZh : detail.paramsEn) && (
              <div className="space-y-1">
                <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
                  {isZh ? '参数' : 'Parameters'}
                </div>
                <div className="text-secondary font-mono break-all leading-relaxed">
                  {isZh ? detail.paramsZh : detail.paramsEn}
                </div>
              </div>
            )}

            {(() => {
              const ex = buildExample(detail);
              return (
                <>
                  {/* 请求地址 */}
                  <div className="space-y-1">
                    <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
                      {isZh ? '请求地址' : 'Request URL'}
                    </div>
                    <div className="p-2.5 rounded-sm bg-bg-subtle border border-border font-mono text-primary break-all select-all">
                      {detail.method} {ex.url}
                    </div>
                  </div>

                  {/* 请求体 */}
                  {ex.body && (
                    <div className="space-y-1.5">
                      <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
                        {isZh ? '请求体 (JSON)' : 'Request Body (JSON)'}
                      </div>
                      <CodeBox code={ex.body} />
                    </div>
                  )}

                  {/* curl */}
                  <div className="space-y-1.5">
                    <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
                      curl
                    </div>
                    <CodeBox code={ex.curl} />
                  </div>

                  {/* PowerShell */}
                  <div className="space-y-1.5">
                    <div className="text-[11px] uppercase tracking-wide text-tertiary font-mono">
                      PowerShell
                    </div>
                    <CodeBox code={ex.powershell} />
                  </div>
                </>
              );
            })()}

            <div className="p-2.5 rounded-sm bg-bg-subtle border border-border text-[11px] text-tertiary leading-relaxed">
              {isZh
                ? `示例中的 ${EXAMPLE_HOST}、${KEY_PLACEHOLDER} 及各类 ID、IP、域名均为占位值：请把主机名换成上方的接口基地址，把 ${KEY_PLACEHOLDER} 换成你的真实密钥，并把路径中的 ID 换成实际资源 ID。`
                : `${EXAMPLE_HOST}, ${KEY_PLACEHOLDER}, and all IDs, IPs, and domain names above are placeholders. Swap in your base URL, your real key, and the actual resource IDs.`}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};
