package netbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"go.uber.org/zap"
)

// Client NetBox API 客户端
type Client struct {
	cfg        *config.Config
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *zap.Logger

	// 缓存
	mu         sync.RWMutex
	devices    []model.Device
	lastUpdate time.Time
}

// NewClient 创建 NetBox 客户端（需要完整 Config 以支持 proxy 映射规则）
func NewClient(cfg *config.Config, logger *zap.Logger) *Client {
	return &Client{
		cfg:     cfg,
		baseURL: strings.TrimRight(cfg.NetBox.URL, "/"),
		token:   cfg.NetBox.Token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		logger: logger,
	}
}

// === NetBox API 响应结构（与 Python 脚本对齐）===

type apiResponse struct {
	Count   int             `json:"count"`
	Results json.RawMessage `json:"results"`
	Next    string          `json:"next"`
}

type regionResult struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Parent *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"parent"`
}

type siteResult struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Region *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"region"`
}

type contactResult struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type contactAssignment struct {
	ObjectID int `json:"object_id"` // site_id
	Contact  *struct {
		ID int `json:"id"`
	} `json:"contact"`
}

type deviceResult struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status struct {
		Value string `json:"value"`
	} `json:"status"`
	Role *struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"role"`
	DeviceRole *struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"device_role"`
	DeviceType struct {
		Manufacturer struct {
			Name string `json:"name"`
		} `json:"manufacturer"`
	} `json:"device_type"`
	Site *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"site"`
	PrimaryIP *struct {
		Address string `json:"address"`
	} `json:"primary_ip"`
}

// FetchDevices 从 NetBox 获取设备列表（完整版，对齐 Python 脚本）
func (c *Client) FetchDevices(ctx context.Context, roles []string) ([]model.Device, error) {
	c.logger.Info("🚀 开始从 NetBox 获取设备列表", zap.Strings("roles", roles))
	start := time.Now()

	// 1. 并发预加载所有关键数据
	c.logger.Info("⚡ 并发预加载所有关键数据...")

	var (
		regionMap   map[int]regionResult
		siteMap     map[int]siteResult
		contactMap  map[int]contactResult
		siteContact map[int]int // site_id -> contact_id
		wg          sync.WaitGroup
		errChan     = make(chan error, 4)
	)

	wg.Add(4)

	// 获取 regions
	go func() {
		defer wg.Done()
		var err error
		regionMap, err = c.fetchAllRegions(ctx)
		if err != nil {
			errChan <- fmt.Errorf("获取 regions 失败: %w", err)
		}
	}()

	// 获取 sites
	go func() {
		defer wg.Done()
		var err error
		siteMap, err = c.fetchAllSites(ctx)
		if err != nil {
			errChan <- fmt.Errorf("获取 sites 失败: %w", err)
		}
	}()

	// 获取 contacts
	go func() {
		defer wg.Done()
		var err error
		contactMap, err = c.fetchAllContacts(ctx)
		if err != nil {
			errChan <- fmt.Errorf("获取 contacts 失败: %w", err)
		}
	}()

	// 获取 contact-assignments
	go func() {
		defer wg.Done()
		var err error
		siteContact, err = c.fetchContactAssignments(ctx)
		if err != nil {
			errChan <- fmt.Errorf("获取 contact-assignments 失败: %w", err)
		}
	}()

	wg.Wait()
	close(errChan)

	// 检查错误（非致命，继续处理）
	for err := range errChan {
		c.logger.Warn("预加载数据时出错", zap.Error(err))
	}

	if regionMap == nil {
		regionMap = make(map[int]regionResult)
	}
	if siteMap == nil {
		siteMap = make(map[int]siteResult)
	}
	if contactMap == nil {
		contactMap = make(map[int]contactResult)
	}
	if siteContact == nil {
		siteContact = make(map[int]int)
	}

	// 2. 构建 region 路径缓存
	regionPathCache := c.buildRegionPathCache(regionMap)

	// 3. 获取设备
	c.logger.Info("🔥 获取设备数据...")
	var allDevices []model.Device
	for _, role := range roles {
		devices, err := c.fetchDevicesByRole(ctx, role, siteMap, regionPathCache, contactMap, siteContact)
		if err != nil {
			c.logger.Error("获取设备失败", zap.String("role", role), zap.Error(err))
			continue
		}
		allDevices = append(allDevices, devices...)
	}

	// 4. 分配 proxy
	for i := range allDevices {
		allDevices[i].ProxyName = c.cfg.GetProxyForDevice(
			allDevices[i].Site,
			allDevices[i].RegionLevel1,
			allDevices[i].RegionLevel2,
		)
	}

	// 5. 更新缓存
	c.mu.Lock()
	c.devices = allDevices
	c.lastUpdate = time.Now()
	c.mu.Unlock()

	c.logger.Info("✅ 设备列表获取完成",
		zap.Int("total", len(allDevices)),
		zap.Duration("duration", time.Since(start)))

	return allDevices, nil
}

// GetCachedDevices 获取缓存的设备列表
func (c *Client) GetCachedDevices() []model.Device {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.devices
}

// GetDevicesForProxy 获取指定 proxy 的设备列表
func (c *Client) GetDevicesForProxy(proxyName string) []model.Device {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []model.Device
	for _, d := range c.devices {
		if d.ProxyName == proxyName {
			result = append(result, d)
		}
	}
	return result
}

// LastUpdateTime 返回最后更新时间
func (c *Client) LastUpdateTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUpdate
}

// === 内部方法 ===

func (c *Client) fetchAllRegions(ctx context.Context) (map[int]regionResult, error) {
	body, err := c.doRequest(ctx, "/api/dcim/regions/?limit=0")
	if err != nil {
		return nil, err
	}

	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	var regions []regionResult
	if err := json.Unmarshal(resp.Results, &regions); err != nil {
		return nil, err
	}

	result := make(map[int]regionResult)
	for _, r := range regions {
		result[r.ID] = r
	}
	c.logger.Debug("加载 regions 完成", zap.Int("count", len(result)))
	return result, nil
}

func (c *Client) fetchAllSites(ctx context.Context) (map[int]siteResult, error) {
	body, err := c.doRequest(ctx, "/api/dcim/sites/?limit=0")
	if err != nil {
		return nil, err
	}

	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	var sites []siteResult
	if err := json.Unmarshal(resp.Results, &sites); err != nil {
		return nil, err
	}

	result := make(map[int]siteResult)
	for _, s := range sites {
		result[s.ID] = s
	}
	c.logger.Debug("加载 sites 完成", zap.Int("count", len(result)))
	return result, nil
}

func (c *Client) fetchAllContacts(ctx context.Context) (map[int]contactResult, error) {
	body, err := c.doRequest(ctx, "/api/tenancy/contacts/?limit=0")
	if err != nil {
		return nil, err
	}

	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	var contacts []contactResult
	if err := json.Unmarshal(resp.Results, &contacts); err != nil {
		return nil, err
	}

	result := make(map[int]contactResult)
	for _, ct := range contacts {
		result[ct.ID] = ct
	}
	c.logger.Debug("加载 contacts 完成", zap.Int("count", len(result)))
	return result, nil
}

func (c *Client) fetchContactAssignments(ctx context.Context) (map[int]int, error) {
	body, err := c.doRequest(ctx, "/api/tenancy/contact-assignments/?content_type=dcim.site&limit=0")
	if err != nil {
		return nil, err
	}

	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	var assignments []contactAssignment
	if err := json.Unmarshal(resp.Results, &assignments); err != nil {
		return nil, err
	}

	result := make(map[int]int) // site_id -> contact_id (只保留第一个)
	for _, a := range assignments {
		if a.Contact != nil && a.ObjectID != 0 {
			if _, exists := result[a.ObjectID]; !exists {
				result[a.ObjectID] = a.Contact.ID
			}
		}
	}
	c.logger.Debug("加载 contact-assignments 完成", zap.Int("count", len(result)))
	return result, nil
}

// buildRegionPathCache 预计算所有 region 的路径
func (c *Client) buildRegionPathCache(regionMap map[int]regionResult) map[int][]string {
	cache := make(map[int][]string)
	for regionID := range regionMap {
		cache[regionID] = c.getRegionPath(regionID, regionMap, cache)
	}
	return cache
}

// getRegionPath 递归获取 region 路径
func (c *Client) getRegionPath(regionID int, regionMap map[int]regionResult, cache map[int][]string) []string {
	if path, ok := cache[regionID]; ok {
		return path
	}

	region, ok := regionMap[regionID]
	if !ok {
		return nil
	}

	var path []string
	if region.Parent != nil {
		parentPath := c.getRegionPath(region.Parent.ID, regionMap, cache)
		path = append(path, parentPath...)
	}
	path = append(path, region.Name)

	cache[regionID] = path
	return path
}

// fetchDevicesByRole 按角色获取设备
func (c *Client) fetchDevicesByRole(
	ctx context.Context,
	role string,
	siteMap map[int]siteResult,
	regionPathCache map[int][]string,
	contactMap map[int]contactResult,
	siteContact map[int]int,
) ([]model.Device, error) {
	params := url.Values{}
	params.Set("role", role)
	params.Set("status", "active")
	params.Set("limit", "1000")

	var allDevices []deviceResult
	endpoint := "/api/dcim/devices/?" + params.Encode()

	for endpoint != "" {
		body, err := c.doRequest(ctx, endpoint)
		if err != nil {
			return nil, err
		}

		var resp apiResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}

		var devices []deviceResult
		if err := json.Unmarshal(resp.Results, &devices); err != nil {
			return nil, err
		}

		allDevices = append(allDevices, devices...)

		// 处理分页
		if resp.Next != "" {
			// 提取相对路径
			if idx := strings.Index(resp.Next, "/api/"); idx >= 0 {
				endpoint = resp.Next[idx:]
			} else {
				endpoint = ""
			}
		} else {
			endpoint = ""
		}
	}

	// 转换为 model.Device
	var devices []model.Device
	for _, d := range allDevices {
		// 跳过无 site 的设备
		if d.Site == nil {
			continue
		}

		// 跳过无 IP 的设备
		if d.PrimaryIP == nil || d.PrimaryIP.Address == "" {
			c.logger.Debug("设备缺少 primary_ip，已跳过", zap.String("name", d.Name))
			continue
		}

		// 提取 IP（去掉 CIDR 后缀）
		ip := d.PrimaryIP.Address
		if idx := strings.Index(ip, "/"); idx > 0 {
			ip = ip[:idx]
		}

		// 获取完整 site 信息
		fullSite, ok := siteMap[d.Site.ID]
		if !ok {
			fullSite = siteResult{
				ID:   d.Site.ID,
				Name: d.Site.Name,
				Slug: d.Site.Slug,
			}
		}

		// 构建 site 格式: slug-name
		siteSlugName := fmt.Sprintf("%s-%s", fullSite.Slug, fullSite.Name)

		// 获取 manufacturer
		manufacturer := d.DeviceType.Manufacturer.Name
		if manufacturer == "" {
			manufacturer = "N/A"
		}

		// 获取 role（兼容 role 和 device_role 字段）
		roleName := ""
		if d.Role != nil {
			roleName = d.Role.Name
		} else if d.DeviceRole != nil {
			roleName = d.DeviceRole.Name
		}

		// 获取 contact 信息
		contactName := "N/A"
		contactEmail := "N/A"
		if contactID, ok := siteContact[fullSite.ID]; ok {
			if contact, ok := contactMap[contactID]; ok {
				contactName = contact.Name
				contactEmail = contact.Email
			}
		}

		// 获取 region 路径
		regionLevel1 := ""
		regionLevel2 := ""
		regionLevel3 := ""
		if fullSite.Region != nil {
			if path, ok := regionPathCache[fullSite.Region.ID]; ok && len(path) > 0 {
				regionLevel1 = path[0]
				if len(path) > 1 {
					regionLevel2 = path[1]
				}
				if len(path) > 2 {
					regionLevel3 = path[2]
				}
			}
		}

		devices = append(devices, model.Device{
			ID:           d.ID,
			Name:         d.Name,
			PrimaryIP:    ip,
			Role:         roleName,
			Manufacturer: manufacturer,
			Site:         siteSlugName,
			ContactName:  contactName,
			ContactEmail: contactEmail,
			RegionLevel1: regionLevel1,
			RegionLevel2: regionLevel2,
			RegionLevel3: regionLevel3,
		})
	}

	c.logger.Debug("角色设备获取完成",
		zap.String("role", role),
		zap.Int("count", len(devices)))

	return devices, nil
}

// doRequest 执行 HTTP 请求
func (c *Client) doRequest(ctx context.Context, endpoint string) ([]byte, error) {
	reqURL := c.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 返回错误: %d - %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
