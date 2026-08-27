import React from 'react';
import { clsx } from 'clsx';

interface PageHeaderProps {
  icon?: React.ReactNode;
  title: string;
  subtitle?: string;
  children?: React.ReactNode;
  variant?: 'page' | 'section';
  className?: string;
}

export const PageHeader: React.FC<PageHeaderProps> = ({
  icon,
  title,
  subtitle,
  children,
  variant = 'page',
  className,
}) => {
  const isPage = variant === 'page';

  return (
    <div
      className={clsx(
        isPage && 'geist-card p-6',
        isPage && 'flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4',
        !isPage && 'flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3',
        className
      )}
    >
      <div className="flex items-center gap-3 min-w-0">
        {icon && <span className="flex-shrink-0">{icon}</span>}
        <div className="min-w-0">
          <h2
            className={clsx(
              'font-semibold text-primary truncate',
              isPage ? 'text-base' : 'text-sm'
            )}
          >
            {title}
          </h2>
          {subtitle && (
            <p className="text-xs text-secondary mt-0.5 truncate">
              {subtitle}
            </p>
          )}
        </div>
      </div>

      {children && <div className="flex items-center gap-2 flex-shrink-0">{children}</div>}
    </div>
  );
};
