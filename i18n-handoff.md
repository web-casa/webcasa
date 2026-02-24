# i18n 翻译工作交接文档

> 本文档为低级模型（或后续开发者）提供 Phase 2 翻译工作的上下文和规范。

## 已完成的工作

框架已搭建完毕，以下文件已就位：

| 文件 | 说明 |
|------|------|
| `web/src/i18n.js` | i18next 初始化，语言检测 (localStorage → navigator)，fallback=en |
| `web/src/locales/en.json` | 英文翻译（**已包含全部 12 个命名空间的完整 key**） |
| `web/src/locales/zh.json` | 中文翻译（**已包含全部 12 个命名空间的完整 key**） |
| `web/src/main.jsx` | 已引入 `import './i18n.js'` |
| `web/src/pages/Login.jsx` | ✅ 已改造完毕（参考示例） |
| `web/src/pages/Layout.jsx` | ✅ 已改造完毕（参考示例） |

## 你的任务

将以下 **9 个页面文件** 中的硬编码中文/英文字符串替换为 `t()` 调用。

### 待改造文件清单

1. `web/src/pages/Dashboard.jsx` — 使用 `dashboard.*` 命名空间
2. `web/src/pages/HostList.jsx` — 使用 `host.*` + `common.*` 命名空间（最大文件，~53KB）
3. `web/src/pages/Settings.jsx` — 使用 `settings.*` 命名空间
4. `web/src/pages/Users.jsx` — 使用 `user.*` 命名空间
5. `web/src/pages/AuditLogs.jsx` — 使用 `audit.*` 命名空间
6. `web/src/pages/DnsProviders.jsx` — 使用 `dns.*` 命名空间
7. `web/src/pages/Certificates.jsx` — 使用 `cert.*` 命名空间
8. `web/src/pages/CaddyfileEditor.jsx` — 使用 `editor.*` 命名空间
9. `web/src/pages/Logs.jsx` — 使用 `log.*` 命名空间

## 改造模式（参照 Login.jsx 和 Layout.jsx）

### Step 1: 添加 import

在文件顶部添加：

```jsx
import { useTranslation } from 'react-i18next'
```

### Step 2: 在组件函数内获取 `t`

```jsx
export default function Dashboard() {
    const { t } = useTranslation()
    // ...
}
```

### Step 3: 替换硬编码字符串

```jsx
// 之前：
<Heading>站点分布</Heading>

// 之后：
<Heading>{t('dashboard.host_distribution')}</Heading>
```

### Step 4: 带插值的情况

```jsx
// 之前：
tooltip={`代理: ${hosts.proxy ?? 0} / 跳转: ${hosts.redirect ?? 0}`}

// 之后：
tooltip={`${t('dashboard.proxy_count', { count: hosts.proxy ?? 0 })} / ${t('dashboard.redirect_count', { count: hosts.redirect ?? 0 })}`}
```

## 翻译 Key 查找

所有翻译 key 已在 `en.json` 和 `zh.json` 中预定义。直接搜索对应命名空间即可。

**如果发现某个字符串在翻译文件中没有对应 key**，请：
1. 在 `en.json` 和 `zh.json` 的对应命名空间下新增 key
2. key 命名使用小写 + 下划线，如 `host.confirm_delete`

## 通用 key（`common.*`）

按钮文本优先使用 `common.*`：
- Save → `t('common.save')`
- Cancel → `t('common.cancel')`
- Delete → `t('common.delete')`
- Loading... → `t('common.loading')`
- Enabled/Disabled → `t('common.enabled')` / `t('common.disabled')`

## 验证

每改完一个文件后运行：

```bash
cd /home/ivmm/webcasa/web && npm run build
```

确认无编译错误。全部完成后可选择在浏览器中测试语言切换。
