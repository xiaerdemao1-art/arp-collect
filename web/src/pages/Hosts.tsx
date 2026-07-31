import { useState, useEffect } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Modal,
  Form,
  Input,
  message,
  Popconfirm,
  Upload,
  Typography,
  Tag,
} from 'antd';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ImportOutlined,
  ExportOutlined,
  UploadOutlined,
  ExperimentOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { UploadProps } from 'antd';

const { Title, Text } = Typography;

interface Host {
  id: number;
  name: string;
  ip: string;
  role: string;
  site: string;
  region: string;
  proxy: string;
  snmp_community: string;
}

const emptyHost: Host = {
  id: 0, name: '', ip: '', role: '', site: '', region: '', proxy: '', snmp_community: '',
};

const Hosts: React.FC = () => {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<Host>(emptyHost);
  const [importOpen, setImportOpen] = useState(false);
  const [testResult, setTestResult] = useState<any>(null);
  const [testOpen, setTestOpen] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [form] = Form.useForm();

  const fetchHosts = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/v1/hosts');
      const data = await res.json();
      setHosts(data.hosts || []);
    } catch (e) {
      message.error('加载主机列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchHosts(); }, []);

  const openAdd = () => {
    setEditing(emptyHost);
    form.setFieldsValue(emptyHost);
    setModalOpen(true);
  };

  const openEdit = (h: Host) => {
    setEditing(h);
    form.setFieldsValue(h);
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    const isEdit = editing.id > 0;
    const url = isEdit ? `/api/v1/hosts/${editing.id}` : '/api/v1/hosts';
    const method = isEdit ? 'PUT' : 'POST';
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(values),
    });
    if (!res.ok) {
      const txt = await res.text();
      message.error(`${isEdit ? '更新' : '新建'}失败: ${txt}`);
      return;
    }
    message.success(isEdit ? '已更新' : '已新建');
    setModalOpen(false);
    fetchHosts();
  };

  const del = async (id: number) => {
    const res = await fetch(`/api/v1/hosts/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      message.error('删除失败');
      return;
    }
    message.success('已删除');
    fetchHosts();
  };

  const testHost = async (h: Host) => {
    setTestingId(h.id);
    setTestOpen(true);
    setTestResult(null);
    try {
      const res = await fetch('/api/v1/snmp/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip: h.ip, community: h.snmp_community, name: h.name }),
      });
      setTestResult(await res.json());
    } catch {
      message.error('请求失败');
    } finally {
      setTestingId(null);
    }
  };

  const exportCSV = async () => {
    try {
      const res = await fetch('/api/v1/hosts/export');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'hosts.csv';
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      message.error('导出失败');
    }
  };

  // CSV 导入：读取文件内容后 POST 为 text/csv
  const uploadProps: UploadProps = {
    accept: '.csv',
    showUploadList: false,
    beforeUpload: (file) => {
      const reader = new FileReader();
      reader.onload = async () => {
        const text = String(reader.result || '');
        const res = await fetch('/api/v1/hosts/import', {
          method: 'POST',
          headers: { 'Content-Type': 'text/csv' },
          body: text,
        });
        if (!res.ok) {
          message.error(`导入失败: ${await res.text()}`);
          return;
        }
        const data = await res.json();
        message.success(`已导入 ${data.imported} 条`);
        setImportOpen(false);
        fetchHosts();
      };
      reader.readAsText(file);
      return false; // 阻止 antd 自动上传
    },
  };

  const columns: ColumnsType<Host> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'IP', dataIndex: 'ip', key: 'ip' },
    { title: '角色', dataIndex: 'role', key: 'role' },
    { title: '站点', dataIndex: 'site', key: 'site' },
    { title: '区域', dataIndex: 'region', key: 'region' },
    {
      title: '采集节点', dataIndex: 'proxy', key: 'proxy',
      render: (v: string) => v ? <Tag color="blue">{v}</Tag> : <Text type="secondary">自动</Text>,
    },
    {
      title: 'SNMP Community', dataIndex: 'snmp_community', key: 'snmp_community',
      render: (v: string) => v || <Text type="secondary">默认</Text>,
    },
    {
      title: '操作', key: 'action', width: 170,
      render: (_, h) => (
        <Space>
          <Button size="small" icon={<ExperimentOutlined />} onClick={() => testHost(h)} loading={testingId === h.id}>测试</Button>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(h)} />
          <Popconfirm title={`删除 ${h.name}？`} onConfirm={() => del(h.id)}>
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Title level={4}>主机管理</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
        手动管理采集设备清单。配置 <code>device_source.type: database</code> 时，Server 从此处读取设备。
        支持 CSV 批量导入（表头：name,ip,role,site,region,proxy,snmp_community）。
      </Text>

      <Card>
        <Space style={{ marginBottom: 16 }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>新增</Button>
          <Button icon={<ImportOutlined />} onClick={() => setImportOpen(true)}>导入 CSV</Button>
          <Button icon={<ExportOutlined />} onClick={exportCSV}>导出 CSV</Button>
          <Button onClick={fetchHosts}>刷新</Button>
        </Space>

        <Table
          columns={columns}
          dataSource={hosts}
          rowKey="id"
          loading={loading}
          size="small"
          pagination={{ pageSize: 50, showSizeChanger: true }}
        />
      </Card>

      {/* 新增/编辑弹窗 */}
      <Modal
        title={editing.id > 0 ? '编辑主机' : '新增主机'}
        open={modalOpen}
        onOk={submit}
        onCancel={() => setModalOpen(false)}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={emptyHost}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="core-sw-01" />
          </Form.Item>
          <Form.Item name="ip" label="管理 IP" rules={[{ required: true }]}>
            <Input placeholder="192.0.2.1" />
          </Form.Item>
          <Form.Item name="role" label="角色"><Input placeholder="CoreSwitch" /></Form.Item>
          <Form.Item name="site" label="站点"><Input placeholder="DC-East" /></Form.Item>
          <Form.Item name="region" label="区域"><Input placeholder="Region-East" /></Form.Item>
          <Form.Item name="proxy" label="采集节点（留空=自动分配）"><Input placeholder="proxy1" /></Form.Item>
          <Form.Item name="snmp_community" label="SNMP Community（留空=默认）"><Input placeholder="" /></Form.Item>
        </Form>
      </Modal>

      {/* 导入弹窗 */}
      <Modal
        title="导入 CSV"
        open={importOpen}
        onCancel={() => setImportOpen(false)}
        footer={[
          <Button key="close" onClick={() => setImportOpen(false)}>关闭</Button>,
        ]}
      >
        <Upload {...uploadProps}>
          <Button icon={<UploadOutlined />}>选择 CSV 文件</Button>
        </Upload>
        <Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
          CSV 格式：首行表头 <code>name,ip,role,site,region,proxy,snmp_community</code>，
          按名称去重，已存在则更新。可先"导出 CSV"作为模板。
        </Text>
      </Modal>

      {/* SNMP 测试结果弹窗 */}
      <Modal
        title="SNMP 测试结果"
        open={testOpen}
        onCancel={() => setTestOpen(false)}
        footer={[
          <Button key="close" onClick={() => setTestOpen(false)}>关闭</Button>,
        ]}
        width={720}
      >
        {testResult ? (
          <>
            <Space style={{ marginBottom: 16 }}>
              <Text>状态：</Text>
              {testResult.status === 'success'
                ? <Tag color="green">成功</Tag>
                : <Tag color="red">失败</Tag>}
              <Text>ARP 条目：<b>{testResult.arp_count}</b>{testResult.truncated ? '（已截断，仅采样）' : ''}</Text>
              <Text>耗时：<b>{testResult.duration_ms} ms</b></Text>
            </Space>
            {testResult.error && (
              <p style={{ color: 'red' }}>错误：{testResult.error}</p>
            )}
            {testResult.sample && testResult.sample.length > 0 && (
              <Table
                columns={[
                  { title: 'IP', dataIndex: 'ip', key: 'ip' },
                  { title: 'MAC', dataIndex: 'mac', key: 'mac' },
                  { title: '接口', dataIndex: 'interface', key: 'interface' },
                ]}
                dataSource={testResult.sample}
                rowKey={(r: any) => r.ip + r.mac}
                size="small"
                pagination={{ pageSize: 10 }}
              />
            )}
          </>
        ) : (
          <Text type="secondary">采集中…</Text>
        )}
      </Modal>
    </div>
  );
};

export default Hosts;
