package dockerpool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
)

// maskProxyURL 对代理地址脱敏，隐藏用户名密码和部分 IP
func maskProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "***"
	}
	host := u.Hostname()
	port := u.Port()
	parts := strings.Split(host, ".")
	if len(parts) == 4 {
		host = parts[0] + "." + parts[1] + ".*.*"
	}
	masked := u.Scheme + "://"
	if u.User != nil {
		masked += "***@"
	}
	masked += host
	if port != "" {
		masked += ":" + port
	}
	return masked
}

// ImageType 镜像类型枚举
type ImageType string

const (
	// ImageTypeHeadlessShell chromedp/headless-shell（默认，最轻量）
	ImageTypeHeadlessShell ImageType = "headless-shell"
	// ImageTypeBrowserless browserless/chrome（功能丰富）
	ImageTypeBrowserless ImageType = "browserless"
	// ImageTypeChromeVNC chrome-vnc（支持 VNC 可视化）
	ImageTypeChromeVNC ImageType = "chrome-vnc"
	// ImageTypeSelenium selenium/standalone-chrome（Selenium 兼容）
	ImageTypeSelenium ImageType = "selenium"
	// ImageTypeKasmWeb kasmweb/chrome（桌面级体验）
	ImageTypeKasmWeb ImageType = "kasmweb"
)

// ImageConfig 镜像配置
type ImageConfig struct {
	Type           ImageType // 镜像类型
	DefaultImage   string    // 默认镜像名称
	CDPPort        string    // 容器内 CDP 端口
	VNCPort        string    // 容器内 VNC 端口（0 表示不支持）
	SupportsVNC    bool      // 是否支持 VNC
	SupportsProxy  bool      // 是否支持代理配置
	ProxyViaEnv    bool      // 代理是否通过环境变量配置
	ProxyViaCmdArg bool      // 代理是否通过命令行参数配置
}

// ImageConfigs 所有支持的镜像配置
var ImageConfigs = map[ImageType]ImageConfig{
	ImageTypeHeadlessShell: {
		Type:           ImageTypeHeadlessShell,
		DefaultImage:   "chromedp/headless-shell:latest",
		CDPPort:        "9222",
		VNCPort:        "",
		SupportsVNC:    false,
		SupportsProxy:  true,
		ProxyViaEnv:    false,
		ProxyViaCmdArg: true,
	},
	ImageTypeBrowserless: {
		Type:           ImageTypeBrowserless,
		DefaultImage:   "browserless/chrome:latest",
		CDPPort:        "3000",
		VNCPort:        "",
		SupportsVNC:    false,
		SupportsProxy:  true,
		ProxyViaEnv:    true,
		ProxyViaCmdArg: false,
	},
	ImageTypeChromeVNC: {
		Type:           ImageTypeChromeVNC,
		DefaultImage:   "ghcr.io/doudou-start/chrome-vnc:ec6da9e",
		CDPPort:        "9222",
		VNCPort:        "6080",
		SupportsVNC:    true,
		SupportsProxy:  true,
		ProxyViaEnv:    true,
		ProxyViaCmdArg: false,
	},
	ImageTypeSelenium: {
		Type:           ImageTypeSelenium,
		DefaultImage:   "selenium/standalone-chrome:latest",
		CDPPort:        "4444",
		VNCPort:        "7900",
		SupportsVNC:    true,
		SupportsProxy:  true,
		ProxyViaEnv:    true,
		ProxyViaCmdArg: false,
	},
	ImageTypeKasmWeb: {
		Type:           ImageTypeKasmWeb,
		DefaultImage:   "kasmweb/chrome:latest",
		CDPPort:        "9222",
		VNCPort:        "6901",
		SupportsVNC:    true,
		SupportsProxy:  true,
		ProxyViaEnv:    true,
		ProxyViaCmdArg: false,
	},
}

// DetectImageType 根据镜像名称检测镜像类型
func DetectImageType(imageName string) ImageType {
	imageName = strings.ToLower(imageName)

	switch {
	case strings.Contains(imageName, "headless-shell"):
		return ImageTypeHeadlessShell
	case strings.Contains(imageName, "browserless"):
		return ImageTypeBrowserless
	case strings.Contains(imageName, "chrome-vnc"):
		return ImageTypeChromeVNC
	case strings.Contains(imageName, "selenium"):
		return ImageTypeSelenium
	case strings.Contains(imageName, "kasmweb"):
		return ImageTypeKasmWeb
	default:
		// 默认使用 headless-shell 配置
		return ImageTypeHeadlessShell
	}
}

// GetImageConfig 获取镜像配置
func GetImageConfig(imageName string) ImageConfig {
	imgType := DetectImageType(imageName)
	if cfg, ok := ImageConfigs[imgType]; ok {
		return cfg
	}
	return ImageConfigs[ImageTypeHeadlessShell]
}

type BrowserContainer struct {
	HostName      string
	ContainerID   string
	ContainerName string // 容器名称

	CDPURL         string // e.g. http://127.0.0.1:32768
	WSURL          string // e.g. ws://127.0.0.1:32768/devtools/browser/...
	VNCURL         string // e.g. http://127.0.0.1:6080 (仅 VNC 镜像)
	VNCPort        string // VNC 外部端口（仅 VNC 镜像）
	VNCHost        string // VNC 主机地址（容器 IP 或远程主机 IP）
	BrowserVersion string // 浏览器版本，如 "141.0.7390.55"（从 /json/version 获取）
}

type StartBrowserOptions struct {
	Image       string   // 镜像名称
	ProxyURL    string   // 代理 URL（支持认证）
	EnableVNC   bool     // 是否启用 VNC（仅支持 VNC 的镜像有效）
	PreferHosts []string // 优先使用的 Docker 主机名列表（为空则自动分配）

	CDPTimeout time.Duration // CDP 就绪超时（默认 60 秒）
}

const (
	DefaultCDPTimeout = 60 * time.Second
	CDPPollInterval   = 500 * time.Millisecond

	// 远程 Docker 固定端口范围基础值
	// CDP 端口: 9500 + slot.Index（如 max_containers=3 → 9500, 9501, 9502）
	// VNC 端口: 9600 + slot.Index（如 max_containers=3 → 9600, 9601, 9602）
	RemoteCDPBasePort = 9500
	RemoteVNCBasePort = 9600
)

// StartBrowserContainer creates and starts a browser container on the given slot.
//
// It supports multiple image types:
// - chromedp/headless-shell (port 9222) - 默认，最轻量
// - browserless/chrome (port 3000) - 功能丰富
// - chrome-vnc (port 9222 + VNC 6080) - 支持 VNC 可视化
// - selenium/standalone-chrome (port 4444 + VNC 7900)
// - kasmweb/chrome (port 9222 + VNC 6901)
func (p *Pool) StartBrowserContainer(ctx context.Context, slot *Slot, opts StartBrowserOptions) (*BrowserContainer, error) {
	if slot == nil {
		return nil, fmt.Errorf("slot is nil")
	}

	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = "chromedp/headless-shell:latest"
	}

	// 获取镜像配置
	imgConfig := GetImageConfig(image)

	p.mu.Lock()
	hostName := slot.HostName
	cli := p.clients[hostName]
	hostCfg, ok := p.hosts[hostName]
	p.mu.Unlock()

	if cli == nil || !ok {
		return nil, fmt.Errorf("docker host not available: %s", hostName)
	}

	// 确保镜像存在，如果不存在则自动拉取
	if err := ensureImageExists(ctx, cli, image); err != nil {
		return nil, fmt.Errorf("prepare image: %w", err)
	}

	// 确定容器端口
	containerCDPPort := imgConfig.CDPPort + "/tcp"
	containerVNCPort := ""
	if opts.EnableVNC && imgConfig.SupportsVNC && imgConfig.VNCPort != "" {
		containerVNCPort = imgConfig.VNCPort + "/tcp"
	}

	// 解析代理
	proxyAddr, _, _ := ParseProxyURL(opts.ProxyURL)

	// 构建环境变量
	env := []string{
		// 清除 Docker Desktop 注入的代理，避免 ERR_NO_SUPPORTED_PROXIES
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"http_proxy=",
		"https_proxy=",
	}

	// 根据镜像类型添加特定环境变量
	var cmd []string
	switch imgConfig.Type {
	case ImageTypeHeadlessShell:
		cmd = GetAntiDetectArgsForHeadlessShell(proxyAddr)
		log.Printf("[DockerPool] headless-shell 启动参数共 %d 项: %v ...", len(cmd), cmd[:min(3, len(cmd))])

	case ImageTypeBrowserless:
		env = append(env, GetBrowserlessEnvVars()...)
		if strings.TrimSpace(opts.ProxyURL) != "" {
			env = append(env,
				"HTTP_PROXY="+opts.ProxyURL,
				"HTTPS_PROXY="+opts.ProxyURL,
				"http_proxy="+opts.ProxyURL,
				"https_proxy="+opts.ProxyURL,
			)
		}
		log.Printf("[DockerPool] browserless 环境变量已配置")

	case ImageTypeChromeVNC:
		env = append(env,
			"SCREEN_WIDTH=1920",
			"SCREEN_HEIGHT=1080",
		)
		if proxyAddr != "" {
			env = append(env, "PROXY_SERVER="+proxyAddr)
			log.Printf("[DockerPool] chrome-vnc 使用代理: %s", maskProxyURL(proxyAddr))
		}

	case ImageTypeSelenium:
		env = append(env,
			"SE_NODE_SESSION_TIMEOUT=600",
			"SE_VNC_NO_PASSWORD=1",
		)
		log.Printf("[DockerPool] selenium 环境变量已配置")

	case ImageTypeKasmWeb:
		env = append(env,
			"VNC_PW=chatgpt-register",
			"CHROME_ARGS=--no-sandbox --disable-dev-shm-usage --disable-gpu --remote-debugging-port=9222",
		)
		log.Printf("[DockerPool] kasmweb 环境变量已配置")
	}

	// 暴露端口
	exposed := map[docker.Port]struct{}{
		docker.Port(containerCDPPort): {},
	}
	if containerVNCPort != "" {
		exposed[docker.Port(containerVNCPort)] = struct{}{}
	}

	containerCfg := &docker.Config{
		Image:        image,
		Cmd:          cmd,
		Env:          env,
		ExposedPorts: exposed,
	}

	// 检查是否有自动检测到的 Docker 网络（本地 Docker 场景）
	network := p.GetNetwork(hostName)
	useNetwork := network != ""

	hostCfgDocker := &docker.HostConfig{
		AutoRemove: false,
		ShmSize:    2 * 1024 * 1024 * 1024, // 2GB 共享内存
	}

	// 网络配置：将浏览器容器加入与主应用相同的 Docker 网络
	var networkingConfig *docker.NetworkingConfig
	if useNetwork {
		networkingConfig = &docker.NetworkingConfig{
			EndpointsConfig: map[string]*docker.EndpointConfig{
				network: {},
			},
		}
	} else {
		// 远程 Docker：使用固定端口绑定（基于 slot 索引）
		// CDP 端口范围: 9500+index，VNC 端口范围: 9600+index
		cdpHostPort := fmt.Sprintf("%d", RemoteCDPBasePort+slot.Index)
		portBindings := map[docker.Port][]docker.PortBinding{
			docker.Port(containerCDPPort): {{HostIP: "0.0.0.0", HostPort: cdpHostPort}},
		}
		if containerVNCPort != "" {
			vncHostPort := fmt.Sprintf("%d", RemoteVNCBasePort+slot.Index)
			portBindings[docker.Port(containerVNCPort)] = []docker.PortBinding{{HostIP: "0.0.0.0", HostPort: vncHostPort}}
		}
		hostCfgDocker.PortBindings = portBindings
		log.Printf("[DockerPool] 远程端口绑定: slot=%d, CDP=%s, host=%s", slot.Index, cdpHostPort, hostName)
	}

	// 容器名称：{prefix}{taskType}-{taskID前8位}，例如 browser-reg-b11bc8b3
	shortTaskID := slot.TaskID
	if len(shortTaskID) > 8 {
		shortTaskID = shortTaskID[:8]
	}
	typeShort := map[string]string{
		"register":     "reg",
		"refresh":      "ref",
		"oauth":        "oauth",
		"subscription": "sub",
	}
	taskTypePrefix := typeShort[slot.TaskType]
	if taskTypePrefix == "" {
		taskTypePrefix = "task"
	}
	containerName := fmt.Sprintf("%s%s-%s", ContainerNamePrefix, taskTypePrefix, shortTaskID)

	created, err := cli.CreateContainer(docker.CreateContainerOptions{
		Name:             containerName,
		Config:           containerCfg,
		HostConfig:       hostCfgDocker,
		NetworkingConfig: networkingConfig,
		Context:          ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	containerID := created.ID
	if err := cli.StartContainer(containerID, nil); err != nil {
		_ = cli.RemoveContainer(docker.RemoveContainerOptions{ID: containerID, Force: true})
		return nil, fmt.Errorf("start container: %w", err)
	}

	p.mu.Lock()
	slot.ContainerID = containerID
	p.mu.Unlock()

	log.Printf("[DockerPool] 容器已启动: %s (%s)", containerName, containerID[:12])

	// 确定 CDP 连接地址
	var cdpURL string
	if useNetwork {
		// 同网络：通过容器 IP + 内部端口直连
		inspect, err := cli.InspectContainerWithContext(containerID, ctx)
		if err != nil {
			p.cleanupFailedContainer(cli, slot, containerID)
			return nil, fmt.Errorf("inspect container: %w", err)
		}
		networkInfo, ok := inspect.NetworkSettings.Networks[network]
		if !ok || networkInfo.IPAddress == "" {
			p.cleanupFailedContainer(cli, slot, containerID)
			return nil, fmt.Errorf("容器未获取到网络 %s 的 IP", network)
		}
		internalPort := strings.TrimSuffix(containerCDPPort, "/tcp")
		cdpURL = fmt.Sprintf("http://%s:%s", networkInfo.IPAddress, internalPort)
	} else {
		// 远程 Docker：使用固定端口（基于 slot 索引）
		cdpHost := extractDockerHost(hostCfg.URL)
		cdpHostPort := RemoteCDPBasePort + slot.Index
		cdpURL = fmt.Sprintf("http://%s:%d", cdpHost, cdpHostPort)
	}
	log.Printf("[DockerPool] CDP 地址: %s", cdpURL)

	// 等待 CDP 就绪
	cdpTimeout := opts.CDPTimeout
	if cdpTimeout <= 0 {
		cdpTimeout = DefaultCDPTimeout
	}

	cdpInfo, err := waitForCDP(ctx, cdpURL, cdpTimeout)
	if err != nil {
		log.Printf("[DockerPool] CDP 就绪失败，清理容器 %s: %v", containerID[:12], err)
		p.cleanupFailedContainer(cli, slot, containerID)
		return nil, fmt.Errorf("CDP 就绪超时: %w", err)
	}

	bc := &BrowserContainer{
		HostName:       hostName,
		ContainerID:    containerID,
		ContainerName:  containerName,
		CDPURL:         cdpURL,
		WSURL:          cdpInfo.WSURL,
		BrowserVersion: cdpInfo.BrowserVersion,
	}

	// 如果启用了 VNC，获取 VNC 地址
	if containerVNCPort != "" {
		vncInternalPort := strings.TrimSuffix(containerVNCPort, "/tcp")
		if useNetwork {
			// 同网络模式：通过容器 IP + 内部 VNC 端口
			inspect, err := cli.InspectContainerWithContext(containerID, ctx)
			if err == nil {
				if networkInfo, ok := inspect.NetworkSettings.Networks[network]; ok && networkInfo.IPAddress != "" {
					bc.VNCURL = fmt.Sprintf("http://%s:%s", networkInfo.IPAddress, vncInternalPort)
					bc.VNCPort = vncInternalPort
					bc.VNCHost = networkInfo.IPAddress
				}
			}
		} else {
			// 远程 Docker：使用固定 VNC 端口（基于 slot 索引）
			cdpHost := extractDockerHost(hostCfg.URL)
			vncHostPort := RemoteVNCBasePort + slot.Index
			bc.VNCURL = fmt.Sprintf("http://%s:%d", cdpHost, vncHostPort)
			bc.VNCPort = fmt.Sprintf("%d", vncHostPort)
			bc.VNCHost = cdpHost
		}
		if bc.VNCURL != "" {
			log.Printf("[DockerPool] VNC 地址: %s", bc.VNCURL)
		}
	}

	return bc, nil
}

// cleanupFailedContainer 清理启动失败的容器
func (p *Pool) cleanupFailedContainer(cli *docker.Client, slot *Slot, containerID string) {
	_ = cli.StopContainer(containerID, 5)
	_ = cli.RemoveContainer(docker.RemoveContainerOptions{ID: containerID, Force: true})
	p.mu.Lock()
	slot.ContainerID = ""
	p.mu.Unlock()
}

// CDPInfo CDP 连接信息
type CDPInfo struct {
	WSURL          string // WebSocket URL
	BrowserVersion string // 浏览器版本，如 "141.0.7390.55"
}

// waitForCDP 轮询 CDP /json/version 端点，等待浏览器就绪
func waitForCDP(ctx context.Context, cdpURL string, timeout time.Duration) (*CDPInfo, error) {
	deadline := time.Now().Add(timeout)
	versionURL := strings.TrimRight(cdpURL, "/") + "/json/version"
	loggedFirstErr := false

	type versionResp struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		Browser              string `json:"Browser"`
		ProtocolVersion      string `json:"Protocol-Version"`
		UserAgent            string `json:"User-Agent"`
		V8Version            string `json:"V8-Version"`
		WebKitVersion        string `json:"WebKit-Version"`
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
		if err != nil {
			time.Sleep(CDPPollInterval)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if !loggedFirstErr {
				log.Printf("[DockerPool] 等待 CDP 就绪 (%s): %v", cdpURL, err)
				loggedFirstErr = true
			}
			time.Sleep(CDPPollInterval)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var v versionResp
			if err := json.NewDecoder(resp.Body).Decode(&v); err == nil {
				resp.Body.Close()

				if strings.TrimSpace(v.WebSocketDebuggerURL) != "" {
					// 替换 WebSocket URL 中的 host（Chrome 可能返回 127.0.0.1）
					wsURL := v.WebSocketDebuggerURL
					cdpParsed, err := url.Parse(cdpURL)
					if err == nil {
						wsParsed, err := url.Parse(wsURL)
						if err == nil {
							wsParsed.Host = cdpParsed.Host
							wsURL = wsParsed.String()
						}
					}

					// 从 v.Browser 提取版本号，如 "Chrome/141.0.7390.55" -> "141.0.7390.55"
					browserVersion := ""
					if strings.HasPrefix(v.Browser, "Chrome/") {
						browserVersion = strings.TrimPrefix(v.Browser, "Chrome/")
					} else if strings.HasPrefix(v.Browser, "HeadlessChrome/") {
						browserVersion = strings.TrimPrefix(v.Browser, "HeadlessChrome/")
					}

					log.Printf("[DockerPool] CDP 就绪: Browser=%s, Protocol=%s", v.Browser, v.ProtocolVersion)
					return &CDPInfo{WSURL: wsURL, BrowserVersion: browserVersion}, nil
				}
			} else {
				resp.Body.Close()
			}
		} else {
			resp.Body.Close()
		}

		time.Sleep(CDPPollInterval)
	}

	return nil, fmt.Errorf("CDP %s 在 %v 内未就绪", cdpURL, timeout)
}

// waitForHostPort 等待容器端口映射到宿主机（远程 Docker 场景）
func waitForHostPort(ctx context.Context, cli *docker.Client, containerID string, port docker.Port) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		inspect, err := cli.InspectContainerWithContext(containerID, ctx)
		if err == nil && inspect.NetworkSettings != nil {
			if bindings, ok := inspect.NetworkSettings.Ports[port]; ok && len(bindings) > 0 {
				if hp := strings.TrimSpace(bindings[0].HostPort); hp != "" {
					return hp, nil
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("端口映射超时: %s", port)
}

// extractDockerHost 从 Docker URL 提取主机地址（远程 Docker 场景）
func extractDockerHost(dockerURL string) string {
	u, err := url.Parse(strings.TrimSpace(dockerURL))
	if err != nil || u.Hostname() == "" {
		return "localhost"
	}
	return u.Hostname()
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "x"
	}
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// ensureImageExists 检查镜像是否存在，如果不存在则自动拉取
func ensureImageExists(ctx context.Context, cli *docker.Client, imageName string) error {
	// 检查镜像是否存在
	_, err := cli.InspectImage(imageName)
	if err == nil {
		// 镜像已存在
		return nil
	}

	// 镜像不存在，开始拉取
	log.Printf("[DockerPool] 镜像 %s 不存在，开始拉取...", imageName)

	pullOpts := docker.PullImageOptions{
		Repository: imageName,
		Context:    ctx,
	}

	// 如果镜像名称包含 tag，分离 repository 和 tag
	if strings.Contains(imageName, ":") {
		parts := strings.SplitN(imageName, ":", 2)
		pullOpts.Repository = parts[0]
		pullOpts.Tag = parts[1]
	}

	err = cli.PullImage(pullOpts, docker.AuthConfiguration{})
	if err != nil {
		return fmt.Errorf("拉取镜像失败: %w", err)
	}

	log.Printf("[DockerPool] 镜像 %s 拉取成功", imageName)
	return nil
}
