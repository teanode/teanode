/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./src/**/*.{ts,tsx,html}'],
  theme: {
    extend: {
      colors: {
        bg: '#0f0f0f',
        surface: '#1a1a1a',
        surface2: '#252525',
        border: '#333',
        dim: '#888',
        accent: '#729d39',
        'accent-dim': '#5a6a35',
        'user-bg': '#1e2a14',
        'tool-bg': '#1e1e14',
        'code-bg': '#111',
        danger: '#c66',
        'danger-dim': '#5a2a2a',
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'Roboto', 'sans-serif'],
        mono: ['"SF Mono"', '"Fira Code"', '"Cascadia Code"', 'Consolas', 'monospace'],
      },
      borderRadius: {
        DEFAULT: '8px',
      },
    },
  },
  plugins: [],
};
