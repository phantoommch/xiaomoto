# 小moto安全驾驶指南 · Xiaomoto Safe Riding Guide

[中文](#中文) · [English](#english) · License: [MIT](LICENSE)

---

## 中文

给中国摩托车骑手用的安全提醒 App。地图上标出**禁摩收费站、禁摩路段和抓拍点**；开始「骑行」后，靠近这些点时用语音、震动和锁屏通知提醒你**不要驶入**。它不是导航，不能代替路边标志和交警指挥，点位可能过时，以现场为准。

### 仓库结构

| 路径 | 内容 |
| --- | --- |
| `Motocam/`、`Motocam.xcodeproj` | iOS App（Swift / SwiftUI，iOS 17+），核心逻辑在 `Motocam/Core` |
| `MotocamTests/`、`Package.swift` | iOS 单元测试；`swift test` 可在 Mac/Linux 上跑核心逻辑 |
| `Config/` | iOS 的 xcconfig（签名团队、高德 Key、服务器地址） |
| `android/` | Android App（Kotlin + Jetpack Compose，Android 10+，高德地图，不依赖 Google Play 服务） |
| `server/` | Go 后端（SQLite）：邮箱激活码登录、社区上报与审核、点位同步、`/privacy` `/terms` `/support` 页面 |
| `docker-compose.yml` | 在仓库根目录启动后端 |
| `docs/DEVELOPMENT.zh.md` | 详细中文开发文档（坐标约定、数据格式、接口、部署） |

### 配置密钥（仓库里没有任何真实密钥）

所有 Key、密码、服务器地址都已换成占位符，需要你自己填：

**iOS** — 复制 `Config/Secrets.example.xcconfig` 为 `Config/Secrets.xcconfig`（已 gitignore）：

```
AMAP_WEB_KEY = YOUR_AMAP_WEB_KEY            // 高德「Web 服务」Key，用于地点搜索
DEVELOPMENT_TEAM = YOUR_APPLE_TEAM_ID        // 你的 Apple 签名团队
COMMUNITY_API_BASE_URL = https://api.example.com   // 你的后端地址（可选）
```

**Android** — 复制 `android/secrets.properties.example` 为 `android/secrets.properties`（已 gitignore）：

```
AMAP_WEB_KEY=YOUR_AMAP_WEB_KEY
AMAP_ANDROID_KEY=YOUR_AMAP_ANDROID_KEY       # 高德 Android 平台 Key，绑定包名 + 签名 SHA1
COMMUNITY_API_BASE_URL=https://api.example.com
```

**后端** — 复制 `server/.env.example` 为 `server/.env`，通过环境变量配置：

| 变量 | 作用 |
| --- | --- |
| `ADMIN_TOKEN` | 管理接口令牌，留空则管理接口全部 401 |
| `PUBLIC_BASE_URL` | 公网 HTTPS 根地址，如 `https://api.example.com` |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` / `SMTP_TLS` | 发激活码的邮箱（`SMTP_PASS` 为邮箱授权码） |
| `AMAP_KEY` | 高德 Web 服务 Key，服务器端重定位用 |
| `CONTACT_EMAIL` | 公开页面上显示的联系邮箱 |
| `REVIEW_EMAIL` / `REVIEW_CODE` | 可选：App 审核用的固定账号和激活码，不填就没有 |
| `AUTH_DEV=1` | 仅本地开发：不发信，把激活码打到日志 |

### 编译

```bash
# iOS（需要 Xcode 15+）
xcodebuild -project Motocam.xcodeproj -scheme Motocam \
  -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
swift test                      # 只跑核心逻辑测试

# Android（JDK 17）
cd android && ./gradlew :app:testDebugUnitTest :app:assembleDebug

# 后端（Go 1.22+，需要 cgo 编译 sqlite）
cd server && go test ./... && AUTH_DEV=1 go run ./cmd/motocam
# 或者：docker compose up --build
```

Bundle ID（`com.example.xiaomoto`）和 Android 包名（`com.example.xiaomoto`）请在自己发布时换掉。仓库不含点位数据库、用户上传内容、证书或签名文件。

---

## English

A safety-reminder app for motorcycle riders in mainland China. It marks **toll stations and road sections closed to motorcycles, and enforcement cameras** on a map. In Ride mode it warns you by voice, vibration and lock-screen notification as you approach one, so you **don't ride in**. It is not navigation and does not replace road signs or police instructions; point data can be out of date.

### Layout

| Path | What |
| --- | --- |
| `Motocam/`, `Motocam.xcodeproj` | iOS app (Swift / SwiftUI, iOS 17+); testable core in `Motocam/Core` |
| `MotocamTests/`, `Package.swift` | iOS unit tests; `swift test` runs the core on macOS/Linux |
| `Config/` | iOS xcconfig files (signing team, AMap key, server URL) |
| `android/` | Android app (Kotlin + Jetpack Compose, Android 10+, AMap SDK, no Google Play services needed) |
| `server/` | Go backend (SQLite): email-code login, community reports and moderation, point sync, `/privacy` `/terms` `/support` pages |
| `docker-compose.yml` | Runs the backend from the repo root |
| `docs/DEVELOPMENT.zh.md` | Detailed developer notes (Chinese) |

### Configuring keys (no real secrets are in this repo)

Every key, password and host has been replaced with a placeholder:

- **iOS**: copy `Config/Secrets.example.xcconfig` to `Config/Secrets.xcconfig` (gitignored) and set `AMAP_WEB_KEY`, `DEVELOPMENT_TEAM`, and optionally `COMMUNITY_API_BASE_URL`.
- **Android**: copy `android/secrets.properties.example` to `android/secrets.properties` (gitignored) and set `AMAP_WEB_KEY`, `AMAP_ANDROID_KEY` (bound to your package name + signing SHA-1) and `COMMUNITY_API_BASE_URL`.
- **Server**: copy `server/.env.example` to `server/.env` and set `ADMIN_TOKEN`, `PUBLIC_BASE_URL`, `SMTP_*`, `AMAP_KEY`, `CONTACT_EMAIL`, and optionally `REVIEW_EMAIL`/`REVIEW_CODE` (a fixed demo login for App Review; disabled when empty). `AUTH_DEV=1` logs login codes instead of emailing them (local development only).

### Build

See the commands in the Chinese section above: `xcodebuild` / `swift test` for iOS, `./gradlew :app:assembleDebug` for Android, `go test ./... && go run ./cmd/motocam` or `docker compose up --build` for the server.

Change the bundle ID (`com.example.xiaomoto`) and Android application ID (`com.example.xiaomoto`) before publishing your own build. The repo contains no point database, user uploads, certificates or signing files.

## License

MIT © 2026 xiaomoto contributors
