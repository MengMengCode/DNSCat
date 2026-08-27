import React, { useState, useEffect } from 'react';
import {
  Plus,
  Download,
  RotateCw,
  Trash2,
  FileCode,
  Users,
  Edit3,
  ShieldCheck,
  Key,
  AlertCircle,
  XCircle,
  ChevronDown,
  Copy,
  Check,
  Terminal,
} from 'lucide-react';
import { Domain, SSLCertificate, CertApplicant, ACMEProvider } from '../types';
import { Button, Input, Badge, Modal, CodeBox, Switch } from '../components/GeistUI';
import { Pagination, usePagination } from '../components/Pagination';
import { api } from '../api/client';
import { useI18n } from '../i18n/I18nContext';
import { useDialog } from '../components/DialogProvider';

interface CertificatesPageProps {
  domain: Domain | null;
  domains: Domain[];
}

const ENCRYPTION_ALGORITHMS = [
  { id: 'ECDSAP256', name: 'ECDSA P-256 (prime256v1)', desc: '极速现代 • 权威推荐 (Recommended)' },
  { id: 'ECDSAP384', name: 'ECDSA P-384 (secp384r1)', desc: '企业级高强度椭圆曲线' },
  { id: 'ECDSAP521', name: 'ECDSA P-521 (secp521r1)', desc: '超高安全级别椭圆曲线' },
  { id: 'ED25519', name: 'Ed25519 (RFC 8410)', desc: '新一代极速现代签名算法' },
  { id: 'RSA2048', name: 'RSA 2048-bit', desc: '经典兼容性最高算法' },
  { id: 'RSA3072', name: 'RSA 3072-bit', desc: '商业级高强度 RSA' },
  { id: 'RSA4096', name: 'RSA 4096-bit', desc: '工业级最高强度 RSA' },
];

// 服务商的展示说明（纯文案，后端不下发）。是否强制 EAB 一律以后端
// /certificates/providers 的 requires_eab 为准，避免前后端规则不一致。
const CA_PROVIDER_DESC: Record<string, { label: string; descZh: string; descEn: string }> = {
  "Let's Encrypt": {
    label: "Let's Encrypt",
    descZh: '全球最大免费权威开源 CA',
    descEn: 'Largest free and open certificate authority',
  },
  ZeroSSL: {
    label: 'ZeroSSL',
    descZh: '90 天免费与商业 ACME，需 EAB 凭证',
    descEn: '90-day free and commercial ACME, requires EAB',
  },
  Buypass: {
    label: 'Buypass Go SSL',
    descZh: '欧洲可信 ACME CA',
    descEn: 'European trusted ACME CA',
  },
};

// 后端返回的证书状态 → i18n key。未收录的值回退显示原始字符串。
const CERT_STATUS_LABEL_KEYS: Record<string, string> = {
  valid: 'certs.status_valid',
  issuing: 'certs.status_issuing',
  expired: 'certs.status_expired',
  failed: 'certs.status_failed',
};

// formatDay 把时间格式化为「年-月-日」。刻意按本地时区取字段而非用 toISOString()，
// 后者会转成 UTC，在东八区可能把日期整体前移一天。
const formatDay = (d: Date): string =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

const MS_PER_DAY = 24 * 60 * 60 * 1000;

interface IssuanceDiagnosis {
  title: string;
  advice: string;
}

// classifyIssuanceError 把 CA / ACME 返回的原始错误归类为「人类可读的原因 + 处理建议」，
// 让签发失败弹窗不再只是一大串技术文本。关键词匹配大小写不敏感，命中第一条即返回；
// 顺序经过安排（CAA / 速率 / EAB 先于更宽泛的 DNS / 网络），都不命中时回退为通用失败。
const classifyIssuanceError = (raw: string, isZh: boolean): IssuanceDiagnosis => {
  const text = (raw || '').toLowerCase();
  const has = (...keys: string[]) => keys.some((k) => text.includes(k));

  if (has('caa')) {
    return {
      title: isZh ? 'CAA 记录阻止了该 CA 签发' : 'CAA record forbids this CA',
      advice: isZh
        ? '该域名的 CAA 记录未授权当前证书颁发机构。请在域名的 CAA 记录中放行该 CA，或改用被授权的 CA 后重试。'
        : "The domain's CAA record does not authorize this CA. Allow this CA in the CAA record, or switch to an authorized CA and retry.",
    };
  }
  if (has('rate limit', 'ratelimited', 'rate_limited', 'too many')) {
    return {
      title: isZh ? '触发了 CA 的速率限制' : 'CA rate limit reached',
      advice: isZh
        ? '短时间内对该组域名签发过于频繁。请稍后再试，或先用测试环境 (staging) / 其他 CA 验证流程。'
        : 'Too many issuances for these domains recently. Wait and retry, or validate via staging / another CA first.',
    };
  }
  if (has('external account', 'externalaccountrequired', 'eab')) {
    return {
      title: isZh ? 'EAB 外部账户绑定无效或缺失' : 'Invalid or missing EAB binding',
      advice: isZh
        ? 'ZeroSSL / Google 等 CA 需要有效的 EAB 凭证。请在申请人 Profile 中核对 EAB Key ID 与 HMAC Key 是否正确。'
        : 'ZeroSSL / Google require a valid EAB. Verify the EAB Key ID and HMAC Key in the applicant profile.',
    };
  }
  if (has('_acme-challenge', 'dns-01', 'dns problem', 'no txt', 'txt record', 'propagation', '传播', 'nxdomain', 'no such host', '校验未通过', '校验失败')) {
    return {
      title: isZh ? 'DNS-01 域名校验未通过' : 'DNS-01 validation failed',
      advice: isZh
        ? 'CA 未能查询到 _acme-challenge TXT 记录。请确认该域名的 NS 已委派到本系统、权威 DNS 公网可达，并稍等 DNS 传播后重试。'
        : 'The CA could not find the _acme-challenge TXT record. Ensure the domain is delegated here and the authoritative DNS is publicly reachable, then retry.',
    };
  }
  if (has('unauthorized', 'forbidden', '403')) {
    return {
      title: isZh ? 'CA 拒绝了本次授权' : 'Authorization rejected by CA',
      advice: isZh
        ? 'CA 认为该账户无权为此域名签发。请检查账户注册状态、EAB 凭证与域名归属后重试。'
        : 'The CA rejected the account for this domain. Check account registration, EAB, and domain ownership, then retry.',
    };
  }
  if (has('注册 acme 账户', 'registration', 'account')) {
    return {
      title: isZh ? 'ACME 账户注册失败' : 'ACME account registration failed',
      advice: isZh
        ? '无法在该 CA 完成账户注册。请检查联络邮箱、EAB 凭证，以及服务器到 CA 的网络连通性。'
        : 'Could not register the ACME account. Check the contact email, EAB, and network connectivity to the CA.',
    };
  }
  if (has('不属于托管区域', 'not in zone')) {
    return {
      title: isZh ? '存在不属于该区域的域名' : 'Domain outside the managed zone',
      advice: isZh
        ? '待签发列表包含不属于当前托管区域的域名，无法写入其挑战记录。请移除这些域名后重试。'
        : 'The list contains names outside this managed zone, whose challenge records cannot be written. Remove them and retry.',
    };
  }
  if (has('connection refused', 'i/o timeout', 'timeout', 'dial tcp', 'no route', 'network is unreachable', 'tls handshake')) {
    return {
      title: isZh ? '无法连接到 CA 服务器' : 'Cannot reach the CA server',
      advice: isZh
        ? '与 CA 的网络通信失败。请检查服务器出网、DNS 解析与防火墙设置后重试。'
        : 'Network communication with the CA failed. Check egress, DNS resolution, and firewall, then retry.',
    };
  }
  return {
    title: isZh ? '证书签发失败' : 'Certificate issuance failed',
    advice: isZh
      ? '签发过程中出现错误。可展开下方技术详情查看 CA 返回的原始信息，据此排查后重试。'
      : 'An error occurred during issuance. Expand the technical details below to see the raw CA response.',
  };
};

export const CertificatesPage: React.FC<CertificatesPageProps> = ({ domain, domains }) => {
  const { t, language } = useI18n();
  const isZh = language === 'zh-CN';
  const { confirm, alert } = useDialog();

  const certStatusLabel = (status: string): string => {
    const key = CERT_STATUS_LABEL_KEYS[status];
    return key ? t(key) : status.toUpperCase();
  };

  const [certs, setCerts] = useState<SSLCertificate[]>([]);
  const [applicants, setApplicants] = useState<CertApplicant[]>([]);
  const [providers, setProviders] = useState<ACMEProvider[]>([]);
  const [loading, setLoading] = useState(true);

  // Modals
  const [isIssueModalOpen, setIsIssueModalOpen] = useState(false);
  const [isApplicantModalOpen, setIsApplicantModalOpen] = useState(false);
  const [isEditApplicantOpen, setIsEditApplicantOpen] = useState(false);
  const [selectedCertForView, setSelectedCertForView] = useState<SSLCertificate | null>(null);
  const [selectedCertForError, setSelectedCertForError] = useState<SSLCertificate | null>(null);

  // 签发失败详情弹窗：原始错误默认折叠，复制后短暂显示「已复制」。
  const [showRawError, setShowRawError] = useState(false);
  const [copiedError, setCopiedError] = useState(false);

  // 判断某服务商是否强制要求 EAB，规则来自后端下发。
  const providerRequiresEAB = (providerName: string): boolean => {
    const target = (providerName || '').trim().toLowerCase();
    return providers.some((p) => p.name.toLowerCase() === target && p.requires_eab);
  };

  // 可选服务商列表：以后端下发为准，附加本地展示文案。
  const providerOptions = providers.map((p) => {
    const meta = CA_PROVIDER_DESC[p.name];
    return {
      id: p.name,
      name: meta?.label || p.name,
      desc: meta ? (isZh ? meta.descZh : meta.descEn) : p.directory_url,
      requiresEAB: p.requires_eab,
    };
  });

  // Issue Certificate Form State
  const [formDomainId, setFormDomainId] = useState<number>(domain?.id || domains[0]?.id || 0);
  const [formApplicantId, setFormApplicantId] = useState<string>('');
  const [formProvider, setFormProvider] = useState<string>("Let's Encrypt");
  // SAN 域名列表以数组形式维护，每一行对应一个待签发的域名 / 通配符。
  // 默认不预填：仅给一个空行，由用户自行填写要签发的域名 / 通配符。
  const [formDomainList, setFormDomainList] = useState<string[]>(['']);
  const [formKeyType, setFormKeyType] = useState<string>('ECDSAP256');
  const [issuing, setIssuing] = useState(false);
  const [issueError, setIssueError] = useState('');
  const [renewingId, setRenewingId] = useState<string | null>(null);
  const [autoRenewUpdatingId, setAutoRenewUpdatingId] = useState<string | null>(null);

  // Applicant Edit Form State
  const [editingApplicant, setEditingApplicant] = useState<CertApplicant | null>(null);
  const [appFormName, setAppFormName] = useState('');
  const [appFormEmail, setAppFormEmail] = useState('');
  const [appFormOrg, setAppFormOrg] = useState('');
  const [appFormProvider, setAppFormProvider] = useState("Let's Encrypt");
  const [appFormEabKid, setAppFormEabKid] = useState('');
  const [appFormEabHmac, setAppFormEabHmac] = useState('');
  const [appFormIsDefault, setAppFormIsDefault] = useState(false);
  const [appSubmitting, setAppSubmitting] = useState(false);

  const loadData = async () => {
    try {
      setLoading(true);
      const [certsRes, applicantsRes, providersRes] = await Promise.all([
        api.listCertificates(domain?.id),
        api.listApplicants(),
        api.listACMEProviders(),
      ]);
      setCerts(certsRes.certificates || []);
      setProviders(providersRes.providers || []);
      const appList = applicantsRes.applicants || [];
      setApplicants(appList);

      const defaultApp = appList.find((a) => a.is_default) || appList[0];
      if (defaultApp && !formApplicantId) {
        setFormApplicantId(String(defaultApp.id));
        setFormProvider(defaultApp.provider || "Let's Encrypt");
      }
    } catch (err: any) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  // 仅刷新证书列表，用于「签发中」状态的轮询，避免整页 loading 闪烁。
  const refreshCerts = async () => {
    try {
      const certsRes = await api.listCertificates(domain?.id);
      setCerts(certsRes.certificates || []);
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    loadData();
  }, [domain?.id]);

  // 签发是异步的后台流程（DNS 传播 + CA 校验，通常 1~3 分钟），
  // 存在「签发中」证书时定时轮询，让状态自动收敛，无需用户手动刷新。
  const hasIssuing = certs.some((c) => c.status === 'issuing');
  useEffect(() => {
    if (!hasIssuing) return;
    const timer = setInterval(refreshCerts, 5000);
    return () => clearInterval(timer);
  }, [hasIssuing, domain?.id]);

  // 新建 / 编辑申请人后，后端会在后台注册 ACME 账户。当申请人管理弹窗打开且
  // 存在「未注册」申请人时短期轮询刷新，让账户状态自动收敛为已注册 / 失败，
  // 无需用户手动刷新；收敛后（无 unregistered）自动停止。
  const hasUnregisteredApplicant = applicants.some(
    (a) => a.acme_account_state === 'unregistered'
  );
  useEffect(() => {
    if (!isApplicantModalOpen || !hasUnregisteredApplicant) return;
    const timer = setInterval(async () => {
      try {
        const res = await api.listApplicants();
        setApplicants(res.applicants || []);
      } catch {
        /* 轮询失败忽略，下次继续 */
      }
    }, 3000);
    return () => clearInterval(timer);
  }, [isApplicantModalOpen, hasUnregisteredApplicant]);

  useEffect(() => {
    if (domain) {
      setFormDomainId(domain.id);
    } else if (domains.length > 0) {
      setFormDomainId(domains[0].id);
    }
    // 不再按域名预填 SAN 列表：由用户自行填写。
  }, [domain, domains]);

  const handleOpenIssue = () => {
    // 不预填域名：每次打开签发弹窗都重置为一个空行，由用户自行填写 SAN。
    setFormDomainList(['']);
    setIssueError('');
    setIsIssueModalOpen(true);
  };

  // SAN 表格行操作：新增空行、更新指定行、删除指定行（至少保留一行）。
  const handleAddDomainRow = () => setFormDomainList((prev) => [...prev, '']);
  const handleUpdateDomainRow = (index: number, value: string) =>
    setFormDomainList((prev) => prev.map((item, i) => (i === index ? value : item)));
  const handleRemoveDomainRow = (index: number) =>
    setFormDomainList((prev) => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== index)));

  const handleApplicantSelect = (appIdStr: string) => {
    setFormApplicantId(appIdStr);
    if (appIdStr) {
      const selected = applicants.find((a) => String(a.id) === appIdStr);
      if (selected && selected.provider) {
        setFormProvider(selected.provider);
      }
    }
  };

  const handleIssue = async (e: React.FormEvent) => {
    e.preventDefault();
    setIssueError('');
    setIssuing(true);

    try {
      // 去除空行与前后空格，并按大小写不敏感去重，避免重复 SAN。
      const seen = new Set<string>();
      const dList = formDomainList
        .map((s) => s.trim())
        .filter((s) => {
          if (!s) return false;
          const key = s.toLowerCase();
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });

      if (dList.length === 0) {
        setIssueError(isZh ? '请至少填写一个有效的域名' : 'Please provide at least one valid domain');
        setIssuing(false);
        return;
      }

      const parsedAppId = formApplicantId ? Number(formApplicantId) : undefined;

      await api.issueCertificate({
        domain_id: Number(formDomainId),
        domains: dList,
        key_type: formKeyType,
        applicant_id: parsedAppId,
        provider: formProvider,
      });

      setIsIssueModalOpen(false);
      await loadData();
      // 后端以 202 受理，真实签发在后台进行，这里只提示已受理。
      await alert({ variant: 'info', message: t('certs.issuing_hint') });
    } catch (err: any) {
      setIssueError(err.message);
    } finally {
      setIssuing(false);
    }
  };

  const handleRenew = async (id: string) => {
    try {
      setRenewingId(id);
      await api.renewCertificate(id);
      await loadData();
      // 续期同为异步：新证书就绪后才会替换旧证书，因此这里不能报「已完成」。
      await alert({ variant: 'info', message: t('certs.issuing_hint') });
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setRenewingId(null);
    }
  };

  const handleAutoRenewChange = async (cert: SSLCertificate, enabled: boolean) => {
    try {
      setAutoRenewUpdatingId(cert.uuid);
      const res = await api.updateCertificateAutoRenew(cert.uuid, enabled);
      setCerts((current) => current.map((item) =>
        item.uuid === cert.uuid ? { ...item, auto_renew: res.auto_renew } : item
      ));
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    } finally {
      setAutoRenewUpdatingId(null);
    }
  };

  const handleDelete = async (id: string) => {
    const ok = await confirm({
      variant: 'danger',
      message: t('common.confirm_delete'),
      confirmText: t('common.delete'),
    });
    if (ok) {
      try {
        await api.deleteCertificate(id);
        loadData();
      } catch (err: any) {
        await alert({ variant: 'danger', message: err.message });
      }
    }
  };

  // 证书 / 私钥下载：经鉴权 fetch 取回文件内容后再触发浏览器下载，
  // 避免 <a href> 直连因缺少 Authorization 头被拦成 401。
  const handleDownload = async (cert: SSLCertificate, type: 'cert' | 'key') => {
    try {
      const { blob, filename } = await api.downloadCertificate(cert.uuid, type);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  // Applicant Profile Handlers
  const handleOpenAddApplicant = () => {
    setEditingApplicant(null);
    // 名称 / 邮箱 / 组织三个字段不预填，由用户自行填写（保留输入框 placeholder 作提示）。
    setAppFormName('');
    setAppFormEmail('');
    setAppFormOrg('');
    setAppFormProvider("Let's Encrypt");
    setAppFormEabKid('');
    setAppFormEabHmac('');
    setAppFormIsDefault(applicants.length === 0);
    setIsEditApplicantOpen(true);
  };

  const handleOpenEditApplicant = (app: CertApplicant) => {
    setEditingApplicant(app);
    setAppFormName(app.name);
    setAppFormEmail(app.email);
    setAppFormOrg(app.organization || '');
    setAppFormProvider(app.provider || "Let's Encrypt");
    setAppFormEabKid(app.eab_kid || '');
    setAppFormEabHmac(app.eab_hmac_key || '');
    setAppFormIsDefault(app.is_default);
    setIsEditApplicantOpen(true);
  };

  const handleSaveApplicant = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!appFormName.trim() || !appFormEmail.trim()) {
      await alert({
        variant: 'warning',
        message: isZh ? '请填写申请人名称与邮箱' : 'Please input name and email',
      });
      return;
    }

    // ZeroSSL / Google 缺少 EAB 时不必发请求：后端也会拒绝，
    // 在前端拦下可以少一次往返并把提示直接落在表单上。
    if (providerRequiresEAB(appFormProvider) &&
        (!appFormEabKid.trim() || !appFormEabHmac.trim())) {
      await alert({ variant: 'warning', message: t('certs.eab_required') });
      return;
    }

    try {
      setAppSubmitting(true);
      const payload: Partial<CertApplicant> = {
        name: appFormName.trim(),
        email: appFormEmail.trim(),
        organization: appFormOrg.trim(),
        provider: appFormProvider,
        eab_kid: appFormEabKid.trim(),
        eab_hmac_key: appFormEabHmac.trim(),
        is_default: appFormIsDefault,
      };

      if (editingApplicant) {
        await api.updateApplicant(editingApplicant.id, payload);
      } else {
        await api.createApplicant(payload);
      }

      setIsEditApplicantOpen(false);
      const applicantsRes = await api.listApplicants();
      setApplicants(applicantsRes.applicants || []);
    } catch (err: any) {
      await alert({
        variant: 'danger',
        message:
          err.message || (isZh ? '保存申请人 Profile 失败' : 'Failed to save applicant profile'),
      });
    } finally {
      setAppSubmitting(false);
    }
  };

  const handleDeleteApplicant = async (id: number, name: string) => {
    const conf = await confirm({
      variant: 'danger',
      message: isZh
        ? `确定要删除证书申请人 Profile [${name}] 吗？`
        : `Are you sure you want to delete applicant [${name}]?`,
      confirmText: t('common.delete'),
    });
    if (!conf) return;

    try {
      await api.deleteApplicant(id);
      const applicantsRes = await api.listApplicants();
      setApplicants(applicantsRes.applicants || []);
    } catch (err: any) {
      await alert({ variant: 'danger', message: err.message });
    }
  };

  // 证书列表已全量加载到前端，这里做客户端分页，避免证书较多时表格过长。
  const pager = usePagination(certs, 10);

  return (
    <div className="space-y-6 animate-in fade-in duration-150">
      {/* Toolbar */}
      {/* 子页面标题（左）+ 操作按钮（右）：行高由按钮决定，标题不额外撑高 */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h2 className="text-sm font-semibold text-primary truncate">{t('nav.certificates')}</h2>
        <div className="flex items-center gap-2 flex-wrap">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setIsApplicantModalOpen(true)}
            icon={<Users className="w-3.5 h-3.5" />}
          >
            {isZh ? '证书申请人管理' : 'Applicant Profiles'}
          </Button>

          <Button
            size="sm"
            variant="primary"
            onClick={handleOpenIssue}
            icon={<Plus className="w-3.5 h-3.5" />}
          >
            {t('certs.issue_btn')}
          </Button>
        </div>
      </div>

      {/* Native HTML Table for Issued SSL Certificates */}
      <div className="geist-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
              <tr>
                <th className="py-3 px-4 font-medium">{t('certs.col_name')}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '申请人 Profile' : 'Applicant'}</th>
                <th className="py-3 px-4 font-medium">{isZh ? '加密算法 & 颁发机构' : 'Algorithm & CA'}</th>
                <th className="py-3 px-4 font-medium">{t('certs.col_validity')}</th>
                <th className="py-3 px-4 font-medium">{t('certs.col_auto_renew')}</th>
                <th className="py-3 px-4 font-medium">{t('certs.col_status')}</th>
                <th className="py-3 px-4 font-medium text-right">{t('certs.col_actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {pager.pageItems.map((c) => {
                const now = new Date();
                const expDate = c.valid_to ? new Date(c.valid_to) : null;
                const startDate = c.valid_from ? new Date(c.valid_from) : null;
                const msLeft = expDate ? expDate.getTime() - now.getTime() : 0;
                const expired = expDate !== null && msLeft <= 0;
                const daysLeft = expDate ? Math.max(0, Math.ceil(msLeft / MS_PER_DAY)) : 0;
                // 倒数条按该证书自身的有效周期折算剩余占比：各 CA 有效期并不相同
                // （Let's Encrypt 90 天，商业证书常为 1 年），固定按 90 天算会失真。
                const totalDays =
                  startDate && expDate
                    ? Math.max(1, Math.round((expDate.getTime() - startDate.getTime()) / MS_PER_DAY))
                    : 90;
                const remainPct = Math.min(100, Math.max(0, (daysLeft / totalDays) * 100));

                return (
                  <tr key={c.id} className="hover:bg-bg-subtle/50 transition-colors">
                    {/* Domain & Names */}
                    <td className="py-3 px-4">
                      <div className="space-y-0.5">
                        <div className="font-bold text-primary">{c.name}</div>
                        <div className="text-[11px] text-tertiary truncate max-w-xs">{c.domains}</div>
                      </div>
                    </td>

                    {/* Applicant Profile */}
                    <td className="py-3 px-4 text-secondary">
                      {c.applicant ? (
                        <div className="space-y-0.5">
                          <div className="font-semibold text-primary">{c.applicant.name}</div>
                          <div className="text-[10px] text-tertiary">{c.applicant.email}</div>
                        </div>
                      ) : (
                        <span className="text-tertiary">{isZh ? '系统默认' : 'Default'}</span>
                      )}
                    </td>

                    {/* Algorithm & CA Issuer */}
                    <td className="py-3 px-4">
                      <div className="space-y-0.5">
                        <span className="font-bold text-primary px-1.5 py-0.5 rounded bg-bg-subtle border border-border">
                          {c.key_type}
                        </span>
                        <div className="text-[10px] text-tertiary">{c.issuer}</div>
                      </div>
                    </td>

                    {/* Validity & Progress */}
                    <td className="py-3 px-4">
                      {expDate ? (
                        <div className="space-y-1.5 w-36" title={expDate.toLocaleString()}>
                          <div className="flex items-baseline justify-between gap-2 text-[11px]">
                            {/* 到期日期（年-月-日）+ 剩余天数倒数 */}
                            <span className="text-primary font-bold">{formatDay(expDate)}</span>
                            <span
                              className={
                                expired || daysLeft <= 10
                                  ? 'text-red-500 font-bold'
                                  : daysLeft <= 30
                                    ? 'text-amber-500 font-bold'
                                    : 'text-secondary'
                              }
                            >
                              {expired
                                ? (isZh ? '已过期' : 'Expired')
                                : (isZh ? `剩 ${daysLeft}d` : `${daysLeft}d left`)}
                            </span>
                          </div>
                          <div className="w-full h-1.5 bg-bg-subtle rounded-full overflow-hidden">
                            <div
                              // 已过期铺满红条，未过期时保留最小可见宽度，避免临期看起来像空条
                              style={{ width: `${expired ? 100 : Math.max(2, remainPct)}%` }}
                              className={`h-full rounded-full transition-all ${
                                expired || daysLeft <= 10
                                  ? 'bg-red-500'
                                  : daysLeft <= 30
                                    ? 'bg-amber-500'
                                    : 'bg-green-500'
                              }`}
                            />
                          </div>
                        </div>
                      ) : (
                        // 尚未签发完成的证书没有有效期，显示占位符而不是误导性的 "0 天"。
                        <span className="text-tertiary">—</span>
                      )}
                    </td>

                    {/* Auto Renew */}
                    <td className="py-3 px-4">
                      <div
                        className="flex items-center gap-2"
                        title={isZh ? '控制这张证书是否在到期前自动续期' : 'Automatically renew this certificate before expiry'}
                      >
                        <Switch
                          checked={c.auto_renew}
                          onChange={(enabled) => handleAutoRenewChange(c, enabled)}
                          disabled={autoRenewUpdatingId === c.uuid}
                          size="sm"
                        />
                        <span className={c.auto_renew ? 'text-green-500 font-semibold' : 'text-tertiary'}>
                          {c.auto_renew ? t('certs.auto_renew_on') : t('certs.auto_renew_off')}
                        </span>
                      </div>
                    </td>

                    {/* Status */}
                    <td className="py-3 px-4">
                      <div className="flex items-center gap-1.5">
                        <Badge
                          variant={
                            c.status === 'valid'
                              ? 'success'
                              : c.status === 'failed'
                              ? 'error'
                              : c.status === 'issuing'
                              ? 'info'
                              : 'warning'
                          }
                          size="sm"
                        >
                          {c.status === 'issuing' && (
                            <RotateCw className="w-3 h-3 animate-spin" />
                          )}
                          {certStatusLabel(c.status)}
                        </Badge>
                        {/* 失败原因是排查签发问题的关键信息，必须能在列表里直接看到。 */}
                        {c.last_error && (
                          <button
                            type="button"
                            onClick={() => setSelectedCertForError(c)}
                            className="p-1 rounded hover:bg-red-500/10 text-red-500 transition-colors cursor-pointer"
                            title={t('certs.view_error')}
                          >
                            <AlertCircle className="w-3.5 h-3.5" />
                          </button>
                        )}
                      </div>
                    </td>

                    {/* Actions */}
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <button
                          onClick={() => handleRenew(c.uuid)}
                          disabled={renewingId === c.uuid}
                          className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer disabled:opacity-50"
                          title={t('certs.renew_btn')}
                        >
                          <RotateCw className={`w-3.5 h-3.5 ${renewingId === c.uuid ? 'animate-spin' : ''}`} />
                        </button>
                        {/* 未签发完成时没有证书内容，禁用下载与查看避免拿到空文件。 */}
                        {c.cert_pem ? (
                          <>
                            <button
                              type="button"
                              onClick={() => handleDownload(c, 'cert')}
                              className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                              title={isZh ? '下载证书文件 (.crt)' : 'Download Certificate (.crt)'}
                            >
                              <Download className="w-3.5 h-3.5" />
                            </button>
                            <button
                              type="button"
                              onClick={() => handleDownload(c, 'key')}
                              className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                              title={isZh ? '下载私钥文件 (.key)' : 'Download Private Key (.key)'}
                            >
                              <Key className="w-3.5 h-3.5 text-amber-500" />
                            </button>
                          </>
                        ) : (
                          <span
                            className="p-1.5 rounded border border-border text-tertiary opacity-40 cursor-not-allowed"
                            title={t('certs.pending_no_download')}
                          >
                            <Download className="w-3.5 h-3.5" />
                          </span>
                        )}
                        <button
                          onClick={() => setSelectedCertForView(c)}
                          disabled={!c.cert_pem}
                          className="p-1.5 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
                          title={t('certs.view_pem')}
                        >
                          <FileCode className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDelete(c.uuid)}
                          className="p-1.5 rounded hover:bg-red-500/10 border border-border text-secondary hover:text-red-500 transition-colors cursor-pointer"
                          title={t('common.delete')}
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}

              {certs.length === 0 && (
                <tr>
                  <td colSpan={7} className="text-center text-secondary py-8">
                    {loading ? t('common.loading') : t('certs.no_certs')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <Pagination
          page={pager.page}
          pageSize={pager.pageSize}
          total={pager.total}
          totalPages={pager.totalPages}
          from={pager.from}
          to={pager.to}
          onPageChange={pager.setPage}
          onPageSizeChange={pager.setPageSize}
        />
      </div>

      {/* 1. Applicant Profiles Management Modal */}
      <Modal
        isOpen={isApplicantModalOpen}
        onClose={() => setIsApplicantModalOpen(false)}
        title={isZh ? '证书申请人 Profile 管理' : 'Certificate Applicant Profiles'}
        maxWidth="3xl"
      >
        <div className="space-y-4 font-mono text-xs">
          <div className="flex items-center justify-between">
            <span className="font-bold text-primary">
              {isZh ? '申请人列表' : 'Profiles List'}
            </span>
            <Button
              size="sm"
              variant="primary"
              onClick={handleOpenAddApplicant}
              icon={<Plus className="w-3 h-3" />}
            >
              {isZh ? '添加申请人' : 'Add Profile'}
            </Button>
          </div>

          <div className="overflow-auto max-h-[55vh] border border-border rounded-sm">
            <table className="w-full text-left text-xs">
              <thead className="bg-bg-subtle border-b border-border text-secondary sticky top-0 z-10">
                <tr>
                  <th className="py-2.5 px-3 font-medium">{isZh ? '申请人名称' : 'Profile Name'}</th>
                  <th className="py-2.5 px-3 font-medium">{isZh ? '联系邮箱' : 'Email'}</th>
                  <th className="py-2.5 px-3 font-medium">{isZh ? '默认服务商' : 'Provider'}</th>
                  <th className="py-2.5 px-3 font-medium">{t('certs.acme_account')}</th>
                  <th className="py-2.5 px-3 font-medium">{isZh ? '默认' : 'Default'}</th>
                  <th className="py-2.5 px-3 font-medium text-right">{isZh ? '操作' : 'Actions'}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {applicants.map((app) => (
                  <tr key={app.id} className="hover:bg-bg-subtle/50">
                    <td className="py-2.5 px-3 font-bold text-primary">
                      {app.name}
                    </td>
                    <td className="py-2.5 px-3 text-secondary">
                      {app.email}
                    </td>
                    <td className="py-2.5 px-3 text-primary">
                      <span className="px-1.5 py-0.5 rounded bg-bg-subtle border border-border text-[11px]">
                        {app.provider || "Let's Encrypt"}
                      </span>
                    </td>
                    {/* ACME 账户状态：申请人必须先在 CA 注册成功才能签发证书，
                        注册失败时把原因一并显示，否则用户无从判断为何签不出来。 */}
                    <td className="py-2.5 px-3">
                      {app.acme_account_state === 'valid' ? (
                        <Badge variant="success" size="sm">{t('certs.acme_state_valid')}</Badge>
                      ) : app.acme_account_state === 'failed' ? (
                        <div className="space-y-1">
                          <Badge variant="error" size="sm">{t('certs.acme_state_failed')}</Badge>
                          {app.acme_account_error && (
                            <div
                              className="text-[10px] text-red-500 max-w-[220px] truncate"
                              title={app.acme_account_error}
                            >
                              {app.acme_account_error}
                            </div>
                          )}
                        </div>
                      ) : (
                        <span
                          className="text-tertiary text-[11px]"
                          title={t('certs.acme_state_hint_unregistered')}
                        >
                          {t('certs.acme_state_unregistered')}
                        </span>
                      )}
                    </td>
                    <td className="py-2.5 px-3">
                      {app.is_default && (
                        <Badge variant="success" size="sm">
                          {isZh ? '默认' : 'Default'}
                        </Badge>
                      )}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => handleOpenEditApplicant(app)}
                          className="p-1 rounded hover:bg-card border border-border text-secondary hover:text-primary transition-colors cursor-pointer"
                        >
                          <Edit3 className="w-3 h-3" />
                        </button>
                        <button
                          onClick={() => handleDeleteApplicant(app.id, app.name)}
                          className="p-1 rounded hover:bg-red-500/10 border border-border text-secondary hover:text-red-500 transition-colors cursor-pointer"
                        >
                          <Trash2 className="w-3 h-3" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}

                {applicants.length === 0 && (
                  <tr>
                    <td colSpan={6} className="text-center text-secondary py-6">
                      {isZh ? '暂无申请人 Profile，请点击右上角添加。' : 'No applicant profiles created yet.'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="flex justify-end pt-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsApplicantModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* 2. Applicant Profile Create/Edit Sub-Modal */}
      <Modal
        isOpen={isEditApplicantOpen}
        onClose={() => setIsEditApplicantOpen(false)}
        title={editingApplicant ? (isZh ? '编辑申请人 Profile' : 'Edit Applicant Profile') : (isZh ? '添加证书申请人 Profile' : 'Add Applicant Profile')}
        maxWidth="2xl"
      >
        <form onSubmit={handleSaveApplicant} className="space-y-3.5 font-mono text-xs">
          <Input
            label={isZh ? '申请人名称 / 团队标识' : 'Profile Name / Team'}
            value={appFormName}
            onChange={(e) => setAppFormName(e.target.value)}
            placeholder="默认自动化运维安全组"
            required
            autoFocus
          />

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input
              label={isZh ? '联系联络邮箱 (接收到期通知)' : 'Contact Email'}
              type="email"
              value={appFormEmail}
              onChange={(e) => setAppFormEmail(e.target.value)}
              placeholder="admin@example.com"
              required
            />
            <Input
              label={isZh ? '所属组织 / 公司名称' : 'Organization Name'}
              value={appFormOrg}
              onChange={(e) => setAppFormOrg(e.target.value)}
              placeholder="DnsCat Security Team"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-secondary block">
              {isZh ? '默认 ACME 证书颁发服务商' : 'Default ACME Provider'}
            </label>
            <select
              value={appFormProvider}
              onChange={(e) => setAppFormProvider(e.target.value)}
              className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
            >
              {providerOptions.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} — {p.desc}
                </option>
              ))}
            </select>
          </div>

          {/* EAB 凭证：是否必填由后端下发的 requires_eab 决定，而非前端硬编码服务商名。 */}
          {providerRequiresEAB(appFormProvider) && (
            <div className="p-3 bg-amber-500/5 border border-amber-500/30 rounded-sm space-y-2.5">
              <div className="text-[11px] font-bold text-amber-600 dark:text-amber-500">
                {t('certs.eab_required')}
              </div>
              <Input
                label="EAB Key ID (KID)"
                value={appFormEabKid}
                onChange={(e) => setAppFormEabKid(e.target.value)}
                placeholder="ZeroSSL / Google PKI EAB Key ID"
                required
              />
              <Input
                label="EAB HMAC Key"
                value={appFormEabHmac}
                onChange={(e) => setAppFormEabHmac(e.target.value)}
                placeholder="ZeroSSL / Google PKI EAB HMAC Key"
                required
              />
            </div>
          )}

          <div className="flex items-center gap-2 pt-1">
            <input
              type="checkbox"
              id="app_default"
              checked={appFormIsDefault}
              onChange={(e) => setAppFormIsDefault(e.target.checked)}
              className="rounded border-border text-primary focus:ring-0"
            />
            <label htmlFor="app_default" className="text-xs text-primary font-medium cursor-pointer">
              {isZh ? '设为默认证书申请人 Profile' : 'Set as default applicant profile'}
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-3 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsEditApplicantOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={appSubmitting}
            >
              {t('common.save')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* 3. Issue SSL Certificate Modal */}
      <Modal
        isOpen={isIssueModalOpen}
        onClose={() => setIsIssueModalOpen(false)}
        title={t('certs.issue_modal_title')}
        maxWidth="2xl"
      >
        <form onSubmit={handleIssue} className="space-y-4 font-mono text-xs">
          {issueError && (
            <div className="p-3 rounded bg-red-500/10 border border-red-500/20 text-red-500">
              {issueError}
            </div>
          )}

          {/* Domain Selection if not locked */}
          {!domain && domains.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-secondary block">
                {t('common.select_domain')}
              </label>
              <select
                value={formDomainId}
                onChange={(e) => {
                  // 切换托管域名时不覆盖用户已填写的 SAN 列表。
                  setFormDomainId(Number(e.target.value));
                }}
                className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
              >
                {domains.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* Applicant Profile Selector */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-secondary block">
                {isZh ? '选择申请人 Profile' : 'Select Applicant Profile'}
              </label>
              <button
                type="button"
                onClick={() => {
                  setIsIssueModalOpen(false);
                  setIsApplicantModalOpen(true);
                }}
                className="text-blue-500 hover:underline text-[11px]"
              >
                {isZh ? '管理申请人' : 'Manage Profiles'}
              </button>
            </div>
            <select
              value={formApplicantId}
              onChange={(e) => handleApplicantSelect(e.target.value)}
              className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
            >
              {applicants.map((app) => (
                <option key={app.id} value={app.id}>
                  {app.name} ({app.email} - {app.provider}) {app.is_default ? (isZh ? '★ 默认' : '★ Default') : ''}
                </option>
              ))}
            </select>
          </div>

          {/* CA Provider Selection */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-secondary block">
              {isZh ? '证书颁发机构 (CA Provider)' : 'Certificate Authority (CA Provider)'}
            </label>
            <select
              value={formProvider}
              onChange={(e) => setFormProvider(e.target.value)}
              className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
            >
              {providerOptions.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} — {p.desc}
                </option>
              ))}
            </select>
            {/* 换用需要 EAB 的 CA 时，若所选申请人没有 EAB 凭证，提前给出提示：
                否则请求会被后端拒绝，用户不易定位原因。 */}
            {providerRequiresEAB(formProvider) &&
              (() => {
                const selected = applicants.find((a) => String(a.id) === formApplicantId);
                if (selected && (!selected.eab_kid || !selected.eab_hmac_key)) {
                  return (
                    <p className="text-[11px] text-amber-600 dark:text-amber-500">
                      {t('certs.eab_required')}
                    </p>
                  );
                }
                return null;
              })()}
          </div>

          {/* SAN 域名列表：表格化管理，可逐行增删 */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-secondary block">
                {t('certs.form_domains')}
              </label>
              <span className="text-[11px] text-tertiary">
                {isZh ? `共 ${formDomainList.length} 个域名` : `${formDomainList.length} name(s)`}
              </span>
            </div>

            <div className="border border-border rounded-sm overflow-hidden">
              <table className="w-full text-left text-xs">
                <thead className="bg-bg-subtle border-b border-border text-secondary select-none">
                  <tr>
                    <th className="py-2 px-3 font-medium w-10">#</th>
                    <th className="py-2 px-3 font-medium">
                      {isZh ? '域名 / 通配符 (SAN)' : 'Domain / Wildcard (SAN)'}
                    </th>
                    <th className="py-2 px-3 font-medium w-20 text-center">
                      {isZh ? '类型' : 'Type'}
                    </th>
                    <th className="py-2 px-3 font-medium w-12 text-right">
                      {isZh ? '操作' : ''}
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {formDomainList.map((entry, index) => {
                    const trimmed = entry.trim();
                    const isWildcard = trimmed.startsWith('*.');
                    return (
                      <tr key={index} className="hover:bg-bg-subtle/40">
                        <td className="py-1.5 px-3 text-tertiary">{index + 1}</td>
                        <td className="py-1.5 px-2">
                          <input
                            type="text"
                            value={entry}
                            onChange={(e) => handleUpdateDomainRow(index, e.target.value)}
                            placeholder={index === 0 ? 'example.com' : '*.example.com'}
                            className="w-full bg-card border border-border rounded-sm px-2.5 py-1.5 text-xs font-mono text-primary focus:outline-none focus:border-primary"
                          />
                        </td>
                        <td className="py-1.5 px-3 text-center">
                          {trimmed ? (
                            <Badge variant={isWildcard ? 'warning' : 'default'} size="sm">
                              {isWildcard ? (isZh ? '通配符' : 'Wildcard') : (isZh ? '单域名' : 'Single')}
                            </Badge>
                          ) : (
                            <span className="text-tertiary">—</span>
                          )}
                        </td>
                        <td className="py-1.5 px-3 text-right">
                          <button
                            type="button"
                            onClick={() => handleRemoveDomainRow(index)}
                            disabled={formDomainList.length <= 1}
                            className="p-1 rounded hover:bg-red-500/10 border border-border text-secondary hover:text-red-500 transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent disabled:hover:text-secondary"
                            title={t('common.delete')}
                            aria-label={t('common.delete')}
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>

              <button
                type="button"
                onClick={handleAddDomainRow}
                className="w-full flex items-center justify-center gap-1.5 py-2 text-xs font-medium text-secondary hover:text-primary hover:bg-bg-subtle border-t border-border transition-colors cursor-pointer"
              >
                <Plus className="w-3.5 h-3.5" />
                {isZh ? '添加域名 (SAN)' : 'Add domain (SAN)'}
              </button>
            </div>

            <p className="text-[11px] text-tertiary">{t('certs.form_domains_helper')}</p>
          </div>

          {/* Rich Encryption Algorithms Selection */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-secondary block">
              {isZh ? '密钥加密算法体系' : 'Key Encryption Algorithm'}
            </label>
            <select
              value={formKeyType}
              onChange={(e) => setFormKeyType(e.target.value)}
              className="w-full bg-card border border-border rounded-sm px-3 py-2 text-xs font-mono text-primary focus:outline-none focus:border-primary"
            >
              {ENCRYPTION_ALGORITHMS.map((algo) => (
                <option key={algo.id} value={algo.id}>
                  {algo.name} — {algo.desc}
                </option>
              ))}
            </select>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setIsIssueModalOpen(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={issuing}
              icon={<ShieldCheck className="w-4 h-4" />}
            >
              {isZh ? '立即自动签发 SSL 证书' : 'Issue Certificate'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* 4. View Certificate PEM Modal */}
      {selectedCertForView && (
        <Modal
          isOpen={true}
          onClose={() => setSelectedCertForView(null)}
          title={`${isZh ? '证书与私钥' : 'Certificate & Key'} — ${selectedCertForView.name}`}
          maxWidth="4xl"
        >
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 max-h-[70vh] overflow-y-auto">
            <div className="min-w-0">
              <label className="text-xs font-semibold text-primary block mb-1">
                {isZh ? '证书完整链 / 公钥 PEM (.crt)' : 'Certificate / Public Key PEM (.crt)'}
              </label>
              <CodeBox code={selectedCertForView.cert_pem} />
            </div>

            <div className="min-w-0">
              <label className="text-xs font-semibold text-primary block mb-1">
                {isZh ? '私钥 PEM (.key)' : 'Private Key PEM (.key)'}
              </label>
              <CodeBox code={selectedCertForView.key_pem} />
            </div>
          </div>
        </Modal>
      )}

      {/* 6. 签发失败原因。CA 返回的错误往往包含定位问题的关键细节
             （CAA 阻断、速率限制、DNS 校验超时等），完整展示而不截断。 */}
      {selectedCertForError && (() => {
        const diag = classifyIssuanceError(selectedCertForError.last_error, isZh);
        const raw = selectedCertForError.last_error || '';
        const copyRaw = () => {
          navigator.clipboard?.writeText(raw);
          setCopiedError(true);
          window.setTimeout(() => setCopiedError(false), 1500);
        };
        return (
          <Modal
            isOpen={true}
            onClose={() => setSelectedCertForError(null)}
            title={t('certs.error_modal_title')}
            maxWidth="lg"
          >
            <div className="space-y-4">
              {/* 原因摘要 + 处理建议：把 CA 的长错误提炼成一句话结论与下一步动作 */}
              <div className="flex items-start gap-3 p-4 rounded-md bg-red-500/5 border border-red-500/20">
                <div className="mt-0.5 flex-shrink-0 w-8 h-8 rounded-full bg-red-500/10 flex items-center justify-center">
                  <XCircle className="w-4 h-4 text-red-500" />
                </div>
                <div className="min-w-0 space-y-1">
                  <div className="text-sm font-semibold text-primary">{diag.title}</div>
                  <div className="text-xs text-secondary leading-relaxed">{diag.advice}</div>
                </div>
              </div>

              {/* 证书结构化信息 */}
              <div className="rounded-md border border-border divide-y divide-border text-xs font-mono">
                <div className="flex items-start gap-3 px-3 py-2">
                  <span className="w-16 flex-shrink-0 text-tertiary">{isZh ? '域名' : 'Domains'}</span>
                  <span className="text-primary break-all">{selectedCertForError.domains}</span>
                </div>
                <div className="flex items-start gap-3 px-3 py-2">
                  <span className="w-16 flex-shrink-0 text-tertiary">{isZh ? '颁发机构' : 'CA'}</span>
                  <span className="text-primary">{selectedCertForError.issuer || '—'}</span>
                </div>
                <div className="flex items-start gap-3 px-3 py-2">
                  <span className="w-16 flex-shrink-0 text-tertiary">{isZh ? '算法' : 'Key'}</span>
                  <span className="text-primary">{selectedCertForError.key_type}</span>
                </div>
              </div>

              {/* 原始错误详情：默认折叠，可一键复制，避免一进来就铺满长文本 */}
              <div className="rounded-md border border-border overflow-hidden">
                <div className="flex items-center justify-between px-3 py-2 bg-bg-subtle">
                  <button
                    type="button"
                    onClick={() => setShowRawError((v) => !v)}
                    className="flex items-center gap-1.5 text-xs font-medium text-secondary hover:text-primary transition-colors cursor-pointer"
                  >
                    <Terminal className="w-3.5 h-3.5" />
                    {isZh ? '技术详情（原始错误）' : 'Technical details (raw error)'}
                    <ChevronDown className={`w-4 h-4 transition-transform ${showRawError ? 'rotate-180' : ''}`} />
                  </button>
                  <button
                    type="button"
                    onClick={copyRaw}
                    className="flex items-center gap-1 text-[11px] text-tertiary hover:text-primary transition-colors cursor-pointer"
                    title={isZh ? '复制错误信息' : 'Copy error'}
                  >
                    {copiedError ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
                    {copiedError ? (isZh ? '已复制' : 'Copied') : (isZh ? '复制' : 'Copy')}
                  </button>
                </div>
                {showRawError && (
                  <div className="p-3 border-t border-border bg-bg text-[11px] text-red-500 font-mono whitespace-pre-wrap break-all max-h-[40vh] overflow-y-auto">
                    {raw || (isZh ? '无更多信息' : 'No additional details')}
                  </div>
                )}
              </div>

              {/* 操作：可直接重试签发（对失败证书会重新走完整签发流程） */}
              <div className="flex justify-end items-center gap-2 pt-3 border-t border-border">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setSelectedCertForError(null)}
                >
                  {t('common.cancel')}
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  icon={<RotateCw className="w-3.5 h-3.5" />}
                  onClick={() => {
                    const uuid = selectedCertForError.uuid;
                    setSelectedCertForError(null);
                    handleRenew(uuid);
                  }}
                >
                  {isZh ? '重试签发' : 'Retry issuance'}
                </Button>
              </div>
            </div>
          </Modal>
        );
      })()}
    </div>
  );
};
