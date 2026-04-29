package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
)

func loadAccounts(email, password, path string) ([]accountInput, error) {
	if path == "" {
		if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
			return nil, nil
		}
		return []accountInput{{Email: strings.TrimSpace(email), Password: password}}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开账号文件失败: %w", err)
	}
	defer f.Close()

	var accounts []accountInput
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		acc, err := parseAccountLine(line)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func parseAccountLine(line string) (accountInput, error) {
	if strings.HasPrefix(line, "{") {
		var acc accountInput
		if err := json.Unmarshal([]byte(line), &acc); err != nil {
			return accountInput{}, fmt.Errorf("解析账号 JSON 失败: %w", err)
		}
		acc.Email = strings.TrimSpace(acc.Email)
		if acc.Email == "" || acc.Password == "" {
			return accountInput{}, fmt.Errorf("账号 JSON 缺少 email/password")
		}
		return acc, nil
	}

	for _, sep := range []string{"----", ",", "\t"} {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 {
			acc := accountInput{Email: strings.TrimSpace(parts[0]), Password: strings.TrimSpace(parts[1])}
			if acc.Email == "" || acc.Password == "" {
				return accountInput{}, fmt.Errorf("账号行缺少邮箱或密码: %s", line)
			}
			return acc, nil
		}
	}
	return accountInput{}, fmt.Errorf("账号行格式错误: %s", line)
}

func createGeneratedAccounts(count int, domain string, passwordLength int, interactive bool, stdin *bufio.Reader) ([]accountInput, error) {
	domain = normalizeDomain(domain)
	if count <= 0 {
		if !interactive {
			return nil, nil
		}
		var err error
		count, domain, passwordLength, err = promptGenerateOptions(domain, passwordLength, stdin)
		if err != nil {
			return nil, err
		}
	}
	if count <= 0 {
		return nil, nil
	}

	accounts := make([]accountInput, 0, count)
	seen := make(map[string]struct{}, count)
	for len(accounts) < count {
		local, err := randomString("abcdefghijklmnopqrstuvwxyz0123456789", 12)
		if err != nil {
			return nil, fmt.Errorf("生成邮箱失败: %w", err)
		}
		email := "u" + local + "@" + domain
		if _, exists := seen[email]; exists {
			continue
		}
		password, err := randomString("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*_-+=", passwordLength)
		if err != nil {
			return nil, fmt.Errorf("生成密码失败: %w", err)
		}
		seen[email] = struct{}{}
		accounts = append(accounts, accountInput{Email: email, Password: password})
	}

	for _, acc := range accounts {
		log.Printf("已生成账号: %s / %s", acc.Email, acc.Password)
	}
	return accounts, nil
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "@")
	if domain == "" {
		return "k9ray.com"
	}
	return domain
}

func randomString(charset string, length int) (string, error) {
	b := make([]byte, length)
	max := big.NewInt(int64(len(charset)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = charset[n.Int64()]
	}
	return string(b), nil
}
