# FineReport Auto Print Tool

轻量级 Wails 应用，用于自动打开 FineReport 报表页面、等待 `FR` 对象就绪，并执行 `FR.doURLPrint()` 完成处方单打印。

## 当前能力（阶段 2）

- ✅ Go 侧定义打印数据结构（`PrintParams`/`Reportlet` 等）并提供默认参数  
- ✅ 封装打印服务：校验参数、生成请求 ID、向 WebView 注入脚本并同步返回结果  
- ✅ 前端实现 `waitForFR`/`executePrint` 流程：加载远端报表页面、轮询 `FR` 对象并调用 `FR.doURLPrint`
- ✅ 启动内置反向代理（127.0.0.1 随机端口）并注入 WebView2 启动参数，以绕过 X-Frame-Options / 同源限制
- ✅ UI 支持在线编辑打印 JSON、一键恢复默认、查看 FineReport 会话

## 开发与运行

```powershell
# 安装依赖（前端会在 build/dev 时自动安装）
wails dev         # HMR 开发模式，默认 http://localhost:34115
wails build       # 生成 xautoprint.exe（产物位于 build/bin）
```

> ⚠️ Go 端在启动时会写入 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`，关闭 Web 安全策略以便父页面访问远端 iframe。  
> 该设置仅适用于可信内网环境，请勿在公网场景下使用。

### 反向代理（解决 “拒绝连接”）

- 应用启动后会在 `127.0.0.1:<随机端口>` 上开一个反向代理，转发至 `http://172.20.38.62:8080`
- 代理会删除 `X-Frame-Options`/`Content-Security-Policy`，允许在本地 WebView 中嵌入 FineReport 页面
- 前端默认的 `entryUrl`、`printUrl` 会被自动替换成代理地址，无需手动修改
- 如果后端地址有变，可在 `printer.DefaultParams()` 或后续配置中心内调整基础 URL

## 目录结构

```
.
├── app.go                     # 绑定打印服务
├── internal/printer           # 打印领域模型 + Service
├── internal/proxy             # 反向代理 Server
├── frontend/src               # 参数编辑器 & FineReport iframe 驱动
└── wails.json                 # Wails 配置
```

## 常见问题

- 如果 `FR` 对象长时间未出现，前端会抛出 “等待 FineReport 对象超时” 并在 Go 侧返回错误。
- 需要切换打印 URL，可在前端 JSON 中调整 `entryUrl/printUrl` 字段，或在 Go 侧更新 `printer.Config` 默认值。
