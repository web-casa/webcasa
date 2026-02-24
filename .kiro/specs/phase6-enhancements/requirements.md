# 需求文档 — Phase 6 增强功能

## 简介

WebCasa Phase 6 批量开发，包含 6 个新功能：站点一键克隆、域名 DNS 解析检查、响应式移动端适配、2FA TOTP 双因素认证、站点分组/标签、站点模板。这些功能旨在提升面板的易用性、安全性和管理效率。

## 术语表

- **Panel**：WebCasa Web 管理面板（Go 后端 + React 前端）
- **Host**：面板中管理的一个站点配置，对应 Caddyfile 中的一个 server block
- **HostList**：站点列表页面，展示所有 Host 的表格视图
- **Clone_Dialog**：站点克隆弹窗，用于输入新域名并执行克隆操作
- **DNS_Checker**：域名 DNS 解析检查模块，验证域名 A/AAAA 记录是否指向服务器 IP
- **Sidebar**：左侧导航栏组件，包含所有页面入口
- **Hamburger_Menu**：移动端折叠菜单按钮，点击展开/收起 Sidebar
- **TOTP**：基于时间的一次性密码算法（Time-based One-Time Password，RFC 6238）
- **Recovery_Code**：2FA 恢复码，用于在 TOTP 设备丢失时恢复账户访问
- **Group**：站点分组，一个 Host 只能属于一个 Group（一对多关系）
- **Tag**：站点标签，一个 Host 可以有多个 Tag（多对多关系）
- **Template**：站点模板，保存 Host 配置的可复用快照
- **Preset_Template**：系统内置的预设模板（如 WordPress、SPA 静态站等）
- **Settings**：系统设置页面，包含服务器 IP 等全局配置
- **AuditLog**：审计日志，记录所有增删改操作
- **Dashboard**：仪表盘页面，展示系统概览统计

## 需求

### 需求 1：站点一键克隆

**用户故事：** 作为管理员，我想基于已有站点配置快速创建新站点，以便避免重复手动配置相同参数。

#### 验收标准

1. WHEN 管理员在 HostList 中点击某个 Host 的克隆按钮，THE Panel SHALL 弹出 Clone_Dialog 并预填原 Host 的域名供参考。
2. WHEN 管理员在 Clone_Dialog 中输入新域名并确认，THE Panel SHALL 复制原 Host 的全部配置字段（host_type、tls_mode、compression、cors、security_headers 等所有主表字段）到新 Host。
3. WHEN 克隆操作执行时，THE Panel SHALL 同时复制原 Host 的所有子表数据，包括 upstreams、custom_headers、access_rules、basic_auths 和 routes。
4. WHEN 克隆操作执行时，THE Panel SHALL 为新 Host 生成独立的数据库记录（新 ID），子表记录的 host_id 指向新 Host。
5. IF 用户输入的新域名已存在于数据库中，THEN THE Panel SHALL 返回明确的域名重复错误提示，拒绝创建。
6. WHEN 克隆操作成功完成，THE Panel SHALL 记录一条 AuditLog，包含操作类型 "CLONE"、源 Host ID 和新 Host ID。
7. WHEN 克隆操作成功完成，THE Panel SHALL 自动触发 ApplyConfig 流程（重新生成 Caddyfile 并 reload Caddy）。

### 需求 2：域名 DNS 解析检查

**用户故事：** 作为管理员，我想在申请 TLS 证书前检查域名 DNS 解析是否正确，以便避免因 DNS 未生效导致证书申请失败。

#### 验收标准

1. THE Panel SHALL 提供一个 DNS 解析检查 API 端点，接受域名参数并返回该域名的 A 和 AAAA 记录。
2. WHEN DNS_Checker 执行检查时，THE DNS_Checker SHALL 将查询到的 A 记录与 Settings 中配置的 server_ipv4 进行比对，将 AAAA 记录与 server_ipv6 进行比对。
3. WHEN DNS 记录与服务器 IP 匹配时，THE DNS_Checker SHALL 返回 "matched" 状态。
4. WHEN DNS 记录与服务器 IP 不匹配时，THE DNS_Checker SHALL 返回 "mismatched" 状态，并附带实际解析到的 IP 地址列表。
5. IF DNS 查询失败或域名无任何 A/AAAA 记录，THEN THE DNS_Checker SHALL 返回 "no_record" 状态和具体错误信息。
6. WHEN 管理员在 HostList 页面查看站点列表时，THE Panel SHALL 在每个 Host 行中展示 DNS 解析状态图标（匹配为绿色、不匹配为黄色、无记录为灰色）。
7. WHEN 管理员在 Host 编辑弹窗中输入或修改域名时，THE Panel SHALL 自动触发 DNS 解析检查并展示结果。
8. WHILE Settings 中未配置 server_ipv4 和 server_ipv6（两者均为空），THE DNS_Checker SHALL 跳过比对逻辑，仅返回查询到的 DNS 记录。

### 需求 3：响应式移动端适配

**用户故事：** 作为管理员，我想在手机上也能正常使用面板的核心功能，以便在移动场景下快速查看和管理站点。

#### 验收标准

1. WHEN 浏览器视口宽度小于 768px 时，THE Sidebar SHALL 默认隐藏，并在页面顶部显示 Hamburger_Menu 按钮。
2. WHEN 管理员点击 Hamburger_Menu 按钮时，THE Sidebar SHALL 以覆盖层（overlay）形式从左侧滑入展示。
3. WHEN 管理员在移动端 Sidebar 中点击任意导航链接时，THE Sidebar SHALL 自动收起。
4. WHEN 浏览器视口宽度小于 768px 时，THE Dashboard SHALL 将统计卡片从多列布局切换为单列纵向排列。
5. WHEN 浏览器视口宽度小于 768px 时，THE HostList SHALL 将表格视图切换为卡片列表视图，每个 Host 显示为独立卡片。
6. WHEN 浏览器视口宽度小于 768px 时，THE Settings SHALL 将表单布局调整为全宽单列排列。
7. WHEN 浏览器视口宽度大于等于 768px 时，THE Panel SHALL 恢复桌面端的标准布局（固定 Sidebar + 多列内容）。

### 需求 4：2FA TOTP 双因素认证

**用户故事：** 作为管理员，我想为账户启用 TOTP 双因素认证，以便增强账户安全性防止未授权登录。

#### 验收标准

1. THE Panel SHALL 提供 2FA 设置 API，允许用户生成 TOTP 密钥并返回 otpauth:// URI 和 Base32 编码的密钥。
2. WHEN 用户请求启用 2FA 时，THE Panel SHALL 生成一个 TOTP 密钥，并以 QR 码形式展示 otpauth:// URI 供 Google Authenticator 或 Authy 扫描。
3. WHEN 用户扫描 QR 码后提交验证码确认时，THE Panel SHALL 验证该 TOTP 验证码的正确性，验证通过后才正式启用 2FA。
4. WHEN 2FA 启用成功时，THE Panel SHALL 生成 8 个一次性 Recovery_Code（每个为 8 位字母数字组合），以 bcrypt 哈希存储，并向用户展示明文恢复码供保存。
5. WHEN 已启用 2FA 的用户登录时，THE Panel SHALL 在用户名密码验证通过后，要求输入 6 位 TOTP 验证码作为第二步验证。
6. WHEN 用户在 2FA 验证步骤输入 Recovery_Code 替代 TOTP 验证码时，THE Panel SHALL 验证该恢复码的有效性，验证通过后允许登录并将该恢复码标记为已使用。
7. IF 用户输入的 TOTP 验证码或 Recovery_Code 均无效，THEN THE Panel SHALL 拒绝登录并返回 "验证码无效" 错误。
8. THE Panel SHALL 允许已启用 2FA 的用户禁用 2FA，禁用时需要输入当前 TOTP 验证码确认身份。
9. WHEN 2FA 状态发生变更（启用或禁用）时，THE Panel SHALL 记录一条 AuditLog。
10. THE Panel SHALL 将 2FA 设为可选功能，未启用 2FA 的用户登录流程保持不变（仅用户名密码）。

### 需求 5：站点分组与标签

**用户故事：** 作为管理员，我想对站点进行分组和打标签，以便在站点数量较多时快速筛选和批量管理。

#### 验收标准

1. THE Panel SHALL 提供 Group CRUD API，每个 Group 包含名称和可选的颜色属性。
2. THE Panel SHALL 提供 Tag CRUD API，每个 Tag 包含名称和可选的颜色属性。
3. THE Panel SHALL 支持为 Host 分配一个 Group（一对多关系：一个 Group 包含多个 Host，一个 Host 最多属于一个 Group）。
4. THE Panel SHALL 支持为 Host 分配多个 Tag（多对多关系：一个 Host 可有多个 Tag，一个 Tag 可关联多个 Host）。
5. WHEN 管理员在 HostList 页面选择 Group 筛选条件时，THE HostList SHALL 仅展示属于该 Group 的 Host。
6. WHEN 管理员在 HostList 页面选择 Tag 筛选条件时，THE HostList SHALL 仅展示包含该 Tag 的 Host。
7. WHEN 管理员在 HostList 页面同时选择 Group 和 Tag 筛选条件时，THE HostList SHALL 展示同时满足两个条件的 Host。
8. THE Panel SHALL 在 HostList 的每个 Host 行中展示其所属 Group 名称和关联的 Tag 列表。
9. WHEN 管理员对某个 Group 执行 "批量禁用" 操作时，THE Panel SHALL 将该 Group 下所有 Host 的 enabled 字段设为 false，并触发 ApplyConfig。
10. WHEN 管理员对某个 Group 执行 "批量启用" 操作时，THE Panel SHALL 将该 Group 下所有 Host 的 enabled 字段设为 true，并触发 ApplyConfig。
11. WHEN Group 或 Tag 的增删改操作执行时，THE Panel SHALL 记录 AuditLog。
12. IF 管理员尝试删除一个仍关联有 Host 的 Group，THEN THE Panel SHALL 提示确认，并在确认后将关联 Host 的 group_id 设为空（解除关联），而非级联删除 Host。

### 需求 6：站点模板

**用户故事：** 作为管理员，我想将常用的站点配置保存为模板并复用，以便快速创建标准化配置的新站点，也能与其他 WebCasa 实例共享模板。

#### 验收标准

1. THE Panel SHALL 提供 Template CRUD API，每个 Template 包含名称、描述、模板类型（preset 或 custom）和完整的 Host 配置快照（JSON 格式）。
2. WHEN 管理员在 Host 编辑页面点击 "保存为模板" 时，THE Panel SHALL 将当前 Host 的全部配置（包括子表数据 upstreams、custom_headers、access_rules、basic_auths、routes）序列化为 JSON 并保存为 custom 类型模板。
3. WHEN 管理员选择 "从模板创建新 Host" 时，THE Panel SHALL 展示所有可用模板（preset + custom），管理员选择模板后输入新域名即可创建 Host。
4. WHEN 从模板创建 Host 时，THE Panel SHALL 将模板中的配置反序列化并写入新 Host 记录及其子表，域名使用用户输入的新域名。
5. THE Panel SHALL 支持模板导出功能，将模板序列化为 JSON 文件供下载。
6. THE Panel SHALL 支持模板导入功能，接受 JSON 文件上传并解析为 Template 记录。
7. IF 导入的模板 JSON 格式无效或缺少必要字段，THEN THE Panel SHALL 返回明确的格式错误提示，拒绝导入。
8. THE Panel SHALL 内置 6 个 Preset_Template，分别为：WordPress 反代、SPA 静态站、API 反向代理、PHP-FPM 站点、静态文件下载站、WebSocket 应用。
9. THE Panel SHALL 在数据库初始化时自动创建 Preset_Template，Preset_Template 不可被用户删除或修改。
10. WHEN 模板的增删改或导入导出操作执行时，THE Panel SHALL 记录 AuditLog。
11. FOR ALL 有效的 Template 配置快照 JSON，导出后再导入 SHALL 产生等价的 Template 记录（round-trip 属性）。

### 需求 7：多语言翻译（i18n）

**用户故事：** 作为管理员，我想在使用 Phase 6 新增功能时看到与现有页面一致的多语言支持，以便中英文用户都能正常使用。

#### 验收标准

1. FOR ALL Phase 6 新增的前端 UI 文本（按钮、标签、提示、错误信息、占位符），THE Panel SHALL 在 `web/src/locales/en.json` 和 `web/src/locales/zh.json` 中添加对应的翻译键值对。
2. FOR ALL Phase 6 新增的前端组件，THE Panel SHALL 使用 `useTranslation()` hook 和 `t()` 函数引用翻译键，不得硬编码任何用户可见的中文或英文文本。
3. WHEN 用户切换语言时，THE Panel SHALL 将 Phase 6 新增功能的所有 UI 文本同步切换为目标语言。
4. FOR ALL 6 个内置 Preset_Template 的名称和描述，THE Panel SHALL 提供中英文翻译，在前端根据当前语言动态展示。
5. FOR ALL Phase 6 新增的后端 API 错误信息，THE Panel SHALL 返回可被前端翻译的错误码或键名，前端根据当前语言展示对应的本地化错误提示。
