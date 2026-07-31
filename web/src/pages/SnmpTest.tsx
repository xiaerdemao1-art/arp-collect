import { useState } from 'react';
import {
  Card,
  Input,
  Button,
  Table,
  Tag,
  Space,
  message,
  Typography,
  Descriptions,
  Alert,
} from 'antd';
import { ExperimentOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';

const { Title, Text } = Typography;

interface ArpEntry {
  ip: string;
  mac: string;
  interface: string;
  type: string;
}

interface TestResult {
  status: string;
  arp_count: number;
  duration_ms: number;
  error: string;
  truncated: boolean;
  sample: ArpEntry[];
}

const SnmpTest: React.FC = () => {
  const [ip, setIp] = useState('');
  const [community, setCommunity] = useState('');
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<TestResult | null>(null);

  const test = async () => {
    if (!ip.trim()) {
      message.warning('请输入设备 IP');
      return;
    }
    setLoading(true);
    setResult(null);
    try {
      const res = await fetch('/api/v1/snmp/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip: ip.trim(), community: community.trim(), name: name.trim() }),
      });
      const data = await res.json();
      setResult(data);
      if (data.status === 'success') {
        message.success(`采集成功，共 ${data.arp_count} 条 ARP`);
      } else {
        message.error(data.error || '采集失败');
      }
    } catch {
      message.error('请求失败');
    } finally {
      setLoading(false);
    }
  };

  const columns: ColumnsType<ArpEntry> = [
    { title: 'IP', dataIndex: 'ip', key: 'ip' },
    { title: 'MAC', dataIndex: 'mac', key: 'mac' },
    { title: '接口', dataIndex: 'interface', key: 'interface' },
    { title: '类型', dataIndex: 'type', key: 'type' },
  ];

  return (
    <div>
      <Title level={4}>SNMP 测试</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
        对目标设备执行一次限量 SNMPv2c 采集（最多 50 条 ARP），验证连通性与采集能力。
        不会写入数据库。community 留空则使用全局 snmp.community 配置。
      </Text>

      <Card style={{ marginBottom: 16 }}>
        <Space wrap>
          <Input
            placeholder="设备 IP *"
            value={ip}
            onChange={(e) => setIp(e.target.value)}
            style={{ width: 200 }}
            onPressEnter={test}
          />
          <Input
            placeholder="SNMP Community（可选）"
            value={community}
            onChange={(e) => setCommunity(e.target.value)}
            style={{ width: 200 }}
            onPressEnter={test}
          />
          <Input
            placeholder="设备名（可选）"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: 200 }}
            onPressEnter={test}
          />
          <Button type="primary" icon={<ExperimentOutlined />} loading={loading} onClick={test}>
            测试
          </Button>
        </Space>
      </Card>

      {result && (
        <Card title="测试结果">
          <Descriptions size="small" column={3} bordered style={{ marginBottom: 16 }}>
            <Descriptions.Item label="状态">
              {result.status === 'success'
                ? <Tag color="green">成功</Tag>
                : <Tag color="red">失败</Tag>}
            </Descriptions.Item>
            <Descriptions.Item label="ARP 条目数">
              {result.arp_count}{result.truncated ? '（已截断，仅采样）' : ''}
            </Descriptions.Item>
            <Descriptions.Item label="耗时">{result.duration_ms} ms</Descriptions.Item>
          </Descriptions>

          {result.error && (
            <Alert
              type="error"
              message="错误信息"
              description={result.error}
              style={{ marginBottom: 16 }}
              showIcon
            />
          )}

          {result.sample && result.sample.length > 0 ? (
            <Table
              columns={columns}
              dataSource={result.sample}
              rowKey={(r) => r.ip + r.mac}
              size="small"
              pagination={{ pageSize: 10 }}
            />
          ) : (
            result.status === 'success' && <Text type="secondary">设备返回 0 条 ARP 记录</Text>
          )}
        </Card>
      )}
    </div>
  );
};

export default SnmpTest;
