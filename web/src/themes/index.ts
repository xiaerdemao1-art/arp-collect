// 主题配置
export interface ThemeConfig {
  name: string;
  label: string;
  primaryColor: string;
  backgroundColor: string;
  headerBg: string;
  siderBg: string;
  textColor: string;
  borderColor: string;
  cardBg: string;
  successColor: string;
  errorColor: string;
  warningColor: string;
  // 特殊样式
  gradientStart?: string;
  gradientEnd?: string;
  fontFamily?: string;
}

// 深色主题
export const darkTheme: ThemeConfig = {
  name: 'dark',
  label: '🌙 深色',
  primaryColor: '#d97706',
  backgroundColor: '#1a1a1a',
  headerBg: '#262626',
  siderBg: '#1f1f1f',
  textColor: '#e5e5e5',
  borderColor: '#404040',
  cardBg: '#262626',
  successColor: '#10b981',
  errorColor: '#ef4444',
  warningColor: '#f59e0b',
  gradientStart: '#d97706',
  gradientEnd: '#ea580c',
};

// 梵高向日葵浅色主题
export const vanGoghTheme: ThemeConfig = {
  name: 'vangogh',
  label: '🌻 向日葵',
  primaryColor: '#d97706',
  backgroundColor: '#fef7ed',
  headerBg: '#fffbf5',
  siderBg: '#fff8f0',
  textColor: '#78350f',
  borderColor: '#fed7aa',
  cardBg: '#ffffff',
  successColor: '#65a30d',
  errorColor: '#dc2626',
  warningColor: '#ea580c',
  gradientStart: '#fbbf24',
  gradientEnd: '#f59e0b',
  fontFamily: '"Georgia", "Noto Serif SC", serif',
};

// 默认蓝色主题
export const defaultTheme: ThemeConfig = {
  name: 'default',
  label: '💙 默认',
  primaryColor: '#1890ff',
  backgroundColor: '#f0f2f5',
  headerBg: '#ffffff',
  siderBg: '#001529',
  textColor: '#000000',
  borderColor: '#d9d9d9',
  cardBg: '#ffffff',
  successColor: '#52c41a',
  errorColor: '#ff4d4f',
  warningColor: '#faad14',
  gradientStart: '#1890ff',
  gradientEnd: '#096dd9',
};

export const themes = {
  default: defaultTheme,
  dark: darkTheme,
  vangogh: vanGoghTheme,
};

export type ThemeName = keyof typeof themes;

// 获取当前主题
export const getCurrentTheme = (): ThemeName => {
  return (localStorage.getItem('arp_theme') as ThemeName) || 'default';
};

// 设置主题
export const setCurrentTheme = (theme: ThemeName) => {
  localStorage.setItem('arp_theme', theme);
};

// 应用主题到 CSS 变量
export const applyTheme = (theme: ThemeConfig) => {
  const root = document.documentElement;
  root.style.setProperty('--primary-color', theme.primaryColor);
  root.style.setProperty('--bg-color', theme.backgroundColor);
  root.style.setProperty('--header-bg', theme.headerBg);
  root.style.setProperty('--sider-bg', theme.siderBg);
  root.style.setProperty('--text-color', theme.textColor);
  root.style.setProperty('--border-color', theme.borderColor);
  root.style.setProperty('--card-bg', theme.cardBg);
  root.style.setProperty('--success-color', theme.successColor);
  root.style.setProperty('--error-color', theme.errorColor);
  root.style.setProperty('--warning-color', theme.warningColor);
  root.style.setProperty('--gradient-start', theme.gradientStart || theme.primaryColor);
  root.style.setProperty('--gradient-end', theme.gradientEnd || theme.primaryColor);
  if (theme.fontFamily) {
    root.style.setProperty('--font-family', theme.fontFamily);
  } else {
    root.style.setProperty('--font-family', '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif');
  }
  
  // 设置 body 类名用于特殊样式
  document.body.className = `theme-${theme.name}`;
};
