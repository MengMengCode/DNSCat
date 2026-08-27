import React, { useState } from 'react';
import { clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { Check, Copy, X } from 'lucide-react';
import * as Dialog from '@radix-ui/react-dialog';
import * as SwitchPrimitive from '@radix-ui/react-switch';
import * as ProgressPrimitive from '@radix-ui/react-progress';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import { useI18n } from '../i18n/I18nContext';

// 1. Radix UI Powered Modal / Dialog
interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: React.ReactNode;
  maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl' | '4xl';
}

export const Modal: React.FC<ModalProps> = ({
  isOpen,
  onClose,
  title,
  description,
  children,
  maxWidth = 'md',
}) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const widthClasses = {
    sm: 'max-w-sm',
    md: 'max-w-md',
    lg: 'max-w-lg',
    xl: 'max-w-xl',
    '2xl': 'max-w-2xl',
    '3xl': 'max-w-3xl',
    '4xl': 'max-w-4xl',
  };

  return (
    <Dialog.Root open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs animate-in fade-in duration-200" />
        <Dialog.Content
          className={clsx(
            'fixed left-[50%] top-[50%] z-50 translate-x-[-50%] translate-y-[-50%]',
            'w-full bg-card border border-border rounded-md shadow-2xl p-6',
            'animate-in fade-in zoom-in-95 duration-200 focus:outline-none',
            widthClasses[maxWidth]
          )}
        >
          <div className="flex items-start justify-between gap-4 mb-4">
            <div>
              <Dialog.Title className="text-base font-bold text-primary font-mono tracking-tight">
                {title}
              </Dialog.Title>
              {description && (
                <Dialog.Description className="text-xs text-secondary mt-1 font-mono">
                  {description}
                </Dialog.Description>
              )}
            </div>
            <Dialog.Close asChild>
              <button
                onClick={onClose}
                className="text-secondary hover:text-primary p-1 rounded-sm hover:bg-bg-subtle transition-colors cursor-pointer"
                aria-label={isZh ? '关闭' : 'Close'}
              >
                <X className="w-4 h-4" />
              </button>
            </Dialog.Close>
          </div>

          <div>{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
};

// 2. Radix UI Powered Switch
interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: string;
  disabled?: boolean;
  size?: 'sm' | 'md';
}

export const Switch: React.FC<SwitchProps> = ({ checked, onChange, label, disabled, size = 'md' }) => {
  const isSmall = size === 'sm';

  return (
    <div className="inline-flex items-center gap-2.5 select-none">
      <SwitchPrimitive.Root
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
        className={clsx(
          'bg-border rounded-full relative transition-colors focus:outline-none focus:ring-1 focus:ring-primary cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed',
          isSmall ? 'w-8 h-[18px]' : 'w-10 h-6',
          'data-[state=checked]:bg-primary'
        )}
      >
        <SwitchPrimitive.Thumb
          className={clsx(
            'block bg-white dark:bg-black rounded-full shadow-md transition-transform will-change-transform',
            isSmall
              ? 'w-3 h-3 translate-x-[3px] data-[state=checked]:translate-x-[17px]'
              : 'w-4 h-4 translate-x-1 data-[state=checked]:translate-x-5'
          )}
        />
      </SwitchPrimitive.Root>
      {label && <span className="text-xs font-mono text-primary font-medium">{label}</span>}
    </div>
  );
};

// 3. Radix UI Powered Progress Bar
interface ProgressProps {
  value: number;
  max?: number;
  className?: string;
  indicatorClassName?: string;
}

export const Progress: React.FC<ProgressProps> = ({
  value,
  max = 100,
  className,
  indicatorClassName,
}) => {
  const percent = Math.min(100, Math.max(0, (value / max) * 100));

  return (
    <ProgressPrimitive.Root
      value={percent}
      className={clsx('relative h-1.5 w-full overflow-hidden rounded-full bg-bg-subtle', className)}
    >
      <ProgressPrimitive.Indicator
        style={{ transform: `translateX(-${100 - percent}%)` }}
        className={clsx('h-full w-full bg-primary transition-all duration-300', indicatorClassName)}
      />
    </ProgressPrimitive.Root>
  );
};

// 4. Radix UI Powered Tooltip
interface TooltipProps {
  content: React.ReactNode;
  children: React.ReactNode;
  side?: 'top' | 'right' | 'bottom' | 'left';
}

export const Tooltip: React.FC<TooltipProps> = ({ content, children, side = 'top' }) => {
  return (
    <TooltipPrimitive.Provider delayDuration={150}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            side={side}
            className="z-50 overflow-hidden rounded bg-card px-2.5 py-1.5 text-[11px] font-mono text-primary shadow-popover border border-border-muted animate-in fade-in zoom-in-95 duration-150"
          >
            {content}
            <TooltipPrimitive.Arrow className="fill-border" />
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
};

// 5. Button
interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'tertiary' | 'error' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  icon?: React.ReactNode;
}

export const Button: React.FC<ButtonProps> = ({
  children,
  variant = 'secondary',
  size = 'md',
  loading = false,
  icon,
  className,
  disabled,
  ...props
}) => {
  const sizeClasses = {
    sm: 'h-8 px-3 text-xs rounded-sm gap-1.5',
    md: 'h-10 px-4 text-sm rounded-sm gap-2',
    lg: 'h-12 px-6 text-base rounded-md gap-2.5',
  };

  const variantClasses = {
    primary:
      'bg-primary text-bg font-medium hover:opacity-90 active:scale-[0.98] border border-transparent shadow-sm',
    secondary:
      'bg-card text-primary border border-border hover:border-border-hover hover:bg-bg-subtle active:scale-[0.98]',
    tertiary:
      'bg-transparent text-secondary hover:text-primary hover:bg-gray-100 dark:hover:bg-gray-200 border-none',
    error:
      'bg-red-500 text-white font-medium hover:bg-red-600 active:scale-[0.98] border border-transparent shadow-sm',
    outline:
      'bg-transparent text-primary border border-border hover:border-border-hover hover:bg-bg-subtle',
  };

  return (
    <button
      className={clsx(
        'inline-flex items-center justify-center font-sans transition-all duration-150 select-none disabled:opacity-50 disabled:pointer-events-none cursor-pointer',
        sizeClasses[size],
        variantClasses[variant],
        className
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <svg
          className="animate-spin -ml-1 mr-2 h-4 w-4 text-current"
          xmlns="http://www.w3.org/2000/svg"
          fill="none"
          viewBox="0 0 24 24"
        >
          <circle
            className="opacity-25"
            cx="12"
            cy="12"
            r="10"
            stroke="currentColor"
            strokeWidth="4"
          ></circle>
          <path
            className="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
          ></path>
        </svg>
      ) : (
        icon && <span className="flex-shrink-0">{icon}</span>
      )}
      {children}
    </button>
  );
};

// 6. Input
interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  helper?: string;
  prefixIcon?: React.ReactNode;
  suffixIcon?: React.ReactNode;
}

export const Input: React.FC<InputProps> = ({
  label,
  error,
  helper,
  prefixIcon,
  suffixIcon,
  className,
  ...props
}) => {
  return (
    <div className="w-full flex flex-col gap-1.5">
      {label && (
        <label className="text-xs font-medium text-secondary flex items-center justify-between">
          <span>{label}</span>
        </label>
      )}
      <div className="relative flex items-center">
        {prefixIcon && (
          <div className="absolute left-3 text-secondary pointer-events-none flex items-center">
            {prefixIcon}
          </div>
        )}
        {/* 用 twMerge 而非 clsx 合并：同类工具类冲突时以调用方传入的为准。
            clsx 只是拼接字符串，h-10 与外部传入的 h-8 同时存在时，
            实际生效的取决于 Tailwind 的生成顺序而非书写顺序，导致尺寸无法被覆盖。 */}
        <input
          className={twMerge(
            'w-full h-10 bg-card text-primary text-sm rounded-sm border border-border px-3 font-sans transition-colors placeholder:text-tertiary focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary',
            prefixIcon && 'pl-9',
            suffixIcon && 'pr-9',
            error && 'border-red-500 focus:border-red-500 focus:ring-red-500',
            className
          )}
          {...props}
        />
        {suffixIcon && (
          <div className="absolute right-3 text-secondary flex items-center">{suffixIcon}</div>
        )}
      </div>
      {error ? (
        <span className="text-xs text-red-500">{error}</span>
      ) : helper ? (
        <span className="text-xs text-tertiary">{helper}</span>
      ) : null}
    </div>
  );
};

// 7. Select
interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  options: { label: string; value: string | number }[];
}

export const Select: React.FC<SelectProps> = ({ label, error, options, className, ...props }) => {
  return (
    <div className="w-full flex flex-col gap-1.5">
      {label && <label className="text-xs font-medium text-secondary">{label}</label>}
      <select
        className={clsx(
          'w-full h-10 bg-card text-primary text-sm rounded-sm border border-border px-3 font-sans transition-colors focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary cursor-pointer',
          error && 'border-red-500',
          className
        )}
        {...props}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      {error && <span className="text-xs text-red-500">{error}</span>}
    </div>
  );
};

// 8. Badge
interface BadgeProps {
  children: React.ReactNode;
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info' | 'purple';
  size?: 'sm' | 'md';
}

export const Badge: React.FC<BadgeProps> = ({ children, variant = 'default', size = 'md' }) => {
  const variantStyles = {
    default: 'bg-bg-subtle text-primary border-border',
    purple: 'bg-bg-subtle text-primary border-border',
    info: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
    success: 'bg-green-500/10 text-green-500 border-green-500/20',
    warning: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
    error: 'bg-red-500/10 text-red-500 border-red-500/20',
  };

  const sizeStyles = {
    sm: 'px-2 py-0.5 text-[11px] rounded-full',
    md: 'px-2.5 py-0.5 text-xs rounded-full',
  };

  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1 font-mono font-medium border leading-none whitespace-nowrap',
        variantStyles[variant],
        sizeStyles[size]
      )}
    >
      {children}
    </span>
  );
};

// 9. StatCard
interface StatCardProps {
  title: string;
  value: string | number;
  subValue?: string;
  icon?: React.ReactNode;
  trend?: {
    value: string;
    isUp: boolean;
  };
  onClick?: () => void;
}

export const StatCard: React.FC<StatCardProps> = ({
  title,
  value,
  subValue,
  icon,
  trend,
  onClick,
}) => {
  return (
    <div
      onClick={onClick}
      className={clsx(
        'geist-card p-5 flex flex-col justify-between transition-all duration-150',
        onClick && 'cursor-pointer hover:border-border-hover'
      )}
    >
      <div className="flex items-center justify-between text-secondary mb-2">
        <span className="text-xs font-medium font-sans">{title}</span>
        {icon && <span className="text-secondary">{icon}</span>}
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-2xl font-bold font-sans tracking-tight text-primary">{value}</span>
        {subValue && <span className="text-xs text-tertiary">{subValue}</span>}
        {trend && (
          <div className="flex items-center gap-1 mt-1 text-xs">
            <span className={trend.isUp ? 'text-green-500' : 'text-red-500'}>
              {trend.isUp ? '↑' : '↓'} {trend.value}
            </span>
            <span className="text-tertiary">vs last period</span>
          </div>
        )}
      </div>
    </div>
  );
};

// 10. CodeBox
interface CodeBoxProps {
  code: string;
  language?: string;
}

export const CodeBox: React.FC<CodeBoxProps> = ({ code }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group rounded-md border border-border bg-bg-subtle overflow-hidden">
      <div className="absolute right-3 top-3 z-10 opacity-0 group-hover:opacity-100 transition-opacity">
        <button
          onClick={handleCopy}
          className="p-1.5 rounded bg-card border border-border text-secondary hover:text-primary shadow-xs transition-colors cursor-pointer flex items-center gap-1 text-xs font-mono"
        >
          {copied ? (
            <>
              <Check className="w-3.5 h-3.5 text-green-500" />
              <span>{isZh ? '已复制' : 'Copied'}</span>
            </>
          ) : (
            <>
              <Copy className="w-3.5 h-3.5" />
              <span>{isZh ? '复制' : 'Copy'}</span>
            </>
          )}
        </button>
      </div>
      <pre className="p-4 text-xs font-mono text-primary overflow-x-auto select-all leading-relaxed whitespace-pre-wrap break-all">
        <code>{code}</code>
      </pre>
    </div>
  );
};
