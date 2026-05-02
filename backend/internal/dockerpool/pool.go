// Package dockerpool 管理 Docker 容器池
// 提供浏览器容器的启动、管理和资源回收功能
package dockerpool

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	docker "github.com/fsouza/go-dockerclient"

	"chatgpt-register-script/internal/config"
)

// ContainerNamePrefix 容器名称前缀（用于识别和清理）
const ContainerNamePrefix = "browser-"

type Slot struct {
	HostName    string
	Index       int // 在主机上的槽位索引（从 0 开始），用于固定端口分配
	ContainerID string
	TaskID      string
	TaskType    string // 任务类型：register/refresh/oauth
	IsBusy      bool
}

type Pool struct {
	mu sync.Mutex

	hosts      map[string]config.DockerHost
	clients    map[string]*docker.Client
	slots      map[string][]*Slot
	networks   map[string]string // 每个主机自动检测到的 Docker 网络名
	hostErrors map[string]string // 每个主机的连接错误信息
}

func New() *Pool {
	return &Pool{
		hosts:      map[string]config.DockerHost{},
		clients:    map[string]*docker.Client{},
		slots:      map[string][]*Slot{},
		networks:   map[string]string{},
		hostErrors: map[string]string{},
	}
}

// GetNetwork 获取指定主机的 Docker 网络名（自动检测结果）
func (p *Pool) GetNetwork(hostName string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.networks[hostName]
}

func (p *Pool) Initialize(ctx context.Context, cfg config.YamlConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 保存旧的 slots，用于保留正在运行的任务
	oldSlots := p.slots

	p.hosts = map[string]config.DockerHost{}
	p.clients = map[string]*docker.Client{}
	p.slots = map[string][]*Slot{}
	p.networks = map[string]string{}
	p.hostErrors = map[string]string{}

	for _, h := range cfg.DockerHosts {
		if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.URL) == "" || h.MaxContainers <= 0 {
			continue
		}

		p.hosts[h.Name] = h

		// 跳过禁用的主机（仍保留 hosts 以便 UI 显示）
		if !h.IsEnabled() {
			// 保留正在运行的 slot（禁用不影响已运行的任务）
			if old, ok := oldSlots[h.Name]; ok {
				var busy []*Slot
				for _, s := range old {
					if s.IsBusy {
						busy = append(busy, s)
					}
				}
				if len(busy) > 0 {
					p.slots[h.Name] = busy
					log.Printf("[DockerPool] 主机已禁用，保留 %d 个运行中的任务: %s", len(busy), h.Name)
				}
			}
			p.hostErrors[h.Name] = "已禁用"
			log.Printf("[DockerPool] 主机已禁用，跳过: %s", h.Name)
			continue
		}

		// 重建 slots：保留正在运行的 slot，调整总数到新的 MaxContainers
		var newSlots []*Slot
		if old, ok := oldSlots[h.Name]; ok {
			// 保留所有正在运行的 slot
			for _, s := range old {
				if s.IsBusy {
					newSlots = append(newSlots, s)
				}
			}
		}
		// 补充空闲 slot 到 MaxContainers（使用未被占用的索引）
		usedIndices := map[int]bool{}
		for _, s := range newSlots {
			usedIndices[s.Index] = true
		}
		for i := 0; len(newSlots) < h.MaxContainers; i++ {
			if !usedIndices[i] {
				newSlots = append(newSlots, &Slot{HostName: h.Name, Index: i})
				usedIndices[i] = true
			}
		}
		p.slots[h.Name] = newSlots

		cli, err := newDockerClient(h)
		if err != nil {
			errMsg := fmt.Sprintf("创建客户端失败: %v", err)
			log.Printf("[DockerPool] %s [%s]", errMsg, h.Name)
			p.hostErrors[h.Name] = errMsg
			continue
		}

		// best-effort ping
		if err := cli.Ping(); err != nil {
			errMsg := fmt.Sprintf("连接失败: %v", err)
			log.Printf("[DockerPool] Docker 主机 %s [%s]", errMsg, h.Name)
			p.hostErrors[h.Name] = errMsg
			continue
		}

		p.clients[h.Name] = cli

		// 本地 Docker（unix socket）：自动检测主应用所在的 Docker 网络
		if strings.HasPrefix(h.URL, "unix:") {
			if network := detectSelfNetwork(ctx, cli); network != "" {
				p.networks[h.Name] = network
				log.Printf("[DockerPool] 自动检测到网络: %s [%s]", network, h.Name)
			}
		}

		// 清理该主机上的残留容器（仅清理空闲的）
		p.cleanupStaleContainersOnHost(ctx, cli, h.Name)
	}

	log.Printf("[DockerPool] 初始化完成，共 %d 个主机在线", len(p.clients))
}

// cleanupStaleContainersOnHost 清理指定主机上的残留容器
// 清理名称以 ContainerNamePrefix 开头的容器（上次异常退出可能留下的）
func (p *Pool) cleanupStaleContainersOnHost(ctx context.Context, cli *docker.Client, hostName string) {
	containers, err := cli.ListContainers(docker.ListContainersOptions{
		All:     true,
		Context: ctx,
	})
	if err != nil {
		log.Printf("[DockerPool] 列出容器失败 [%s]: %v", hostName, err)
		return
	}

	cleaned := 0
	for _, c := range containers {
		for _, name := range c.Names {
			// 容器名称以 / 开头
			containerName := strings.TrimPrefix(name, "/")
			if strings.HasPrefix(containerName, ContainerNamePrefix) {
				log.Printf("[DockerPool] 清理残留容器 [%s]: %s (%s)", hostName, containerName, c.ID[:12])

				// 强制停止
				_ = cli.StopContainer(c.ID, 5)

				// 强制删除
				_ = cli.RemoveContainer(docker.RemoveContainerOptions{
					ID:            c.ID,
					Force:         true,
					RemoveVolumes: true,
					Context:       ctx,
				})
				cleaned++
				break
			}
		}
	}

	if cleaned > 0 {
		log.Printf("[DockerPool] 清理完成 [%s]，共清理 %d 个残留容器", hostName, cleaned)
	}
}

// AcquireSlot 获取一个可用的容器时隙
// preferHosts 可选：指定优先使用的主机名列表，为空则从所有主机中选择
func (p *Pool) AcquireSlot(taskID, taskType string, preferHosts ...string) *Slot {
	p.mu.Lock()
	defer p.mu.Unlock()

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}

	// 构建候选主机列表
	hostFilter := map[string]bool{}
	for _, h := range preferHosts {
		if h = strings.TrimSpace(h); h != "" {
			hostFilter[h] = true
		}
	}

	for hostName, slots := range p.slots {
		if _, ok := p.clients[hostName]; !ok {
			continue
		}
		// 如果指定了主机列表，只在这些主机上分配
		if len(hostFilter) > 0 && !hostFilter[hostName] {
			continue
		}
		for _, s := range slots {
			if !s.IsBusy {
				s.IsBusy = true
				s.TaskID = taskID
				s.TaskType = taskType
				return s
			}
		}
	}
	return nil
}

func (p *Pool) ReleaseSlot(ctx context.Context, slot *Slot) {
	if slot == nil {
		return
	}

	p.mu.Lock()
	hostName := slot.HostName
	containerID := slot.ContainerID
	cli := p.clients[hostName]

	// mark free first to avoid deadlocks (cleanup is best-effort)
	slot.ContainerID = ""
	slot.TaskID = ""
	slot.TaskType = ""
	slot.IsBusy = false
	p.mu.Unlock()

	if containerID == "" || cli == nil {
		return
	}

	// Cleanup container best-effort.
	_ = cli.StopContainer(containerID, 5)
	_ = cli.RemoveContainer(docker.RemoveContainerOptions{
		ID:    containerID,
		Force: true,
	})
}

// detectSelfNetwork 检测本应用容器所在的 Docker 网络
// 通过 HOSTNAME 环境变量（Docker 默认设为容器 ID）inspect 自身容器
func detectSelfNetwork(ctx context.Context, cli *docker.Client) string {
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		return "" // 非容器环境
	}

	container, err := cli.InspectContainerWithContext(hostname, ctx)
	if err != nil {
		return ""
	}

	// 选择第一个非 bridge 的自定义网络
	for name := range container.NetworkSettings.Networks {
		if name != "bridge" {
			return name
		}
	}
	return ""
}

// TestConnection 测试指定主机的 Docker 连接，返回详细错误信息
func (p *Pool) TestConnection(hostName string) (online bool, dockerVersion string, errMsg string) {
	p.mu.Lock()
	h, ok := p.hosts[hostName]
	p.mu.Unlock()

	if !ok {
		return false, "", "主机不存在"
	}

	cli, err := newDockerClient(h)
	if err != nil {
		return false, "", fmt.Sprintf("创建客户端失败: %v", err)
	}

	if err := cli.Ping(); err != nil {
		return false, "", fmt.Sprintf("Ping 失败: %v", err)
	}

	env, err := cli.Version()
	if err != nil {
		return true, "", fmt.Sprintf("获取版本失败: %v", err)
	}

	ver := env.Get("Version")

	// 测试成功，更新连接状态
	p.mu.Lock()
	p.clients[hostName] = cli
	delete(p.hostErrors, hostName)
	p.mu.Unlock()

	return true, ver, ""
}

func newDockerClient(h config.DockerHost) (*docker.Client, error) {
	host := strings.TrimSpace(h.URL)
	if host == "" {
		return nil, fmt.Errorf("host url is empty")
	}

	if len(h.TLS) > 0 {
		caPath := strings.TrimSpace(h.TLS["ca"])
		certPath := strings.TrimSpace(h.TLS["cert"])
		keyPath := strings.TrimSpace(h.TLS["key"])
		if caPath != "" && certPath != "" && keyPath != "" {
			return docker.NewTLSClient(host, certPath, keyPath, caPath)
		}
	}
	return docker.NewClient(host)
}
