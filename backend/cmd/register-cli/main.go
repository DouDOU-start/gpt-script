package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"chatgpt-register-script/internal/accountstore"
	"chatgpt-register-script/internal/config"
	"chatgpt-register-script/internal/dockerpool"
	migratedruntime "chatgpt-register-script/internal/runtime"
)

func main() {
	var email string
	var password string
	var accountsPath string
	var outputPath string
	var configPath string
	var mode string
	var backend string
	var proxyURL string
	var screenshotDir string
	var codesPath string
	var workers int
	var headless bool
	var includeSecrets bool
	var interactive bool
	var preferHostsRaw string
	var workspaceID string
	var organizationID string
	var generateCount int
	var emailDomain string
	var passwordLength int
	var runsDir string
	var proxyPoolPath string
	var dbPath string
	var dbMigrate bool
	var importLegacyPath string
	var dbSource bool
	var dbStatus string
	var dbLimit int
	var dbWriteResults bool
	var saveGeneratedToDB bool
	var webMode bool
	var webAddr string

	flag.StringVar(&email, "email", "", "单账号邮箱")
	flag.StringVar(&password, "password", "", "单账号密码")
	flag.StringVar(&accountsPath, "accounts", "", "批量账号文件，支持 JSONL 或 email----password")
	flag.IntVar(&generateCount, "generate", 0, "随机生成账号数量，0 表示交互式询问")
	flag.StringVar(&emailDomain, "domain", "k9ray.com", "随机生成邮箱域名")
	flag.IntVar(&passwordLength, "password-length", 14, "随机生成密码长度")
	flag.StringVar(&outputPath, "output", "results/register_results.jsonl", "结果输出 JSONL 文件")
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&mode, "mode", "register", "运行模式：register、login 或 oauth")
	flag.StringVar(&backend, "backend", "", "浏览器后端：local 或 docker，默认读取 config.yaml")
	flag.StringVar(&proxyURL, "proxy", "", "代理地址，如 http://user:pass@host:port")
	flag.StringVar(&proxyPoolPath, "proxy-pool", "", "代理池文件，每行一个代理")
	flag.StringVar(&runsDir, "runs-dir", "", "运行时目录，保存 manifest、summary 和账号事件")
	flag.StringVar(&screenshotDir, "screenshots", "screenshots", "截图保存目录")
	flag.StringVar(&codesPath, "codes", "", "验证码文件，格式 email=code")
	flag.IntVar(&workers, "workers", 1, "批量并发数")
	flag.BoolVar(&headless, "headless", true, "是否无头运行浏览器")
	flag.BoolVar(&includeSecrets, "include-secrets", true, "结果中写入 cookies/access_token 等敏感字段")
	flag.BoolVar(&interactive, "interactive", true, "需要验证码时从控制台输入")
	flag.StringVar(&preferHostsRaw, "prefer-hosts", "", "Docker 主机名白名单，逗号分隔")
	flag.StringVar(&workspaceID, "workspace-id", "", "OAuth 工作空间 ID，留空时自动选择个人空间")
	flag.StringVar(&organizationID, "organization-id", "", "OAuth 组织 ID，留空时自动选择第一个组织")
	flag.StringVar(&dbPath, "db", filepath.Join("data", "accounts.db"), "SQLite 账号数据库路径")
	flag.BoolVar(&dbMigrate, "db-migrate", false, "创建或升级账号数据库后退出")
	flag.StringVar(&importLegacyPath, "import-legacy", "", "导入旧 SQLite 账号库后退出")
	flag.BoolVar(&dbSource, "db-source", false, "从账号数据库读取账号")
	flag.StringVar(&dbStatus, "db-status", "", "数据库账号状态筛选，逗号分隔")
	flag.IntVar(&dbLimit, "db-limit", 0, "数据库账号读取数量限制，0 表示不限制")
	flag.BoolVar(&dbWriteResults, "db-write-results", true, "运行结果回写账号数据库")
	flag.BoolVar(&saveGeneratedToDB, "save-generated-to-db", false, "随机生成账号后保存到数据库")
	flag.BoolVar(&webMode, "web", false, "启动本地 Web 管理页面")
	flag.StringVar(&webAddr, "web-addr", "127.0.0.1:8080", "Web 管理页面监听地址")
	flag.Parse()
	explicitFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	stdin := bufio.NewReader(os.Stdin)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = filepath.Join("data", "accounts.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	store, err := accountstore.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("打开账号数据库失败: %v", err)
	}
	defer store.Close()
	accountStore := accountstore.Store(store)
	if dbMigrate || importLegacyPath != "" {
		if dbMigrate {
			if err := accountStore.Migrate(ctx); err != nil {
				log.Fatalf("迁移账号数据库失败: %v", err)
			}
			log.Printf("账号数据库已迁移: %s", dbPath)
		}
		if importLegacyPath != "" {
			summary, err := accountStore.ImportLegacy(ctx, importLegacyPath)
			if err != nil {
				log.Fatalf("导入旧账号库失败: %v", err)
			}
			log.Printf("旧账号库导入完成: imported=%d updated=%d skipped=%d invalid=%d", summary.Imported, summary.Updated, summary.Skipped, summary.Invalid)
		}
		return
	}
	if webMode {
		yamlStore, err := config.LoadYamlStore(configPath)
		if err != nil {
			log.Fatalf("加载配置失败: %v", err)
		}
		yamlCfg := yamlStore.Get()
		if strings.TrimSpace(backend) != "" {
			yamlCfg.BrowserBackend = strings.TrimSpace(backend)
		}
		if strings.TrimSpace(runsDir) != "" {
			yamlCfg.RunsDir = strings.TrimSpace(runsDir)
		}
		if strings.TrimSpace(proxyURL) != "" {
			yamlCfg.Proxy.Enabled = true
			yamlCfg.Proxy.Mode = "single"
			yamlCfg.Proxy.Proxy = strings.TrimSpace(proxyURL)
		}
		if strings.TrimSpace(proxyPoolPath) != "" {
			yamlCfg.Proxy.Enabled = true
			yamlCfg.Proxy.Mode = "pool"
			yamlCfg.Proxy.PoolFile = strings.TrimSpace(proxyPoolPath)
		}
		if explicitFlags["headless"] {
			yamlCfg.BrowserHeadless = &headless
		} else {
			headless = yamlCfg.BrowserHeadlessValue()
		}
		if explicitFlags["include-secrets"] {
			yamlCfg.IncludeSecrets = &includeSecrets
		} else {
			includeSecrets = yamlCfg.IncludeSecretsValue()
		}
		if err := startWebServer(ctx, webServerOptions{
			Addr:           webAddr,
			ConfigPath:     configPath,
			YamlStore:      yamlStore,
			YAMLConfig:     yamlCfg,
			Store:          accountStore,
			OutputPath:     outputPath,
			ScreenshotDir:  screenshotDir,
			CodesPath:      codesPath,
			ProxyURL:       proxyURL,
			PreferHostsRaw: preferHostsRaw,
			IncludeSecrets: includeSecrets,
			DBWriteResults: dbWriteResults,
		}); err != nil {
			log.Fatalf("启动 Web 服务失败: %v", err)
		}
		return
	}

	var accounts []accountInput
	accountSource := ""
	explicitAccountSource := strings.TrimSpace(email) != "" || strings.TrimSpace(password) != "" || strings.TrimSpace(accountsPath) != "" || generateCount > 0
	if flag.NFlag() == 0 && flag.NArg() == 0 {
		accounts, accountSource, err = runInteractiveWizard(stdin, accountStore != nil, &mode, &accountsPath, &generateCount, &emailDomain, &passwordLength, &backend, &proxyURL, &outputPath, &screenshotDir, &workers, &headless, &includeSecrets, &dbSource, &dbStatus, &dbLimit)
		if err != nil {
			log.Fatal(err)
		}
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "register" && mode != "login" && mode != "oauth" {
		log.Fatalf("mode 只能是 register、login 或 oauth")
	}
	if workers <= 0 {
		workers = 1
	}
	if passwordLength < 8 {
		log.Fatalf("password-length 不能小于 8")
	}

	if len(accounts) == 0 {
		accounts, err = loadAccounts(email, password, accountsPath)
		if err != nil {
			log.Fatal(err)
		}
		if len(accounts) > 0 {
			if strings.TrimSpace(accountsPath) != "" {
				accountSource = "file"
			} else {
				accountSource = "single"
			}
		}
	}
	if len(accounts) == 0 && (dbSource || !explicitAccountSource) {
		if err := accountStore.Migrate(ctx); err != nil {
			log.Fatalf("迁移账号数据库失败: %v", err)
		}
		statuses := dbStatusesForMode(mode, dbStatus)
		accounts, err = loadAccountsFromDB(ctx, accountStore, statuses, dbLimit)
		if err != nil {
			log.Fatalf("读取数据库账号失败: %v", err)
		}
		accountSource = fmt.Sprintf("db status=%s", strings.Join(statuses, ","))
	}
	if len(accounts) == 0 && mode == "register" {
		accounts, err = createGeneratedAccounts(generateCount, emailDomain, passwordLength, interactive, stdin)
		if err != nil {
			log.Fatal(err)
		}
		if len(accounts) > 0 {
			accountSource = "generated"
			if accountStore != nil && saveGeneratedToDB {
				if err := accountStore.Migrate(ctx); err != nil {
					log.Fatalf("迁移账号数据库失败: %v", err)
				}
				if err := saveGeneratedAccountsToDB(ctx, accountStore, accounts); err != nil {
					log.Fatalf("保存生成账号到数据库失败: %v", err)
				}
			}
		}
	}
	if len(accounts) == 0 {
		log.Fatalf("请通过交互式菜单、-email/-password、-accounts、-db-source 或 -generate 提供账号")
	}
	for i := range accounts {
		accounts[i].Index = i
	}

	yamlStore, err := config.LoadYamlStore(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	yamlCfg := yamlStore.Get()
	if strings.TrimSpace(backend) != "" {
		yamlCfg.BrowserBackend = strings.TrimSpace(backend)
	}
	if strings.TrimSpace(runsDir) != "" {
		yamlCfg.RunsDir = strings.TrimSpace(runsDir)
	}
	if strings.TrimSpace(proxyURL) != "" {
		yamlCfg.Proxy.Enabled = true
		yamlCfg.Proxy.Mode = "single"
		yamlCfg.Proxy.Proxy = strings.TrimSpace(proxyURL)
	}
	if strings.TrimSpace(proxyPoolPath) != "" {
		yamlCfg.Proxy.Enabled = true
		yamlCfg.Proxy.Mode = "pool"
		yamlCfg.Proxy.PoolFile = strings.TrimSpace(proxyPoolPath)
	}
	if explicitFlags["headless"] {
		yamlCfg.BrowserHeadless = &headless
	} else {
		headless = yamlCfg.BrowserHeadlessValue()
	}
	if explicitFlags["include-secrets"] {
		yamlCfg.IncludeSecrets = &includeSecrets
	} else {
		includeSecrets = yamlCfg.IncludeSecretsValue()
	}

	var dockerPool *dockerpool.Pool
	if mode != "oauth" && strings.TrimSpace(yamlCfg.BrowserBackend) == "docker" {
		dockerPool = dockerpool.New()
		dockerPool.Initialize(ctx, yamlCfg)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatalf("创建结果目录失败: %v", err)
	}
	if mode != "oauth" {
		if err := os.MkdirAll(screenshotDir, 0o755); err != nil {
			log.Fatalf("创建截图目录失败: %v", err)
		}
	}
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("打开结果文件失败: %v", err)
	}
	defer out.Close()

	planned := make([]migratedruntime.PlannedAccount, 0, len(accounts))
	for _, acc := range accounts {
		planned = append(planned, migratedruntime.PlannedAccount{Index: acc.Index, Email: acc.Email, Password: acc.Password, Name: "ChatGPT User"})
	}
	runCtx, err := migratedruntime.CreatePlannedRun(mode, planned, yamlCfg.RunsDir, map[string]any{"proxy_enabled": yamlCfg.Proxy.Enabled})
	if err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	runStore := migratedruntime.StoreFromContext(*runCtx)
	proxyPool := buildProxyPool(yamlCfg, configPath)

	if flag.NFlag() == 0 && flag.NArg() == 0 {
		ok, err := confirmRunSummary(stdin, runSummary{
			Mode:           mode,
			AccountCount:   len(accounts),
			AccountSource:  accountSource,
			OutputPath:     outputPath,
			RunDir:         runCtx.RunDir,
			DBEnabled:      accountStore != nil,
			DBWriteResults: accountStore != nil && dbWriteResults,
		})
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			log.Printf("已取消执行")
			return
		}
	}

	rc := runnerConfig{
		mode:           mode,
		proxyURL:       proxyURL,
		screenshotDir:  screenshotDir,
		includeSecrets: includeSecrets,
		interactive:    interactive,
		codesPath:      codesPath,
		stdin:          stdin,
		preferHosts:    splitCSV(preferHostsRaw),
		workspaceID:    strings.TrimSpace(workspaceID),
		organizationID: strings.TrimSpace(organizationID),
		runsDir:        yamlCfg.RunsDir,
		runID:          runCtx.RunID,
		runStore:       &runStore,
		accountStore:   accountStore,
		dbWriteResults: accountStore != nil && dbWriteResults,
		proxyPool:      proxyPool,
	}

	log.Printf("运行目录: %s", runCtx.RunDir)
	if mode == "oauth" {
		log.Printf("开始执行：账号=%d，模式=%s，并发=%d", len(accounts), mode, workers)
	} else {
		log.Printf("开始执行：账号=%d，模式=%s，后端=%s，并发=%d", len(accounts), mode, firstNonEmpty(yamlCfg.BrowserBackend, "local"), workers)
	}
	runBatch(ctx, accounts, workers, dockerPool, yamlCfg, &rc, out)
	log.Printf("执行完成，结果已写入 %s", outputPath)
}

func dbStatusesForMode(mode, raw string) []string {
	statuses := splitCSV(raw)
	if len(statuses) > 0 {
		return statuses
	}
	return []string{"active"}
}
