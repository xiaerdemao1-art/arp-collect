import { useState, useEffect } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu, Typography, Badge, Select, Tooltip, ConfigProvider, theme } from 'antd';
import {
  DashboardOutlined,
  SearchOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ClockCircleOutlined,
  BgColorsOutlined,
} from '@ant-design/icons';
import type { MenuProps } from 'antd';
import { themes, getCurrentTheme, setCurrentTheme, applyTheme, type ThemeName } from '../themes';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

interface MainLayoutProps {
  onThemeChange?: (theme: ThemeName) => void;
}

const MainLayout: React.FC<MainLayoutProps> = ({ onThemeChange }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);
  const [stats, setStats] = useState({ total: 0, success: 0, failed: 0, pending: 0 });
  const [currentTheme, setCurrentThemeState] = useState<ThemeName>(getCurrentTheme());

  useEffect(() => {
    fetchStats();
    // 应用保存的主题
    applyTheme(themes[currentTheme]);
  }, []);

  const fetchStats = async () => {
    try {
      const res = await fetch('/api/v1/devices');
      const data = await res.json();
      const devices = data.devices || [];
      const success = devices.filter((d: { arp_count: number }) => d.arp_count > 0).length;
      const failed = devices.filter((d: { collected_at: string; arp_count: number }) => d.collected_at && d.arp_count === 0).length;
      setStats({
        total: devices.length,
        success,
        failed,
        pending: devices.length - success - failed,
      });
    } catch (e) {
      console.error('获取统计失败:', e);
    }
  };

  const handleThemeChange = (themeName: ThemeName) => {
    setCurrentThemeState(themeName);
    setCurrentTheme(themeName);
    applyTheme(themes[themeName]);
    onThemeChange?.(themeName);
  };

  const themeConfig = themes[currentTheme];
  const isDark = currentTheme === 'dark';

  const menuItems: MenuProps['items'] = [
    {
      key: 'monitor',
      label: '监控',
      type: 'group',
      children: [
        {
          key: '/',
          icon: <DashboardOutlined />,
          label: (
            <span>
              仪表盘
              <Badge count={stats.total} style={{ marginLeft: 8 }} showZero color={themeConfig.primaryColor} />
            </span>
          ),
        },
      ],
    },
    {
      key: 'query',
      label: '查询',
      type: 'group',
      children: [
        {
          key: '/arp',
          icon: <SearchOutlined />,
          label: 'ARP 检索',
        },
      ],
    },
    {
      key: 'status',
      label: '采集状态',
      type: 'group',
      children: [
        {
          key: 'success',
          icon: <CheckCircleOutlined style={{ color: themeConfig.successColor }} />,
          label: (
            <span>
              采集成功
              <Badge count={stats.success} style={{ marginLeft: 8 }} showZero color={themeConfig.successColor} />
            </span>
          ),
        },
        {
          key: 'failed',
          icon: <CloseCircleOutlined style={{ color: themeConfig.errorColor }} />,
          label: (
            <span>
              采集失败
              <Badge count={stats.failed} style={{ marginLeft: 8 }} showZero color={themeConfig.errorColor} />
            </span>
          ),
        },
        {
          key: 'pending',
          icon: <ClockCircleOutlined style={{ color: themeConfig.warningColor }} />,
          label: (
            <span>
              待采集
              <Badge count={stats.pending} style={{ marginLeft: 8 }} showZero color={themeConfig.warningColor} />
            </span>
          ),
        },
      ],
    },
  ];

  const handleMenuClick: MenuProps['onClick'] = (e) => {
    if (e.key.startsWith('/')) {
      navigate(e.key);
    } else {
      navigate(`/?status=${e.key}`);
    }
  };

  return (
    <ConfigProvider
      theme={{
        algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: themeConfig.primaryColor,
          colorBgContainer: themeConfig.cardBg,
          colorBgLayout: themeConfig.backgroundColor,
          colorSuccess: themeConfig.successColor,
          colorError: themeConfig.errorColor,
          colorWarning: themeConfig.warningColor,
          fontFamily: themeConfig.fontFamily || '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        },
      }}
    >
      <Layout style={{ minHeight: '100vh', background: themeConfig.backgroundColor }}>
        <Sider
          collapsible
          collapsed={collapsed}
          onCollapse={setCollapsed}
          theme={isDark ? 'dark' : 'light'}
          width={240}
          style={{
            overflow: 'auto',
            height: '100vh',
            position: 'fixed',
            left: 0,
            top: 0,
            bottom: 0,
            background: isDark ? themeConfig.siderBg : (currentTheme === 'vangogh' ? themeConfig.siderBg : '#001529'),
            borderRight: currentTheme === 'vangogh' ? `1px solid ${themeConfig.borderColor}` : 'none',
          }}
        >
          <div style={{
            height: 64,
            display: 'flex',
            alignItems: 'center',
            justifyContent: collapsed ? 'center' : 'flex-start',
            padding: collapsed ? 0 : '0 16px',
            borderBottom: `1px solid ${isDark ? 'rgba(255,255,255,0.1)' : (currentTheme === 'vangogh' ? themeConfig.borderColor : 'rgba(255,255,255,0.1)')}`,
          }}>
            <div style={{
              width: 32,
              height: 32,
              background: `linear-gradient(135deg, ${themeConfig.gradientStart}, ${themeConfig.gradientEnd})`,
              borderRadius: 6,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 16,
            }}>
              {currentTheme === 'vangogh' ? '🌻' : '📡'}
            </div>
            {!collapsed && (
              <span style={{ 
                marginLeft: 12, 
                color: currentTheme === 'vangogh' ? themeConfig.textColor : '#fff', 
                fontSize: 16, 
                fontWeight: 600 
              }}>
                ARP Collect
              </span>
            )}
          </div>
          <Menu
            theme={currentTheme === 'vangogh' ? 'light' : 'dark'}
            mode="inline"
            selectedKeys={[location.pathname]}
            items={menuItems}
            onClick={handleMenuClick}
            style={{ 
              borderRight: 0,
              background: 'transparent',
            }}
          />
        </Sider>

        <Layout style={{ marginLeft: collapsed ? 80 : 240, transition: 'margin-left 0.2s', background: themeConfig.backgroundColor }}>
          <Header style={{
            background: themeConfig.headerBg,
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: `1px solid ${themeConfig.borderColor}`,
            position: 'sticky',
            top: 0,
            zIndex: 10,
          }}>
            <Text style={{ fontSize: 18, fontWeight: 600, color: themeConfig.textColor }}>
              {location.pathname === '/' ? '📊 仪表盘' : '🔍 ARP 检索'}
            </Text>
            <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
              {/* 主题切换 */}
              <Tooltip title="切换主题">
                <Select
                  value={currentTheme}
                  onChange={handleThemeChange}
                  style={{ width: 130 }}
                  suffixIcon={<BgColorsOutlined />}
                  options={Object.entries(themes).map(([key, t]) => ({
                    value: key,
                    label: t.label,
                  }))}
                />
              </Tooltip>
              <Text type="secondary" style={{ fontSize: 13 }}>
                🕐 {new Date().toLocaleString('zh-CN')}
              </Text>
            </div>
          </Header>
          <Content style={{ margin: 24, minHeight: 280 }}>
            <Outlet />
          </Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
};

export default MainLayout;
