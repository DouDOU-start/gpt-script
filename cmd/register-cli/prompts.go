package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func runInteractiveWizard(stdin *bufio.Reader, mode *string, accountsPath *string, generateCount *int, emailDomain *string, passwordLength *int, backend *string, proxyURL *string, outputPath *string, screenshotDir *string, workers *int, headless *bool, includeSecrets *bool) ([]accountInput, error) {
	fmt.Println("ChatGPT 注册脚本交互式向导")
	fmt.Println("直接回车使用默认值。")

	modeChoice, err := promptChoice("运行模式", []string{"注册新账号", "登录已有账号", "OAuth 授权"}, 1, stdin)
	if err != nil {
		return nil, err
	}
	switch modeChoice {
	case 2:
		*mode = "login"
	case 3:
		*mode = "oauth"
	default:
		*mode = "register"
	}

	var accounts []accountInput
	accountOptions := []string{"随机生成账号", "手动输入单个账号", "读取账号文件"}
	defaultAccountOption := 1
	if *mode == "login" || *mode == "oauth" {
		defaultAccountOption = 2
	}
	accountChoice, err := promptChoice("账号来源", accountOptions, defaultAccountOption, stdin)
	if err != nil {
		return nil, err
	}
	switch accountChoice {
	case 1:
		if *mode == "login" || *mode == "oauth" {
			return nil, fmt.Errorf("登录/OAuth 模式不能随机生成账号")
		}
		count, err := promptInt("生成数量 [1]: ", 1, stdin)
		if err != nil {
			return nil, err
		}
		domain, err := promptString(fmt.Sprintf("邮箱域名 [%s]: ", normalizeDomain(*emailDomain)), normalizeDomain(*emailDomain), stdin)
		if err != nil {
			return nil, err
		}
		length, err := promptInt(fmt.Sprintf("密码长度 [%d]: ", *passwordLength), *passwordLength, stdin)
		if err != nil {
			return nil, err
		}
		*generateCount = count
		*emailDomain = normalizeDomain(domain)
		*passwordLength = length
		accounts, err = createGeneratedAccounts(*generateCount, *emailDomain, *passwordLength, false, stdin)
		if err != nil {
			return nil, err
		}
	case 2:
		email, err := promptString("邮箱: ", "", stdin)
		if err != nil {
			return nil, err
		}
		password, err := promptString("密码: ", "", stdin)
		if err != nil {
			return nil, err
		}
		email = strings.TrimSpace(email)
		if email == "" || password == "" {
			return nil, fmt.Errorf("邮箱和密码不能为空")
		}
		accounts = []accountInput{{Email: email, Password: password}}
	case 3:
		path, err := promptString("账号文件路径 [accounts.txt]: ", "accounts.txt", stdin)
		if err != nil {
			return nil, err
		}
		*accountsPath = path
		accounts, err = loadAccounts("", "", *accountsPath)
		if err != nil {
			return nil, err
		}
	}

	backendChoice, err := promptChoice("浏览器后端", []string{"读取 config.yaml", "本机浏览器", "Docker 浏览器"}, 1, stdin)
	if err != nil {
		return nil, err
	}
	switch backendChoice {
	case 2:
		*backend = "local"
	case 3:
		*backend = "docker"
	}

	proxy, err := promptString("代理地址，留空不用代理: ", *proxyURL, stdin)
	if err != nil {
		return nil, err
	}
	*proxyURL = strings.TrimSpace(proxy)

	workerCount, err := promptInt(fmt.Sprintf("并发数 [%d]: ", *workers), *workers, stdin)
	if err != nil {
		return nil, err
	}
	*workers = workerCount

	newHeadless, err := promptYesNo("是否无头运行浏览器？[Y/n]: ", *headless, stdin)
	if err != nil {
		return nil, err
	}
	*headless = newHeadless

	newIncludeSecrets, err := promptYesNo("结果文件是否保存密码/cookies/token？[Y/n]: ", *includeSecrets, stdin)
	if err != nil {
		return nil, err
	}
	*includeSecrets = newIncludeSecrets

	newOutputPath, err := promptString(fmt.Sprintf("结果输出文件 [%s]: ", *outputPath), *outputPath, stdin)
	if err != nil {
		return nil, err
	}
	*outputPath = newOutputPath

	newScreenshotDir, err := promptString(fmt.Sprintf("截图目录 [%s]: ", *screenshotDir), *screenshotDir, stdin)
	if err != nil {
		return nil, err
	}
	*screenshotDir = newScreenshotDir

	return accounts, nil
}

func promptGenerateOptions(defaultDomain string, defaultPasswordLength int, stdin *bufio.Reader) (int, string, int, error) {
	if ok, err := promptYesNo("未提供账号，是否随机生成注册账号？[Y/n]: ", true, stdin); err != nil || !ok {
		return 0, defaultDomain, defaultPasswordLength, err
	}

	count, err := promptInt("生成数量 [1]: ", 1, stdin)
	if err != nil {
		return 0, defaultDomain, defaultPasswordLength, err
	}
	domain, err := promptString(fmt.Sprintf("邮箱域名 [%s]: ", defaultDomain), defaultDomain, stdin)
	if err != nil {
		return 0, defaultDomain, defaultPasswordLength, err
	}
	passwordLength, err := promptInt(fmt.Sprintf("密码长度 [%d]: ", defaultPasswordLength), defaultPasswordLength, stdin)
	if err != nil {
		return 0, defaultDomain, defaultPasswordLength, err
	}
	if passwordLength < 8 {
		return 0, defaultDomain, defaultPasswordLength, fmt.Errorf("password-length 不能小于 8")
	}
	return count, normalizeDomain(domain), passwordLength, nil
}

func promptChoice(title string, options []string, defaultChoice int, stdin *bufio.Reader) (int, error) {
	if defaultChoice < 1 || defaultChoice > len(options) {
		defaultChoice = 1
	}
	for {
		fmt.Println()
		fmt.Println(title + ":")
		for i, option := range options {
			fmt.Printf("  %d. %s\n", i+1, option)
		}
		fmt.Printf("请选择 [%d]: ", defaultChoice)
		line, err := stdin.ReadString('\n')
		if err != nil {
			return 0, err
		}
		value := strings.TrimSpace(line)
		if value == "" {
			return defaultChoice, nil
		}
		choice, err := strconv.Atoi(value)
		if err == nil && choice >= 1 && choice <= len(options) {
			return choice, nil
		}
		fmt.Printf("请输入 1-%d 之间的数字\n", len(options))
	}
}

func promptYesNo(prompt string, defaultValue bool, stdin *bufio.Reader) (bool, error) {
	for {
		fmt.Print(prompt)
		line, err := stdin.ReadString('\n')
		if err != nil {
			return false, err
		}
		value := strings.ToLower(strings.TrimSpace(line))
		if value == "" {
			return defaultValue, nil
		}
		switch value {
		case "y", "yes", "是", "好":
			return true, nil
		case "n", "no", "否", "不":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n")
		}
	}
}

func promptInt(prompt string, defaultValue int, stdin *bufio.Reader) (int, error) {
	for {
		fmt.Print(prompt)
		line, err := stdin.ReadString('\n')
		if err != nil {
			return 0, err
		}
		value := strings.TrimSpace(line)
		if value == "" {
			return defaultValue, nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			fmt.Println("请输入大于 0 的整数")
			continue
		}
		return n, nil
	}
}

func promptString(prompt string, defaultValue string, stdin *bufio.Reader) (string, error) {
	fmt.Print(prompt)
	line, err := stdin.ReadString('\n')
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}
