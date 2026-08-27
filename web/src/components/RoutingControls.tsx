import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, Check, X, Search } from 'lucide-react';
import { useI18n } from '../i18n/I18nContext';
import { RoutingLine } from '../types';
import { localizeDualName } from '../lib/format';
import { REGION_OPTIONS, getRegionOption } from './FlagRegionSelect';

// 大洲维度：与后端 geo 大洲码保持一致 (AS/EU/NA/SA/AF/OC/AN)。
// 必须覆盖内置线路用到的全部七个大洲码，否则列表里会退化成显示原始码（如 "AN"）。
export interface ContinentOption {
  code: string;
  nameZh: string;
  nameEn: string;
  /** 大洲图标：用 Unicode 地球 emoji 的三个朝向区分东/西/欧非半球，南极洲用冰块。 */
  flag: string;
}

export const CONTINENT_OPTIONS: ContinentOption[] = [
  { code: 'AS', nameZh: '亚洲', nameEn: 'Asia', flag: '🌏' },
  { code: 'EU', nameZh: '欧洲', nameEn: 'Europe', flag: '🌍' },
  { code: 'NA', nameZh: '北美洲', nameEn: 'North America', flag: '🌎' },
  { code: 'SA', nameZh: '南美洲', nameEn: 'South America', flag: '🌎' },
  { code: 'AF', nameZh: '非洲', nameEn: 'Africa', flag: '🌍' },
  { code: 'OC', nameZh: '大洋洲', nameEn: 'Oceania', flag: '🌏' },
  { code: 'AN', nameZh: '南极洲', nameEn: 'Antarctica', flag: '🧊' },
];

/** 按大洲码取本地化名称；未知码回退为原始码，便于暴露后端新增维度。 */
export const getContinentLabel = (code: string, isZh: boolean): string => {
  const opt = CONTINENT_OPTIONS.find((o) => o.code === code.toUpperCase());
  if (!opt) return code.toUpperCase();
  return isZh ? opt.nameZh : opt.nameEn;
};

/** 按大洲码取 emoji 图标；未知码回退为通用地球，避免渲染出空白。 */
export const getContinentFlag = (code: string): string =>
  CONTINENT_OPTIONS.find((o) => o.code === code.toUpperCase())?.flag ?? '🌐';

const COUNTRY_OPTIONS = REGION_OPTIONS.filter((opt) => opt.category === 'country');

const splitCsv = (csv: string): string[] =>
  (csv || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

// 大洲多选：七大洲的复选按钮组。
export const ContinentMultiSelect: React.FC<{
  value: string;
  onChange: (csv: string) => void;
}> = ({ value, onChange }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const selected = splitCsv(value).map((c) => c.toUpperCase());

  const toggle = (code: string) => {
    const next = selected.includes(code)
      ? selected.filter((c) => c !== code)
      : [...selected, code];
    onChange(next.join(','));
  };

  return (
    <div className="flex flex-wrap gap-1.5">
      {CONTINENT_OPTIONS.map((opt) => {
        const active = selected.includes(opt.code);
        return (
          <button
            key={opt.code}
            type="button"
            onClick={() => toggle(opt.code)}
            className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-sm border text-xs font-mono transition-colors cursor-pointer ${
              active
                ? 'bg-primary text-bg border-primary font-semibold'
                : 'bg-card text-secondary border-border hover:border-border-hover hover:text-primary'
            }`}
          >
            <span>{opt.flag}</span>
            <span>{isZh ? opt.nameZh : opt.nameEn}</span>
            {active && <Check className="w-3 h-3" />}
          </button>
        );
      })}
    </div>
  );
};

// 国家多选：可搜索的国家复选下拉，输出逗号分隔的小写国家码，允许为空。
export const CountryMultiSelect: React.FC<{
  value: string;
  onChange: (csv: string) => void;
}> = ({ value, onChange }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const dropdownRef = useRef<HTMLDivElement>(null);

  const selected = splitCsv(value).map((c) => c.toLowerCase());

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const toggle = (code: string) => {
    const next = selected.includes(code)
      ? selected.filter((c) => c !== code)
      : [...selected, code];
    onChange(next.join(','));
  };

  const remove = (code: string, e: React.MouseEvent) => {
    e.stopPropagation();
    onChange(selected.filter((c) => c !== code).join(','));
  };

  const filtered = COUNTRY_OPTIONS.filter((opt) => {
    const text = (opt.nameZh + ' ' + opt.nameEn + ' ' + opt.code).toLowerCase();
    return text.includes(search.toLowerCase().trim());
  });

  return (
    <div className="w-full space-y-1.5 relative" ref={dropdownRef}>
      <div
        onClick={() => setIsOpen(!isOpen)}
        className="min-h-10 bg-card border border-border hover:border-border-hover rounded-sm p-1.5 flex items-center justify-between gap-2 cursor-pointer transition-colors"
      >
        <div className="flex flex-wrap items-center gap-1.5 max-h-24 overflow-y-auto">
          {selected.length === 0 && (
            <span className="text-xs text-tertiary font-mono px-1">
              {isZh ? '未选择国家 / 地区（可留空）' : 'No countries selected (optional)'}
            </span>
          )}
          {selected.map((code) => {
            const opt = getRegionOption(code);
            return (
              <span
                key={code}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-bg-subtle border border-border text-xs font-mono text-primary"
              >
                <span>{opt.flag}</span>
                <span>{isZh ? opt.nameZh.split(' ')[0] : opt.nameEn}</span>
                <button
                  type="button"
                  onClick={(e) => remove(code, e)}
                  className="hover:text-red-500 transition-colors ml-0.5"
                >
                  <X className="w-3 h-3" />
                </button>
              </span>
            );
          })}
        </div>
        <ChevronDown className={`w-3.5 h-3.5 text-secondary flex-shrink-0 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </div>

      {isOpen && (
        <div className="absolute left-0 right-0 top-full mt-1 bg-card border border-border rounded-sm shadow-2xl z-50 animate-in fade-in zoom-in-95 duration-100 p-2 space-y-2">
          <div className="flex items-center gap-1.5 px-2 py-1.5 bg-bg-subtle border border-border rounded">
            <Search className="w-3.5 h-3.5 text-tertiary" />
            <input
              type="text"
              placeholder={isZh ? '搜索国家代码或名称 (如 cn, 日本)...' : 'Search country code or name...'}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-transparent text-xs text-primary focus:outline-none font-mono"
              autoFocus
            />
            {search && (
              <button type="button" onClick={() => setSearch('')} className="text-secondary hover:text-primary">
                <X className="w-3 h-3" />
              </button>
            )}
          </div>

          <div className="max-h-56 overflow-y-auto space-y-0.5 py-1">
            {filtered.map((opt) => {
              const isSelected = selected.includes(opt.code);
              return (
                <div
                  key={opt.code}
                  onClick={() => toggle(opt.code)}
                  className={`flex items-center justify-between px-2.5 py-1.5 rounded text-xs font-mono cursor-pointer transition-colors ${
                    isSelected ? 'bg-primary text-bg font-semibold' : 'hover:bg-bg-subtle text-primary'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-sm">{opt.flag}</span>
                    <span>{isZh ? opt.nameZh : opt.nameEn}</span>
                    <span className={`text-[10px] px-1 rounded border ${isSelected ? 'border-bg text-bg/80' : 'border-border text-tertiary'}`}>
                      {opt.code.toUpperCase()}
                    </span>
                  </div>
                  {isSelected && <Check className="w-3.5 h-3.5 flex-shrink-0" />}
                </div>
              );
            })}
            {filtered.length === 0 && (
              <div className="text-center text-secondary py-4 text-xs font-mono">
                {isZh ? '未找到匹配的国家 / 地区' : 'No matching regions found'}
              </div>
            )}
          </div>

          {selected.length > 0 && (
            <div className="pt-2 border-t border-border flex items-center justify-between text-[11px] font-mono text-tertiary">
              <span>{isZh ? `已选择 ${selected.length} 个国家` : `${selected.length} countries selected`}</span>
              <button type="button" onClick={() => onChange('')} className="text-blue-500 hover:underline">
                {isZh ? '清空' : 'Clear'}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

// 线路多选：供解析记录表单选择用户已定义的智能分线线路，输出逗号分隔的线路 key。
// 选中 default（或不选）表示全球默认线路。
export const RoutingLineSelect: React.FC<{
  lines: RoutingLine[];
  value: string;
  onChange: (csv: string) => void;
  label?: string;
}> = ({ lines, value, onChange, label }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const dropdownRef = useRef<HTMLDivElement>(null);

  const selected = splitCsv(value);
  const isDefault = selected.length === 0 || selected.includes('default');

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const enabledLines = lines.filter((l) => l.enabled && l.key !== 'default');

  const lineNameByKey = (key: string): string => {
    const line = lines.find((l) => l.key.toLowerCase() === key.toLowerCase());
    // Name 可能是「中文 (English)」的双语值，按界面语言只取一侧。
    return line ? localizeDualName(line.name, isZh) : key;
  };

  const selectDefault = () => {
    onChange('default');
    setIsOpen(false);
  };

  const toggle = (key: string) => {
    let next = selected.filter((k) => k !== 'default');
    if (next.includes(key)) {
      next = next.filter((k) => k !== key);
    } else {
      next.push(key);
    }
    onChange(next.length === 0 ? 'default' : next.join(','));
  };

  const remove = (key: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const next = selected.filter((k) => k !== key);
    onChange(next.length === 0 ? 'default' : next.join(','));
  };

  const filtered = enabledLines.filter((l) => {
    const text = (l.name + ' ' + l.key + ' ' + l.description).toLowerCase();
    return text.includes(search.toLowerCase().trim());
  });

  return (
    <div className="w-full space-y-1.5 relative" ref={dropdownRef}>
      {label && <label className="text-xs font-medium text-secondary block">{label}</label>}

      <div
        onClick={() => setIsOpen(!isOpen)}
        className="min-h-10 bg-card border border-border hover:border-border-hover rounded-sm p-1.5 flex items-center justify-between gap-2 cursor-pointer transition-colors"
      >
        <div className="flex flex-wrap items-center gap-1.5 max-h-24 overflow-y-auto">
          {isDefault ? (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-bg-subtle border border-border text-xs font-mono text-primary">
              <span>🌐</span>
              <span>{isZh ? '全球默认线路' : 'Global Default'}</span>
            </span>
          ) : (
            selected.map((key) => (
              <span
                key={key}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-bg-subtle border border-border text-xs font-mono text-primary"
              >
                <span>{lineNameByKey(key)}</span>
                <button
                  type="button"
                  onClick={(e) => remove(key, e)}
                  className="hover:text-red-500 transition-colors ml-0.5"
                >
                  <X className="w-3 h-3" />
                </button>
              </span>
            ))
          )}
        </div>
        <ChevronDown className={`w-3.5 h-3.5 text-secondary flex-shrink-0 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </div>

      {isOpen && (
        <div className="absolute left-0 right-0 top-full mt-1 bg-card border border-border rounded-sm shadow-2xl z-50 animate-in fade-in zoom-in-95 duration-100 p-2 space-y-2">
          <div className="flex items-center gap-1.5 px-2 py-1.5 bg-bg-subtle border border-border rounded">
            <Search className="w-3.5 h-3.5 text-tertiary" />
            <input
              type="text"
              placeholder={isZh ? '搜索线路名称或标识...' : 'Search routing line...'}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-transparent text-xs text-primary focus:outline-none font-mono"
              autoFocus
            />
          </div>

          <div
            onClick={selectDefault}
            className={`flex items-center justify-between px-2.5 py-1.5 rounded text-xs font-mono cursor-pointer transition-colors ${
              isDefault ? 'bg-primary text-bg font-semibold' : 'hover:bg-bg-subtle text-primary'
            }`}
          >
            <div className="flex items-center gap-2">
              <span className="text-sm">🌐</span>
              <span>{isZh ? '全球默认线路 (Global Default)' : 'Global Default'}</span>
            </div>
            {isDefault && <Check className="w-3.5 h-3.5 flex-shrink-0" />}
          </div>

          <div className="max-h-56 overflow-y-auto space-y-0.5 py-1 border-t border-border">
            {filtered.map((line) => {
              const isSelected = !isDefault && selected.includes(line.key);
              return (
                <div
                  key={line.id}
                  onClick={() => toggle(line.key)}
                  className={`flex items-center justify-between px-2.5 py-1.5 rounded text-xs font-mono cursor-pointer transition-colors ${
                    isSelected ? 'bg-primary text-bg font-semibold' : 'hover:bg-bg-subtle text-primary'
                  }`}
                >
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="truncate">{localizeDualName(line.name, isZh) || line.key}</span>
                    <span className={`text-[10px] truncate ${isSelected ? 'text-bg/70' : 'text-tertiary'}`}>
                      {line.key}
                      {line.description ? ` · ${line.description}` : ''}
                    </span>
                  </div>
                  {isSelected && <Check className="w-3.5 h-3.5 flex-shrink-0" />}
                </div>
              );
            })}

            {filtered.length === 0 && (
              <div className="text-center text-secondary py-4 text-xs font-mono">
                {isZh ? '暂无可用线路，请先在「智能路由规则」中创建' : 'No routing lines yet. Create one in Smart Routing Rules.'}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

// lineBadgeEmoji 为线路徽章挑选一个地球 / 大洲 emoji：
// 单一大洲的线路用该大洲对应的地球朝向（🌏 / 🌍 / 🌎 / 🧊），单一国家的线路用其国旗，
// 跨多维度或按 ASN 分流的线路用通用地球 🌐；找不到线路时按国家码回退。
const lineBadgeEmoji = (code: string, line?: RoutingLine): string => {
  if (line) {
    const continents = splitCsv(line.continents);
    if (continents.length === 1) return getContinentFlag(continents[0]);
    const countries = splitCsv(line.countries);
    if (countries.length === 1) return getRegionOption(countries[0]).flag || '🌐';
    return '🌐';
  }
  return getRegionOption(code).flag || '🌐';
};

// 线路标签：在记录列表中把 geo_line（线路 key CSV）渲染为线路名称徽章。
export const RoutingLineBadges: React.FC<{ codeString: string; lines: RoutingLine[] }> = ({
  codeString,
  lines,
}) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const codes = splitCsv(codeString);

  if (codes.length === 0 || (codes.length === 1 && codes[0].toLowerCase() === 'default')) {
    return (
      <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-bg-subtle border border-border text-[11px] font-mono text-secondary">
        <span>🌐</span>
        <span>{isZh ? '默认' : 'Default'}</span>
      </span>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {codes.map((code) => {
        const line = lines.find((l) => l.key.toLowerCase() === code.toLowerCase());
        const label = line
          ? localizeDualName(line.name, isZh)
          : (isZh ? getRegionOption(code).nameZh : getRegionOption(code).nameEn);
        const emoji = lineBadgeEmoji(code, line);
        return (
          <span
            key={code}
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-bg-subtle border border-border text-[11px] font-mono text-secondary"
            title={code}
          >
            <span>{emoji}</span>
            <span className="truncate max-w-[140px]">{label}</span>
          </span>
        );
      })}
    </div>
  );
};
