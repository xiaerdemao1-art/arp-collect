import { Routes, Route, Navigate } from 'react-router-dom';
import MainLayout from './layouts/MainLayout';
import Dashboard from './pages/Dashboard';
import ArpSearch from './pages/ArpSearch';

// 认证由服务端的 Basic Auth 统一负责（见 server.web_auth 配置），
// SPA 只在通过认证后才会被下发，前端不再做二次登录校验。
function App() {
  return (
    <Routes>
      <Route path="/" element={<MainLayout />}>
        <Route index element={<Dashboard />} />
        <Route path="arp" element={<ArpSearch />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

export default App;
