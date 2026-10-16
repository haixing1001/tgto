/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#eef6ff',
          100: '#d9eaff',
          500: '#2f7df6',
          600: '#1f66d9',
          700: '#1a54b0'
        },
        panel: {
          light: '#ffffff',
          dark: '#171a21'
        }
      },
      fontFamily: {
        mono: ['JetBrains Mono', 'SFMono-Regular', 'Consolas', 'Liberation Mono', 'monospace']
      }
    }
  },
  plugins: []
}
