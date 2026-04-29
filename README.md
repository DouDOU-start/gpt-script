# ChatGPT Register Script

一个 Go CLI 工具，支持 ChatGPT 账号协议注册、浏览器登录和 OAuth 授权。当前项目以本仓库代码为准，`chatgpt-register/` 仅作为参考目录。

## 功能特性

- `register`：协议注册新账号，输出 session、workspace 和 token 信息
- `login`：通过本机或 Docker 浏览器登录已有账号
- `oauth`：协议 OAuth 授权，输出 OAuth access/refresh token
- 支持单账号、账号文件和注册账号随机生成
- 支持本机 Chrome/Chromium 与 Docker 浏览器后端
- 支持 HTTP 代理、验证码交互输入或文件读取
- 自动保存 JSONL 结果和浏览器流程截图

## 环境要求

- Go 1.24+
- 本机模式需要 Chrome/Chromium
- Docker 模式需要可用 Docker 环境

## 安装依赖

```bash
go mod download
```

也可以使用 Makefile：

```bash
make tidy
```

## 配置

首次运行建议复制示例配置：

```bash
cp config.yaml.example config.yaml
```

示例配置：

```yaml
browser_backend: docker
browser_image: chromedp/headless-shell:latest
browser_headless: true

docker_hosts:
  - name: 本地 Docker
    url: unix:///var/run/docker.sock
    max_containers: 2
    enabled: true
```

`browser_backend` 可选值：

- `docker`：使用 Docker 容器浏览器，适合批量运行，也是缺省配置
- `local`：使用本机 Chrome/Chromium，适合本地调试

## 账号文件格式

批量账号文件支持三种格式：

```text
user@example.com----password
user@example.com,password
{"email":"user@example.com","password":"password"}
```

可以参考 `accounts.example.txt`。

## 运行方式

查看命令参数：

```bash
go run ./cmd/register-cli -h
```

随机生成账号并注册：

```bash
go run ./cmd/register-cli \
  -mode register \
  -generate 1 \
  -domain example.com \
  -password-length 14
```

使用账号文件批量注册：

```bash
go run ./cmd/register-cli \
  -mode register \
  -accounts accounts.txt \
  -workers 1
```

登录已有账号：

```bash
go run ./cmd/register-cli \
  -mode login \
  -backend local \
  -email "user@example.com" \
  -password "your-password"
```

执行 OAuth 授权：

```bash
go run ./cmd/register-cli \
  -mode oauth \
  -email "user@example.com" \
  -password "your-password"
```

使用代理：

```bash
go run ./cmd/register-cli \
  -accounts accounts.txt \
  -proxy "http://user:pass@host:port"
```

指定 Docker 主机白名单：

```bash
go run ./cmd/register-cli \
  -backend docker \
  -accounts accounts.txt \
  -prefer-hosts "本地 Docker" \
  -workers 2
```

## 验证码

默认情况下，程序遇到验证码会在控制台提示输入。

也可以通过文件提供验证码：

```text
user@example.com=123456
another@example.com=654321
```

运行时指定验证码文件：

```bash
go run ./cmd/register-cli \
  -accounts accounts.txt \
  -codes codes.txt
```

## 输出结果

默认输出路径：

```text
results/register_results.jsonl
```

默认截图目录：

```text
screenshots/
```

结果中默认包含 cookies、access token、password 等敏感字段。共享结果文件前可以关闭敏感字段输出：

```bash
go run ./cmd/register-cli \
  -accounts accounts.txt \
  -include-secrets=false
```

## 常用参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-mode` | `register` | 运行模式：`register`、`login`、`oauth` |
| `-email` | 空 | 单账号邮箱 |
| `-password` | 空 | 单账号密码 |
| `-accounts` | 空 | 批量账号文件 |
| `-generate` | `0` | 随机生成注册账号数量 |
| `-domain` | `k9ray.com` | 随机生成邮箱域名 |
| `-password-length` | `14` | 随机生成密码长度，不能小于 8 |
| `-backend` | 读取配置 | 浏览器后端：`local` 或 `docker` |
| `-workers` | `1` | 批量并发数 |
| `-proxy` | 空 | 代理地址 |
| `-codes` | 空 | 验证码文件，格式 `email=code` |
| `-prefer-hosts` | 空 | Docker 主机名白名单，逗号分隔 |
| `-workspace-id` | 空 | OAuth 工作空间 ID，留空自动选择个人空间 |
| `-organization-id` | 空 | OAuth 组织 ID，留空自动选择第一个组织 |
| `-output` | `results/register_results.jsonl` | 结果输出文件 |
| `-screenshots` | `screenshots` | 截图目录 |
| `-headless` | `true` | 是否无头运行浏览器 |
| `-include-secrets` | `true` | 是否输出敏感字段 |
| `-interactive` | `true` | 是否允许控制台输入验证码 |

## Makefile

```bash
make build    # 构建 bin/register-cli
make run      # 运行 go run ./cmd/register-cli
make test     # 运行 go test ./...
make vet      # 运行 go vet ./...
make fmt      # 格式化 cmd/internal
make tidy     # 整理 Go 依赖
make clean    # 删除 bin/
```

## 构建

```bash
make build
```

构建后的二进制文件位于：

```text
bin/register-cli
```

运行二进制：

```bash
./bin/register-cli -h
```

## 注意事项

- 本机调试建议使用 `-backend local`。
- 批量运行建议使用 Docker 后端并按机器资源调整 `-workers`。
- `results/`、`screenshots/`、`backups/`、`config.yaml`、`codes.txt` 默认不会提交到 Git。
- 输出结果可能包含敏感字段，不要公开共享未脱敏的 JSONL 文件。
