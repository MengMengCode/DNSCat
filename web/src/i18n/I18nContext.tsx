import React, { createContext, useContext, useState, useEffect } from 'react';
import { Language, translations } from './locales';

interface I18nContextType {
  language: Language;
  setLanguage: (lang: Language) => void;
  t: (key: string) => string;
}

const I18nContext = createContext<I18nContextType | undefined>(undefined);

export const I18nProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [language, setLanguageState] = useState<Language>('zh-CN');

  useEffect(() => {
    const saved = localStorage.getItem('dnscat_lang') as Language;
    if (saved === 'zh-CN' || saved === 'en-US') {
      setLanguageState(saved);
    }
  }, []);

  const setLanguage = (lang: Language) => {
    setLanguageState(lang);
    localStorage.setItem('dnscat_lang', lang);
  };

  const t = (key: string): string => {
    const dict = translations[language] || translations['zh-CN'];
    return (dict as any)[key] || key;
  };

  return (
    <I18nContext.Provider value={{ language, setLanguage, t }}>
      {children}
    </I18nContext.Provider>
  );
};

export const useI18n = (): I18nContextType => {
  const ctx = useContext(I18nContext);
  if (!ctx) {
    throw new Error('useI18n must be used within an I18nProvider');
  }
  return ctx;
};
