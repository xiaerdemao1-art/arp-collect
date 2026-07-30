package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"go.uber.org/zap"
)

// ARPRecord Kafka 消息格式（与 Vector/Zabbix 标签对齐）
type ARPRecord struct {
	// 设备元信息（与 NetBox 数据对齐）
	DeviceID     int    `json:"device_id"`
	DeviceName   string `json:"device_name"`
	MgmtIP       string `json:"mgmt_ip"`
	Role         string `json:"role"`
	Manufacturer string `json:"manufacturer"`
	Site         string `json:"site"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	RegionLevel1 string `json:"region_level_1"`
	RegionLevel2 string `json:"region_level_2"`
	RegionLevel3 string `json:"region_level_3"`
	ProxyName    string `json:"proxy_name"`

	// ARP 条目
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Interface string `json:"interface"`
	Type      string `json:"type"`

	// 时间戳
	Timestamp time.Time `json:"timestamp"`
}

// Producer Kafka 生产者
type Producer struct {
	writer *kafka.Writer
	logger *zap.Logger
	topic  string
}

// compressionCodec 按配置名映射 kafka-go 压缩算法
func compressionCodec(name string) kafka.Compression {
	switch strings.ToLower(name) {
	case "gzip":
		return kafka.Gzip
	case "snappy":
		return kafka.Snappy
	case "lz4":
		return kafka.Lz4
	case "zstd":
		return kafka.Zstd
	case "none", "":
		return 0 // compress.None，不压缩
	default:
		return kafka.Lz4
	}
}

// NewProducer 创建 Kafka 生产者
func NewProducer(cfg *config.KafkaConfig, logger *zap.Logger) (*Producer, error) {
	if !cfg.Enabled {
		logger.Info("Kafka 已禁用")
		return nil, nil
	}

	// 配置 SASL 认证（仅在配置了用户名时启用，兼容无认证集群）
	var mechanism *plain.Mechanism
	if cfg.Username != "" {
		mechanism = &plain.Mechanism{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	// 配置 TLS
	var tlsConfig *tls.Config
	if cfg.TLS {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	// 首先确保 Topic 存在
	if err := ensureTopicExists(cfg, mechanism, tlsConfig, logger); err != nil {
		logger.Warn("创建 Topic 失败，将尝试继续", zap.Error(err))
	}

	// 创建 Writer
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(cfg.Brokers...),
		Topic:                  cfg.Topic,
		Balancer:               &kafka.LeastBytes{},
		BatchSize:              cfg.BatchSize,
		BatchTimeout:           time.Duration(cfg.BatchTimeout) * time.Second,
		Async:                  cfg.Async,
		Compression:            compressionCodec(cfg.Compression),
		AllowAutoTopicCreation: true, // 启用自动创建 Topic
		Transport: &kafka.Transport{
			SASL: mechanism,
			TLS:  tlsConfig,
		},
	}

	logger.Info("Kafka Producer 初始化成功",
		zap.Strings("brokers", cfg.Brokers),
		zap.String("topic", cfg.Topic))

	return &Producer{
		writer: writer,
		logger: logger,
		topic:  cfg.Topic,
	}, nil
}

// ensureTopicExists 确保 Topic 存在，如果不存在则创建
func ensureTopicExists(cfg *config.KafkaConfig, mechanism *plain.Mechanism, tlsConfig *tls.Config, logger *zap.Logger) error {
	// 创建带认证的 Dialer
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		TLS:       tlsConfig,
	}
	if mechanism != nil {
		dialer.SASLMechanism = *mechanism
	}

	// 连接到 Kafka
	conn, err := dialer.DialContext(context.Background(), "tcp", cfg.Brokers[0])
	if err != nil {
		return fmt.Errorf("连接 Kafka 失败: %w", err)
	}
	defer conn.Close()

	// 获取 Controller
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("获取 Controller 失败: %w", err)
	}

	// 连接到 Controller
	controllerConn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(controller.Host, fmt.Sprintf("%d", controller.Port)))
	if err != nil {
		return fmt.Errorf("连接 Controller 失败: %w", err)
	}
	defer controllerConn.Close()

	// 创建 Topic
	topicConfig := kafka.TopicConfig{
		Topic:             cfg.Topic,
		NumPartitions:     3, // 3 个分区
		ReplicationFactor: 1, // 副本因子
	}

	err = controllerConn.CreateTopics(topicConfig)
	if err != nil {
		// 如果 Topic 已存在，忽略错误
		if strings.Contains(err.Error(), "Topic Already Exists") ||
			strings.Contains(err.Error(), "already exists") {
			logger.Info("Topic 已存在", zap.String("topic", cfg.Topic))
			return nil
		}
		return fmt.Errorf("创建 Topic 失败: %w", err)
	}

	logger.Info("Topic 创建成功", zap.String("topic", cfg.Topic), zap.Int("partitions", 3))
	return nil
}

// Close 关闭生产者
func (p *Producer) Close() error {
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}

// SendARPRecords 发送 ARP 记录到 Kafka
func (p *Producer) SendARPRecords(ctx context.Context, result model.CollectResult) error {
	if p == nil || p.writer == nil {
		return nil
	}

	messages := make([]kafka.Message, 0, len(result.Entries))

	for _, entry := range result.Entries {
		record := ARPRecord{
			DeviceID:     result.DeviceID,
			DeviceName:   result.DeviceName,
			MgmtIP:       result.MgmtIP,
			Role:         result.Role,
			Manufacturer: result.Manufacturer,
			Site:         result.Site,
			ContactName:  result.ContactName,
			ContactEmail: result.ContactEmail,
			RegionLevel1: result.RegionLevel1,
			RegionLevel2: result.RegionLevel2,
			RegionLevel3: result.RegionLevel3,
			ProxyName:    result.ProxyName,
			IP:           entry.IP,
			MAC:          entry.MAC,
			Interface:    entry.Interface,
			Type:         entry.Type,
			Timestamp:    result.CollectedAt,
		}

		data, err := json.Marshal(record)
		if err != nil {
			p.logger.Warn("序列化 ARP 记录失败", zap.Error(err))
			continue
		}

		messages = append(messages, kafka.Message{
			Key:   []byte(fmt.Sprintf("%s:%s", result.DeviceName, entry.IP)),
			Value: data,
		})
	}

	if len(messages) == 0 {
		return nil
	}

	if err := p.writer.WriteMessages(ctx, messages...); err != nil {
		return fmt.Errorf("发送 Kafka 消息失败: %w", err)
	}

	p.logger.Debug("发送 ARP 记录到 Kafka",
		zap.String("device", result.DeviceName),
		zap.Int("count", len(messages)))

	return nil
}
