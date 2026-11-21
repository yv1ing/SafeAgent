# 快速上手

[English](getting-started.md)

本指南从一份全新的代码副本开始，最终让 Agent 流量经过 SafeAgent。

## 环境准备

需要以下环境：

- Go 1.27.4
- Node.js 22 或更新版本
- GNU Make
- 已创建的 PostgreSQL 17 数据库

SafeAgent 在首次启动时创建表，但不会创建数据库本身。先安装锁定版本的依赖：

```sh
make deps
```

## 配置与启动

复制示例配置，并填写 `database_url`：

```sh
cp config.example.toml config.toml
```

```toml
listen = "127.0.0.1:8080"
database_url = "postgres://safeagent:change-me@localhost:5432/safeagent?sslmode=disable"
secure_cookies = false
```

其余配置项均有默认值。以下两项需要按部署方式调整：

- `secure_cookies`：仅在本机明文 HTTP 访问控制台时设为 `false`；其他部署使用 HTTPS 并设为 `true`，否则浏览器会丢弃 Secure Cookie。
- `sslmode`：`disable` 仅适用于本地数据库；远程数据库应启用 TLS。

配置中出现无法识别的键会导致启动失败，拼写错误不会被忽略。

构建并运行：

```sh
make build
build/bin/safeagent -config config.toml serve
```

首次连接空数据库时，进程会创建数据库结构，并在标准输出中打印一次初始管理员账号与密码：

```text
Initial administrator: admin / <password>
```

请妥善保存，后续启动不会再次输出。密码丢失时，可由其他管理员重置，或重建数据库。

同一进程提供控制台（`/console/`）、网关（`/v1/` 和 `/v1beta/`）和健康检查（`/health`）。

## 控制台设置

使用 `admin` 登录 <http://127.0.0.1:8080/console/>，首次登录需要设置新密码。控制台默认打开监控页，汇总当前账号可见的所有项目。

创建项目时填写名称即可，其他默认值可稍后调整。新项目默认启用常用脱敏检测器，并带有一份可见的空策略文档。

进入项目后，侧边栏会切换为该项目的页面。管理员可在设置中修改名称、保留期和使用限额。

| 角色     | 可管理                                                                                     |
| -------- | ------------------------------------------------------------------------------------------ |
| 项目成员 | 路由、非阻断类脱敏规则、仅记录的策略规则、Agent 密钥                                       |
| 管理员   | 渠道、用户、全局日志、项目、成员、阻断类策略与脱敏规则、策略恢复、决策模型、保留天数和限额 |

信任级别和非默认角色仅管理员可以设置。操作记录页面列出本项目发生过的变更。

只有管理员可以创建渠道，每个渠道对应一个上游服务商：

- 选择提供商。Anthropic、OpenAI、Google、DeepSeek、Z.AI 与 Qwen 自带各自的格式、端点、鉴权与 capability profile；自定义提供商使用标准 Chat Completions，需要填写 base URL。
- 控制台会预填主端点。仅在提供商或代理要求时覆盖 base URL；仅在该提供商不支持某格式时禁用它。
- 模型名必须与提供商使用的名称一致。价格按每百万 Token 计算，币种由渠道设置决定。“发现模型”会列出提供商返回的名称。
- 添加上游密钥，保存后仅显示末尾几位。密钥使用实例密钥加密保存。渠道可以在没有密钥的情况下保存，但无法承载流量，启用路由前应补齐。
- 仅在服务商位于内网地址（例如自建推理服务）时启用“允许内网地址”。

创建模型路由。一条路由将对外模型名映射到一个或多个上游模型。优先级数字越小越先尝试，优先级相同时按权重随机选择。

| 结果                                       | 是否转到下一个目标 |
| ------------------------------------------ | ------------------ |
| 瞬时失败、鉴权失败、限流、目标格式转换失败 | 是                 |
| 策略阻断、非法请求                         | 否                 |

在密钥页面签发 Agent 密钥。完整密钥仅显示一次，请在签发时复制。单个密钥可以设置独立的速率和并发限制。只有管理员可以设置信任级别或非默认角色。

如果默认设置不适合当前项目，可以调整脱敏和策略。数据脱敏页面可以用示例文本测试规则效果；策略页面维护一张有序规则表，保存时校验并立即生效，历史版本可以恢复。

新项目默认没有策略规则，请求会正常转发并在后台诊断。如需网关拦截流量，应显式添加阻断规则。项目设置中还可以配置可选的决策模型；规则放行有副作用的工具调用后，模型会再做一次否决判断。

## 连接 Agent

将网关地址和密钥提供给 Agent，模型名使用路由中的对外名称；上游名称保留在渠道配置中。

| Agent 协议         | base URL                   | 密钥位置                            |
| ------------------ | -------------------------- | ----------------------------------- |
| Chat Completions   | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Responses          | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Anthropic Messages | `http://127.0.0.1:8080`    | `x-api-key: <key>`                  |
| Gemini             | `http://127.0.0.1:8080`    | `x-goog-api-key: <key>`，或 `?key=` |

可选的 `X-SafeAgent-Session` 请求头可以把同一 Agent 密钥下的调用归入同一会话。身份来自 Agent 密钥；客户端支持时应优先使用 `previous_response_id`。

快速验证：

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <agent-key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"public-model","messages":[{"role":"user","content":"hello"}]}'
```

验证脱敏效果时，可以发送包含邮箱地址的提示词，然后到控制台查看这次调用：发给服务商的请求中包含 `[EMAIL_…]` 之类的占位符，客户端收到的回复中仍为原始邮箱。

## 容器部署

构建镜像并挂载配置：

```sh
docker build -t safeagent .
docker run -d --name safeagent -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.toml:/app/config.toml:ro" safeagent
```

在容器中，服务需监听所有网卡，并通过容器内可解析的主机名访问数据库：

```toml
listen = "0.0.0.0:8080"
database_url = "postgres://safeagent:change-me@db-host:5432/safeagent?sslmode=require"
secure_cookies = true
```

启用 `secure_cookies = true` 后，控制台需要 HTTPS。应通过反向代理对外提供服务，由反向代理处理 TLS，并将 `public_origin` 设为浏览器实际访问的地址，例如 `https://safeagent.example.com`。若反向代理直接连接到应用，请将其 CIDR 加入 `trusted_proxies`，以便控制台读取客户端地址用于登录限流。镜像不包含 shell 和 HTTP 客户端，请从外部检查 `/health`。

## 常见问题

| 提示或现象                                          | 处理方式                                                                                                                                                |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `database_url is required`                          | 在配置文件中填写该项，或确认进程读取的是已编辑的文件。                                                                                                  |
| 启动时报未知配置键                                  | 某个键拼写有误，按错误信息中的名称改正。                                                                                                                |
| `the database schema does not match this build`     | 数据库来自另一个构建。重建数据库，系统不提供迁移。                                                                                                      |
| `the database has tables but no schema fingerprint` | 数据库由其他工具创建，或上次启动只完成了一部分。请重建数据库；缺失状态无法可靠推断。                                                                    |
| `another SafeAgent instance owns this database`     | 一个数据库只能由一个实例使用，请停止另一个进程。                                                                                                        |
| 控制台返回 404                                      | 控制台前端尚未构建，运行 `make build`。                                                                                                                 |
| 登录成功但刷新后被登出                              | 部署在 HTTP 下设置了 `secure_cookies = true`。使用 HTTPS，或在回环地址上将其设为 false。                                                                |
| 反向代理后控制台写操作失败                          | 将 `public_origin` 设为用户在浏览器中打开的地址。                                                                                                       |
| 请求返回 403，或流以工具调用错误结束                | 命中了阻断类脱敏规则、策略规则或决策模型的否决。请求阶段使用 HTTP 403；流已开始后的工具调用会以协议原生错误结束。调用记录会写明来源，异步分析仍会运行。 |

## 相关

- [架构](architecture_zh.md)
- [协议转换](protocol-conversion_zh.md)
- [Agent 安全模型](agent-safety_zh.md)
- [项目说明](../../README_zh.md)
