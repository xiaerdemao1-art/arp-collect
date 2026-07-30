import { useState } from 'react';
import {
  Card,
  Input,
  Table,
  Tag,
  Space,
  message,
  Typography,
  Tooltip,
  Button,
} from 'antd';
import {
  DownloadOutlined,
  CopyOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import SunflowerIcon from '../components/SunflowerIcon';

const { Text, Title } = Typography;

interface ArpRecord {
  ip: string;
  mac: string;
  interface: string;
  type: string;
  device_name: string;
  device_ip: string;
  site: string;
  proxy_name: string;
  collected_at: string;
  created_at: string;
}

const ArpSearch: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [results, setResults] = useState<ArpRecord[]>([]);
  const [searched, setSearched] = useState(false);
  const [searchValue, setSearchValue] = useState('');

  const onSearch = async (value: string) => {
    const query = value.trim();
    if (!query) {
      message.warning('请输入搜索条件');
      return;
    }

    setLoading(true);
    setSearched(true);
    try {
      const res = await fetch(`/api/v1/arp/search?q=${encodeURIComponent(query)}`);
      const data = await res.json();
      if (data.error) {
        message.error(data.error);
        setResults([]);
      } else {
        setResults(data.records || []);
        if ((data.records || []).length === 0) {
          message.info('未找到匹配的 ARP 记录');
        } else {
          message.success(`找到 ${data.records.length} 条记录`);
        }
      }
    } catch {
      message.error('搜索失败');
      setResults([]);
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      message.success('已复制');
    });
  };

  const formatTime = (t: string) => {
    if (!t) return '-';
    return new Date(t).toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  };

  const columns: ColumnsType<ArpRecord> = [
    {
      title: 'IP 地址',
      dataIndex: 'ip',
      key: 'ip',
      width: 140,
      render: (ip) => (
        <Tooltip title="点击复制">
          <Text copyable={{ text: ip, tooltips: false }} style={{ cursor: 'pointer' }}>
            {ip}
          </Text>
        </Tooltip>
      ),
    },
    {
      title: 'MAC 地址',
      dataIndex: 'mac',
      key: 'mac',
      width: 160,
      render: (mac) => (
        <Tooltip title="点击复制">
          <Text code style={{ cursor: 'pointer' }} onClick={() => copyToClipboard(mac)}>
            {mac}
          </Text>
        </Tooltip>
      ),
    },
    {
      title: '接口',
      dataIndex: 'interface',
      key: 'interface',
      width: 120,
    },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      width: 80,
      render: (type) => (
        <Tag color={type === 'dynamic' ? 'blue' : 'orange'}>{type}</Tag>
      ),
    },
    {
      title: '设备名称',
      dataIndex: 'device_name',
      key: 'device_name',
      width: 400,
      ellipsis: true,
    },
    {
      title: '设备 IP',
      dataIndex: 'device_ip',
      key: 'device_ip',
      width: 130,
    },
    {
      title: '站点',
      dataIndex: 'site',
      key: 'site',
      width: 160,
      ellipsis: true,
    },
    {
      title: '首次发现',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
      render: (t) => (
        <Tooltip title="首次采集到该 IP 的时间">
          <span style={{ color: '#8c8c8c' }}>{formatTime(t)}</span>
        </Tooltip>
      ),
    },
    {
      title: '上次可见',
      dataIndex: 'collected_at',
      key: 'collected_at',
      width: 160,
      render: (t) => (
        <Tooltip title="最后一次采集到该 IP 的时间">
          <span>{formatTime(t)}</span>
        </Tooltip>
      ),
    },
    {
      title: 'Proxy',
      dataIndex: 'proxy_name',
      key: 'proxy_name',
      width: 140,
      render: (name) => (
        <Tooltip title={name}>
          <Tag color="geekblue">{name || '-'}</Tag>
        </Tooltip>
      ),
    },
  ];

  const exportCSV = () => {
    if (results.length === 0) {
      message.warning('无数据可导出');
      return;
    }
    const headers = ['IP地址', 'MAC地址', '接口', '类型', '设备名称', '设备IP', '站点', '首次发现', '上次可见', 'Proxy'];
    const rows = results.map(r => [
      r.ip, r.mac, r.interface, r.type,
      r.device_name, r.device_ip, r.site,
      formatTime(r.created_at), formatTime(r.collected_at), r.proxy_name,
    ]);
    const csv = [headers, ...rows].map(row => row.map(c => `"${c || ''}"`).join(',')).join('\n');
    const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `arp_search_${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div style={{ maxWidth: 1600, margin: '0 auto', padding: '40px 20px' }}>
      {/* 搜索区域 - 居中显示 */}
      <div style={{ textAlign: 'center', marginBottom: 48 }}>
        {/* 向日葵图标 */}
        <div style={{ marginBottom: 24 }}>
          <SunflowerIcon size={100} />
        </div>
        
        <Title level={2} style={{ marginBottom: 8, color: 'var(--text-color)' }}>
          ARP 记录检索
        </Title>
        <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>
          支持搜索 IP 地址、MAC 地址、设备名称、Proxy 名称
        </Text>
        
        {/* 统一搜索框 */}
        <div style={{ maxWidth: 600, margin: '0 auto' }}>
          <Input.Search
            placeholder="输入 IP / MAC / 设备名 / Proxy 进行搜索..."
            size="large"
            allowClear
            enterButton="🔍 搜索"
            value={searchValue}
            onChange={(e) => setSearchValue(e.target.value)}
            onSearch={onSearch}
            loading={loading}
            style={{
              boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
              borderRadius: 8,
            }}
          />
        </div>
        
        <div style={{ marginTop: 12 }}>
          <Text type="secondary" style={{ fontSize: 13 }}>
            💡 示例：<code>192.0.2.1</code> · <code>aa:bb:cc:dd:ee:ff</code> · <code>proxy1</code> · <code>CORE_SW</code>
          </Text>
        </div>
      </div>

      {/* 搜索结果 */}
      {searched && (
        <Card
          title={
            <Space>
              <span>📋 查询结果</span>
              <Tag color="blue">{results.length} 条记录</Tag>
            </Space>
          }
          extra={
            <Space>
              {results.length > 0 && (
                <>
                  <Button
                    icon={<CopyOutlined />}
                    onClick={() => {
                      const text = results.map(r => `${r.ip}\t${r.mac}\t${r.device_name}`).join('\n');
                      copyToClipboard(text);
                    }}
                  >
                    复制
                  </Button>
                  <Button icon={<DownloadOutlined />} onClick={exportCSV}>
                    导出 CSV
                  </Button>
                </>
              )}
            </Space>
          }
        >
          <Table
            columns={columns}
            dataSource={results}
            rowKey={(r) => `${r.ip}-${r.mac}-${r.device_ip}`}
            loading={loading}
            scroll={{ x: 1500 }}
            pagination={{
              showSizeChanger: true,
              showTotal: (total) => `共 ${total} 条记录`,
              defaultPageSize: 50,
              pageSizeOptions: ['20', '50', '100', '200'],
            }}
            size="middle"
          />
        </Card>
      )}
    </div>
  );
};

export default ArpSearch;
