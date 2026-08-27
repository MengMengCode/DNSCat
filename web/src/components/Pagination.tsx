import React, { useEffect, useMemo, useState } from 'react';
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react';
import { useI18n } from '../i18n/I18nContext';

// usePagination 对「已全量加载到前端」的数组做客户端切片分页。
// 列表 API 目前返回全量数据，分页主要解决一次渲染大量表格行带来的性能与可读性问题。
// 当数据经筛选 / 搜索后长度变化、当前页越界时，会自动把页码收敛到有效范围，避免停在空白页。
export function usePagination<T>(items: T[], initialPageSize = 10) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(initialPageSize);

  const total = items.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  // safePage 保证即便 page 状态尚未同步（如刚筛选完数据变少），本次渲染也用有效页码切片。
  const safePage = Math.min(Math.max(1, page), totalPages);

  useEffect(() => {
    if (page !== safePage) {
      setPage(safePage);
    }
  }, [page, safePage]);

  const start = (safePage - 1) * pageSize;
  const pageItems = useMemo(
    () => items.slice(start, start + pageSize),
    [items, start, pageSize]
  );

  const from = total === 0 ? 0 : start + 1;
  const to = Math.min(start + pageSize, total);

  // 改变每页条数后回到第一页，避免停留在不存在的高页码。
  const changePageSize = (n: number) => {
    setPageSize(n);
    setPage(1);
  };

  return {
    page: safePage,
    setPage,
    pageSize,
    setPageSize: changePageSize,
    total,
    totalPages,
    pageItems,
    from,
    to,
  };
}

// buildPageWindow 生成带省略号的页码序列，例如 [1, 'ellipsis', 4, 5, 6, 'ellipsis', 20]，
// 始终包含首尾页与当前页相邻的页码，避免页数过多时页码条无限变宽。
function buildPageWindow(page: number, totalPages: number): (number | 'ellipsis')[] {
  const pages = new Set<number>();
  pages.add(1);
  pages.add(totalPages);
  for (let p = page - 1; p <= page + 1; p++) {
    if (p >= 1 && p <= totalPages) {
      pages.add(p);
    }
  }

  const sorted = Array.from(pages).sort((a, b) => a - b);
  const out: (number | 'ellipsis')[] = [];
  let prev = 0;
  for (const p of sorted) {
    if (prev && p - prev > 1) {
      out.push('ellipsis');
    }
    out.push(p);
    prev = p;
  }
  return out;
}

interface PaginationProps {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
  from: number;
  to: number;
  onPageChange: (p: number) => void;
  onPageSizeChange: (n: number) => void;
  pageSizeOptions?: number[];
  className?: string;
}

// Pagination 是与 usePagination 配套的展示组件：左侧显示计数，右侧为每页条数选择与页码导航。
// total 为 0 时不渲染，交由表格自身的空状态行提示。
export const Pagination: React.FC<PaginationProps> = ({
  page,
  pageSize,
  total,
  totalPages,
  from,
  to,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions = [10, 20, 50, 100],
  className = '',
}) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';

  if (total === 0) {
    return null;
  }

  const windowPages = buildPageWindow(page, totalPages);
  const atStart = page <= 1;
  const atEnd = page >= totalPages;

  const navBtn =
    'inline-flex items-center justify-center h-8 min-w-[2rem] px-2 rounded-sm border border-border text-secondary hover:text-primary hover:bg-bg-subtle disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer';

  return (
    <div
      className={`flex flex-col sm:flex-row items-center justify-between gap-3 px-4 py-3 border-t border-border text-xs font-mono ${className}`}
    >
      <div className="text-tertiary order-2 sm:order-1">
        {isZh
          ? `共 ${total.toLocaleString()} 条 · 第 ${from.toLocaleString()}-${to.toLocaleString()} 条`
          : `${from.toLocaleString()}-${to.toLocaleString()} of ${total.toLocaleString()}`}
      </div>

      <div className="flex items-center gap-3 order-1 sm:order-2">
        <div className="flex items-center gap-1.5 text-tertiary">
          <span>{isZh ? '每页' : 'Rows'}</span>
          <select
            value={pageSize}
            onChange={(e) => onPageSizeChange(Number(e.target.value))}
            className="h-8 bg-card text-primary rounded-sm border border-border px-2 focus:outline-none focus:border-primary cursor-pointer"
            aria-label={isZh ? '每页条数' : 'Rows per page'}
          >
            {pageSizeOptions.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </div>

        <div className="flex items-center gap-1">
          <button
            type="button"
            className={navBtn}
            disabled={atStart}
            onClick={() => onPageChange(1)}
            title={isZh ? '首页' : 'First page'}
            aria-label={isZh ? '首页' : 'First page'}
          >
            <ChevronsLeft className="w-3.5 h-3.5" />
          </button>
          <button
            type="button"
            className={navBtn}
            disabled={atStart}
            onClick={() => onPageChange(page - 1)}
            title={isZh ? '上一页' : 'Previous page'}
            aria-label={isZh ? '上一页' : 'Previous page'}
          >
            <ChevronLeft className="w-3.5 h-3.5" />
          </button>

          {windowPages.map((p, idx) =>
            p === 'ellipsis' ? (
              <span key={`ellipsis-${idx}`} className="px-1 text-tertiary select-none">
                …
              </span>
            ) : (
              <button
                key={p}
                type="button"
                onClick={() => onPageChange(p)}
                aria-current={p === page ? 'page' : undefined}
                className={
                  p === page
                    ? 'inline-flex items-center justify-center h-8 min-w-[2rem] px-2 rounded-sm border border-primary bg-primary text-bg font-semibold cursor-pointer'
                    : navBtn
                }
              >
                {p}
              </button>
            )
          )}

          <button
            type="button"
            className={navBtn}
            disabled={atEnd}
            onClick={() => onPageChange(page + 1)}
            title={isZh ? '下一页' : 'Next page'}
            aria-label={isZh ? '下一页' : 'Next page'}
          >
            <ChevronRight className="w-3.5 h-3.5" />
          </button>
          <button
            type="button"
            className={navBtn}
            disabled={atEnd}
            onClick={() => onPageChange(totalPages)}
            title={isZh ? '末页' : 'Last page'}
            aria-label={isZh ? '末页' : 'Last page'}
          >
            <ChevronsRight className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
};
