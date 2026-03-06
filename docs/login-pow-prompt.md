
# 任务

为本项目添加登录验证功能，采用 CPU 算力证明 (Proof-of-Work) 替代传统图形验证码。

# 设计来源

该方案来自 WebCasa 面板 (https://web.casa) 的生产实现，使用 Altcha 协议。
核心思路：服务端生成 SHA-256 挑战，客户端暴力搜索 nonce 证明"我是真人浏览器不是脚本"。

# 完整协议流程

```
客户端                                  服务端
  |                                       |
  |  GET /api/auth/challenge              |
  |-------------------------------------->|
  |                                       | 生成: salt(随机), secret_number(0~50000随机)
  |                                       | challenge = SHA256(salt + secret_number)
  |                                       | signature = HMAC-SHA256(challenge, hmac_key)
  |  {salt, challenge, maxnumber,         |
  |   algorithm, signature}               |
  |<--------------------------------------|
  |                                       |
  | 本地暴力搜索:                          |
  | for i = 0..50000:                     |
  |   if SHA256(salt + i) == challenge:   |
  |     found = i; break                  |
  |                                       |
  |  POST /api/auth/login                 |
  |  {username, password,                 |
  |   altcha: base64({algorithm,          |
  |     challenge, number, salt,          |
  |     signature})}                      |
  |-------------------------------------->|
  |                                       | 1. 验证 PoW (HMAC + hash 正确性)
  |                                       | 2. IP 限速检查 (5次/15分钟, 指数退避)
  |                                       | 3. 验证用户名密码 (bcrypt)
  |                                       | 4. 签发 JWT (HS256, 24h)
  |  {token, user}                        |
  |<--------------------------------------|

```

# 关键实现细节

## 1. 服务端挑战生成

参数：
- maxnumber: 50000 (搜索空间上限，普通设备 0.5~2 秒可解)
- 有效期: 120 秒
- HMAC 密钥: 从配置读取，服务端保密

Go 参考 (使用 altcha-lib-go):
```go
import "github.com/altcha-org/altcha-lib-go"

func GenerateChallenge(hmacKey string) (altcha.Challenge, error) {
    expires := time.Now().Add(120 * time.Second)
    return altcha.CreateChallenge(altcha.ChallengeOptions{
        HMACKey:   hmacKey,
        MaxNumber: 50000,
        Expires:   &expires,
    })
}
```

验证:
```go
func VerifySolution(payload string, hmacKey string) (bool, error) {
    decoded, _ := base64.StdEncoding.DecodeString(payload)
    var data altcha.Payload
    json.Unmarshal(decoded, &data)
    return altcha.VerifySolution(data, hmacKey, true) // true = 检查过期
}
```

## 2. 前端 PoW 求解

⚠️ **关键踩坑 #1: 绝对不要使用 Web Crypto API (crypto.subtle)**

Web Crypto 仅在安全上下文 (HTTPS/localhost) 可用。
管理面板、内网工具通常通过 http://ip:port 访问 → Web Crypto 不可用 → 验证卡死。

**必须使用纯 JavaScript SHA-256 实现。**

⚠️ **关键踩坑 #2: SHA-256 padding 计算**

如果自己实现 SHA-256，padding 长度公式必须是:
```javascript
// ✅ 正确 (FIPS 180-4)
const padLen = (64 - ((msgLen + 9) % 64)) % 64

// ❌ 错误 (某些 AI 会生成这个)
const padLen = ((msgLen + 8) % 64 === 0) ? 0 : (64 - ((msgLen + 8) % 64))
```
+9 = 1字节 0x80 填充位 + 8字节消息长度。用 +8 在某些消息长度下会产生错误哈希。

**推荐做法**: 直接复用经过验证的纯 JS SHA-256 实现，不要让 AI 从头写。

求解代码 (带 UI 线程让步):
```javascript
async function solvePow(salt, target, max) {
    for (let i = 0; i <= max; i++) {
        if (sha256(salt + String(i)) === target) return i
        // 每 1000 次让出 UI 线程，防止页面冻结
        if (i % 1000 === 0) await new Promise(r => setTimeout(r, 0))
    }
    return null
}
```

提交格式:
```javascript
const payload = btoa(JSON.stringify({
    algorithm: "SHA-256",
    challenge: target,
    number: found,
    salt: salt,
    signature: signature
}))
// POST body: { username, password, altcha: payload }
```

## 3. 限速机制

```
尝试次数    等待时间
1          1 秒
2          2 秒
3          4 秒
4          8 秒
5          封锁 (15分钟窗口内)
```

按 IP 追踪，成功登录后清零。每 5 分钟清理过期记录。

## 4. JWT Token

- 算法: HS256
- 有效期: 24 小时
- Claims: { user_id, username, exp, iat, iss }
- 前端: localStorage 存储，axios 拦截器自动附加 Authorization: Bearer {token}
- 401 响应: 自动清除 token 并跳转登录页

## 5. 密码存储

- 算法: bcrypt, cost=10
- 永远不返回密码哈希到前端 (json:"-")

# 避坑清单

| 坑 | 现象 | 原因 | 解决方案 |
|----|------|------|----------|
| Web Crypto 不可用 | 用户点验证后转圈 5 分钟无反应 | HTTP 上下文无 crypto.subtle | 用纯 JS SHA-256，不依赖 Web Crypto |
| SHA-256 哈希偶尔不对 | 某些 salt+nonce 组合验证失败 | padding 公式 +8 应为 +9 | 用 `(64 - ((len+9)%64))%64` |
| 页面冻结 | 求解时页面无响应 | 主线程被阻塞 | 每 1000 次迭代 `await setTimeout(0)` |
| Altcha Widget 包 | npm 包依赖 Web Crypto | 封装层调用 crypto.subtle | 不用 npm 包，自己写 PowCaptcha 组件 |
| 挑战过期 | 用户放置太久再点登录 | 2 分钟有效期 | 过期后自动重新获取挑战 |
| base64 编码 | 服务端解码失败 | btoa 不支持 Unicode | payload 只含 ASCII，无需担心；但如果 salt 含非 ASCII 需要额外处理 |

# 实现要求

1. 后端:
   - GET /api/auth/challenge → 返回挑战 JSON
   - POST /api/auth/login → 验证 PoW + 密码，返回 JWT
   - 限速中间件 (按 IP)
   - 根据项目的技术栈选择合适的 Altcha 库或自行实现 HMAC 验证

2. 前端:
   - PowCaptcha 组件: 显示"点击验证" → 求解中动画 → 验证完成 ✓
   - 纯 JS SHA-256 (不依赖 Web Crypto)
   - 求解过程不阻塞 UI
   - 登录表单集成 PoW 组件

3. 安全:
   - HMAC 密钥仅服务端持有
   - 每个挑战一次性使用
   - bcrypt 存储密码
   - JWT secret 足够强 (32+ 字符随机)

