import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Card,
  Row,
  Col,
  Statistic,
  Table,
  Input,
  Select,
  Button,
  Tag,
  Space,
  message,
} from 'antd';
import {
  ReloadOutlined,
  DownloadOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  DatabaseOutlined,
  FieldTimeOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';

interface Device {
  id: number;
  name: string;
  mgmt_ip: string;
  role: string;
  site: string;
  region: string;
  proxy_name: string;
  arp_count: number;
  duration_ms: number;
  collected_at: string;
}

const Dashboard: React.FC = () => {
  const [searchParams] = useSearchParams();
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchText, setSearchText] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>(searchParams.get('status') || '');
  const [roleFilter, setRoleFilter] = useState<string>('');
  const [selectedProxy, setSelectedProxy] = useState<string>('');

  useEffect(() => {
    fetchDevices();
  }, []);

  useEffect(() => {
    const status = searchParams.get('status');
    if (status) setStatusFilter(status);
  }, [searchParams]);

  const fetchDevices = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/v1/devices');
      const data = await res.json();
      setDevices(data.devices || []);
    } catch (e) {
      message.error('获取设备列表失败');
    } finally {
      setLoading(false);
    }
  };

  // 统计计算
  const stats = {
    total: devices.length,
    success: devices.filter(d => d.arp_count > 0).length,
    failed: devices.filter(d => d.collected_at && d.arp_count === 0).length,
    pending: devices.filter(d => !d.collected_at && d.arp_count === 0).length,
    arpCount: devices.reduce((sum, d) => sum + (d.arp_count || 0), 0),
    avgDuration: devices.filter(d => d.duration_ms > 0).length > 0
      ? Math.round(devices.filter(d => d.duration_ms > 0).reduce((sum, d) => sum + d.duration_ms, 0) / devices.filter(d => d.duration_ms > 0).length)
      : 0,
  };

  // Proxy 统计
  const proxyStats = devices.reduce((acc: { [key: string]: number }, d) => {
    const name = d.proxy_name || 'unknown';
    acc[name] = (acc[name] || 0) + 1;
    return acc;
  }, {});

  // 过滤设备
  const filteredDevices = devices.filter(d => {
    if (searchText) {
      const s = searchText.toLowerCase();
      if (!(d.name?.toLowerCase().includes(s) || d.mgmt_ip?.includes(s) || d.site?.toLowerCase().includes(s))) return false;
    }
    if (statusFilter === 'success' && !(d.arp_count > 0)) return false;
    if (statusFilter === 'failed' && !(d.collected_at && d.arp_count === 0)) return false;
    if (statusFilter === 'pending' && (d.arp_count > 0 || (d.collected_at && d.arp_count === 0))) return false;
    if (roleFilter && !d.role?.includes(roleFilter)) return false;
    if (selectedProxy && d.proxy_name !== selectedProxy) return false;
    return true;
  });

  const columns: ColumnsType<Device> = [
    {
      title: '状态',
      key: 'status',
      width: 80,
      fixed: 'left',
      render: (_, record) => {
        if (record.arp_count > 0) return <Tag color="success">成功</Tag>;
        if (record.collected_at) return <Tag color="error">失败</Tag>;
        return <Tag color="warning">待采集</Tag>;
      },
    },
    { title: '设备名称', dataIndex: 'name', key: 'name', width: 260, ellipsis: true },
    { title: '管理 IP', dataIndex: 'mgmt_ip', key: 'mgmt_ip', width: 130 },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 100,
      render: (role) => (
        <Tag color={role?.includes('DSW') ? 'orange' : 'blue'}>{role}</Tag>
      ),
    },
    { title: '站点', dataIndex: 'site', key: 'site', width: 180, ellipsis: true },
    { title: 'Proxy', dataIndex: 'proxy_name', key: 'proxy_name', width: 150, ellipsis: true },
    {
      title: 'ARP数量',
      dataIndex: 'arp_count',
      key: 'arp_count',
      width: 100,
      sorter: (a, b) => (a.arp_count || 0) - (b.arp_count || 0),
      render: (count) => <span style={{ color: count > 0 ? '#1890ff' : '#999', fontWeight: count > 0 ? 600 : 400 }}>{count || 0}</span>,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      width: 90,
      sorter: (a, b) => (a.duration_ms || 0) - (b.duration_ms || 0),
      render: (ms) => ms ? `${ms}ms` : '-',
    },
    {
      title: '采集时间',
      dataIndex: 'collected_at',
      key: 'collected_at',
      width: 160,
      render: (t) => t ? new Date(t).toLocaleString('zh-CN') : '-',
    },
  ];

  const exportCSV = () => {
    const headers = ['设备名称', '管理IP', '角色', '站点', 'Proxy', 'ARP数量', '耗时(ms)', '采集时间'];
    const rows = filteredDevices.map(d => [d.name, d.mgmt_ip, d.role, d.site, d.proxy_name, d.arp_count || 0, d.duration_ms || '', d.collected_at || '']);
    const csv = [headers, ...rows].map(r => r.map(c => `"${c}"`).join(',')).join('\n');
    const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `devices_${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const clearFilters = () => {
    setSearchText('');
    setStatusFilter('');
    setRoleFilter('');
    setSelectedProxy('');
  };

  return (
    <div>
      {/* 统计卡片 */}
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col xs={24} sm={12} md={8} lg={4} xl={4}>
          <Card hoverable onClick={clearFilters} style={{ cursor: 'pointer' }}>
            <Statistic
              title="📊 设备总数"
              value={stats.total}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={8} lg={5} xl={5}>
          <Card
            hoverable
            onClick={() => setStatusFilter(statusFilter === 'success' ? '' : 'success')}
            style={{ cursor: 'pointer', borderColor: statusFilter === 'success' ? '#52c41a' : undefined }}
          >
            <Statistic
              title={<><CheckCircleOutlined style={{ color: '#52c41a' }} /> 采集成功</>}
              value={stats.success}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={8} lg={5} xl={5}>
          <Card
            hoverable
            onClick={() => setStatusFilter(statusFilter === 'failed' ? '' : 'failed')}
            style={{ cursor: 'pointer', borderColor: statusFilter === 'failed' ? '#ff4d4f' : undefined }}
          >
            <Statistic
              title={<><CloseCircleOutlined style={{ color: '#ff4d4f' }} /> 采集失败</>}
              value={stats.failed}
              valueStyle={{ color: '#ff4d4f' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={8} lg={5} xl={5}>
          <Card hoverable>
            <Statistic
              title={<><DatabaseOutlined style={{ color: '#faad14' }} /> ARP 条目</>}
              value={stats.arpCount}
              valueStyle={{ color: '#faad14' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={8} lg={5} xl={5}>
          <Card hoverable>
            <Statistic
              title={<><FieldTimeOutlined style={{ color: '#722ed1' }} /> 平均耗时</>}
              value={stats.avgDuration}
              suffix="ms"
              valueStyle={{ color: '#722ed1' }}
            />
          </Card>
        </Col>
      </Row>

      {/* Proxy 分布 */}
      <Card title="📍 Proxy 设备分布" size="small" style={{ marginBottom: 24 }}>
        <Space wrap>
          <Button
            type={selectedProxy === '' ? 'primary' : 'default'}
            onClick={() => setSelectedProxy('')}
          >
            全部 ({stats.total})
          </Button>
          {Object.entries(proxyStats)
            .sort((a, b) => b[1] - a[1])
            .map(([name, count]) => (
              <Button
                key={name}
                type={selectedProxy === name ? 'primary' : 'default'}
                onClick={() => setSelectedProxy(selectedProxy === name ? '' : name)}
              >
                {name.substring(0, 12)} ({count})
              </Button>
            ))}
        </Space>
      </Card>

      {/* 设备列表 */}
      <Card
        title="🖥️ 设备列表"
        extra={
          <Space>
            <Input.Search
              placeholder="搜索设备名称、IP、站点..."
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
              style={{ width: 250 }}
              allowClear
            />
            <Select
              placeholder="全部角色"
              value={roleFilter}
              onChange={setRoleFilter}
              style={{ width: 120 }}
              allowClear
              options={[
                { value: '', label: '全部角色' },
                { value: 'CSW', label: 'CSW' },
                { value: 'DSW', label: 'DSW' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={fetchDevices} loading={loading}>
              刷新
            </Button>
            <Button icon={<DownloadOutlined />} onClick={exportCSV}>
              导出CSV
            </Button>
          </Space>
        }
      >
        <Table
          columns={columns}
          dataSource={filteredDevices}
          rowKey="id"
          loading={loading}
          scroll={{ x: 1200 }}
          pagination={{
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 台设备`,
            defaultPageSize: 50,
          }}
          size="middle"
        />
      </Card>
    </div>
  );
};

export default Dashboard;
