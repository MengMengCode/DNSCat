/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        bg: 'var(--bg-100)',
        'bg-subtle': 'var(--bg-200)',
        primary: 'var(--text-primary)',
        secondary: 'var(--text-secondary)',
        tertiary: 'var(--text-tertiary)',
        border: 'var(--border-subtle)',
        'border-hover': 'var(--border-hover)',
        'border-muted': 'var(--border-muted)',
        card: 'var(--card-bg)',
        blue: {
          500: 'var(--accent-blue)',
          600: 'var(--accent-blue-hover)',
          900: 'var(--accent-blue-bg)',
        },
        green: {
          500: 'var(--accent-green)',
          900: 'var(--accent-green-bg)',
        },
        red: {
          500: 'var(--accent-red)',
          900: 'var(--accent-red-bg)',
        },
        amber: {
          500: 'var(--accent-amber)',
          900: 'var(--accent-amber-bg)',
        },
        gray: {
          50: 'var(--gray-50)',
          100: 'var(--gray-100)',
          200: 'var(--gray-200)',
          300: 'var(--gray-300)',
          400: 'var(--gray-400)',
          500: 'var(--gray-500)',
          600: 'var(--gray-600)',
          700: 'var(--gray-700)',
          800: 'var(--gray-800)',
          900: 'var(--gray-900)',
          1000: 'var(--gray-1000)',
        }
      },
      borderRadius: {
        sm: '6px',
        DEFAULT: '8px',
        md: '12px',
        lg: '16px',
      },
      fontFamily: {
        // 全局统一等宽字体：font-sans / font-mono 及默认基础字体都解析为 Geist Mono
        sans: ['Geist Mono', 'monospace'],
        mono: ['Geist Mono', 'monospace'],
      },
      boxShadow: {
        sm: 'var(--shadow-sm)',
        card: 'var(--shadow-card)',
        popover: 'var(--shadow-popover)',
        modal: 'var(--shadow-modal)',
      }
    },
  },
  plugins: [],
}
