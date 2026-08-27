import React, { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { clsx } from 'clsx';
import { AlertTriangle, CheckCircle2, Info, XCircle } from 'lucide-react';
import { Button, Input } from './GeistUI';
import { useI18n } from '../i18n/I18nContext';

// 对话框语义：决定图标与主按钮的视觉强调，用于替代原生 alert / confirm / prompt。
export type DialogVariant = 'info' | 'success' | 'warning' | 'danger';

export interface ConfirmOptions {
  title?: string;
  message: React.ReactNode;
  confirmText?: string;
  cancelText?: string;
  variant?: DialogVariant;
}

export interface AlertOptions {
  title?: string;
  message: React.ReactNode;
  confirmText?: string;
  variant?: DialogVariant;
}

export interface PromptOptions {
  title?: string;
  message: React.ReactNode;
  placeholder?: string;
  defaultValue?: string;
  confirmText?: string;
  cancelText?: string;
  variant?: DialogVariant;
}

interface DialogContextValue {
  /** 传字符串等价于 { message }。返回用户是否确认。 */
  confirm: (options: string | ConfirmOptions) => Promise<boolean>;
  /** 传字符串等价于 { message }。 */
  alert: (options: string | AlertOptions) => Promise<void>;
  /** 返回输入内容，取消时为 null（与原生 prompt 语义一致）。 */
  prompt: (options: string | PromptOptions) => Promise<string | null>;
}

type DialogMode = 'confirm' | 'alert' | 'prompt';

interface DialogState {
  mode: DialogMode;
  title?: string;
  message: React.ReactNode;
  placeholder?: string;
  confirmText?: string;
  cancelText?: string;
  variant: DialogVariant;
}

type DialogResult = boolean | string | null;

const DialogContext = createContext<DialogContextValue | undefined>(undefined);

export const DialogProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useI18n();
  const [state, setState] = useState<DialogState | null>(null);
  const [inputValue, setInputValue] = useState('');
  // 保存当前等待中的 Promise resolve，关闭对话框时结算，避免调用方永久挂起。
  const resolverRef = useRef<((value: DialogResult) => void) | null>(null);

  const finish = useCallback((value: DialogResult) => {
    const resolve = resolverRef.current;
    resolverRef.current = null;
    setState(null);
    setInputValue('');
    resolve?.(value);
  }, []);

  const open = useCallback((next: DialogState, initialInput: string): Promise<DialogResult> => {
    // 上一个对话框若仍在等待（并发调用），先按取消结算，防止 resolver 泄漏。
    // 各入口的包装函数会把 false 收敛成自身语义（confirm -> false，prompt -> null）。
    const pending = resolverRef.current;
    resolverRef.current = null;
    pending?.(false);

    return new Promise<DialogResult>((resolve) => {
      resolverRef.current = resolve;
      setInputValue(initialInput);
      setState(next);
    });
  }, []);

  const confirm = useCallback(
    async (options: string | ConfirmOptions) => {
      const opts: ConfirmOptions = typeof options === 'string' ? { message: options } : options;
      const result = await open(
        {
          mode: 'confirm',
          title: opts.title,
          message: opts.message,
          confirmText: opts.confirmText,
          cancelText: opts.cancelText,
          variant: opts.variant ?? 'warning',
        },
        ''
      );
      return result === true;
    },
    [open]
  );

  const alert = useCallback(
    async (options: string | AlertOptions) => {
      const opts: AlertOptions = typeof options === 'string' ? { message: options } : options;
      await open(
        {
          mode: 'alert',
          title: opts.title,
          message: opts.message,
          confirmText: opts.confirmText,
          variant: opts.variant ?? 'info',
        },
        ''
      );
    },
    [open]
  );

  const prompt = useCallback(
    async (options: string | PromptOptions) => {
      const opts: PromptOptions = typeof options === 'string' ? { message: options } : options;
      const result = await open(
        {
          mode: 'prompt',
          title: opts.title,
          message: opts.message,
          placeholder: opts.placeholder,
          confirmText: opts.confirmText,
          cancelText: opts.cancelText,
          variant: opts.variant ?? 'warning',
        },
        opts.defaultValue ?? ''
      );
      return typeof result === 'string' ? result : null;
    },
    [open]
  );

  const value = useMemo<DialogContextValue>(
    () => ({ confirm, alert, prompt }),
    [confirm, alert, prompt]
  );

  const variantIcons: Record<DialogVariant, React.ReactNode> = {
    info: <Info className="w-5 h-5 text-primary" />,
    success: <CheckCircle2 className="w-5 h-5 text-green-500" />,
    warning: <AlertTriangle className="w-5 h-5 text-amber-500" />,
    danger: <XCircle className="w-5 h-5 text-red-500" />,
  };

  const mode = state?.mode;
  const isPrompt = mode === 'prompt';
  const hasCancel = mode === 'confirm' || isPrompt;
  const defaultTitle =
    mode === 'alert' ? t('common.notice_title') : t('common.confirm_title');

  // 取消 / 关闭：prompt 返回 null，其余返回 false。
  const cancel = () => finish(isPrompt ? null : false);
  const accept = () => finish(isPrompt ? inputValue : true);

  return (
    <DialogContext.Provider value={value}>
      {children}

      <Dialog.Root open={state !== null} onOpenChange={(next) => !next && cancel()}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-[60] bg-black/60 backdrop-blur-xs animate-in fade-in duration-200" />
          <Dialog.Content
            className={clsx(
              'fixed left-[50%] top-[50%] z-[60] translate-x-[-50%] translate-y-[-50%]',
              'w-full max-w-md bg-card border border-border rounded-md shadow-2xl p-6',
              'animate-in fade-in zoom-in-95 duration-200 focus:outline-none'
            )}
          >
            {state && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  accept();
                }}
              >
                <div className="flex items-start gap-3">
                  <span className="flex-shrink-0 mt-0.5">{variantIcons[state.variant]}</span>
                  <div className="min-w-0 flex-1">
                    <Dialog.Title className="text-base font-bold text-primary font-mono tracking-tight">
                      {state.title || defaultTitle}
                    </Dialog.Title>
                    <Dialog.Description asChild>
                      <div className="text-xs text-secondary mt-1.5 font-mono leading-relaxed whitespace-pre-line break-words">
                        {state.message}
                      </div>
                    </Dialog.Description>

                    {isPrompt && (
                      <div className="mt-3">
                        <Input
                          value={inputValue}
                          onChange={(e) => setInputValue(e.target.value)}
                          placeholder={state.placeholder}
                          className="font-mono"
                          autoFocus
                        />
                      </div>
                    )}
                  </div>
                </div>

                <div className="flex justify-end items-center gap-2 pt-5">
                  {hasCancel && (
                    <Button type="button" size="sm" variant="secondary" onClick={cancel}>
                      {state.cancelText || t('common.cancel')}
                    </Button>
                  )}
                  <Button
                    type="submit"
                    size="sm"
                    variant={state.variant === 'danger' ? 'error' : 'primary'}
                    autoFocus={!isPrompt}
                  >
                    {state.confirmText ||
                      (mode === 'alert' ? t('common.ok') : t('common.confirm'))}
                  </Button>
                </div>
              </form>
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </DialogContext.Provider>
  );
};

export const useDialog = (): DialogContextValue => {
  const ctx = useContext(DialogContext);
  if (!ctx) {
    throw new Error('useDialog must be used within a DialogProvider');
  }
  return ctx;
};
