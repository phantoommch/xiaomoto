# 小moto安全驾驶指南 · 开发文档

> 详细中文开发说明。简要介绍见仓库根目录的 [README.md](../README.md)。文中的 `example.invalid`、`example.com`、`YOUR_APPLE_TEAM_ID` 都是占位符。

给摩托车骑手用的禁摩收费站、禁摩路段和抓拍点提醒。点位存在本机，骑行时按距离语音、震动并推送本地通知。可选接一个很小的社区后端，用来收集和审核用户上报的点。

主屏幕名称和包内的 `CFBundleDisplayName`、`CFBundleName` 都是「小moto安全驾驶指南」（moto 为小写）。这个名字比较长，iOS 主屏幕标签经常会被截成前几个字，设置里看到的仍是全称。Xcode 目标、scheme 和 Swift 模块仍叫 `Motocam`，`PRODUCT_NAME` 也保持 `$(TARGET_NAME)`，产物是 `Motocam.app`，测试宿主路径不会因为中文文件名断开。Bundle ID 仍是 `com.example.xiaomoto`。显示名通过 `INFOPLIST_KEY_CFBundleDisplayName`、`INFOPLIST_KEY_CFBundleName` 和 `Motocam/Info.plist` 写入。

界面是中文。iOS 17+ 用 SwiftUI 和系统框架（MapKit、Core Location、Vision、AVFoundation、User Notifications）。Android 在 `android/`，Kotlin + Jetpack Compose，最低 Android 10（API 29），面向没有 Google Play 服务的华为 nova 5i Pro（HarmonyOS 3.0）和荣耀 MagicOS。后端是 Go + SQLite，适合腾讯云轻量应用服务器或任意一台能跑容器的机器。iOS 与 Android 读写同一份 JSON / GPX，见下面「数据格式」，不要另起一套字段。

这个应用提醒你**不要驶入**已标记的禁行点，不能代替路边标志和交警指挥。点位可能过时或标错，以现场标志为准。

## 仓库里有什么

| 路径 | 作用 |
| --- | --- |
| `Motocam.xcodeproj` | Xcode 工程，scheme 名 `Motocam` |
| `Motocam/` | App 与可单测的核心逻辑（`Motocam/Core`） |
| `MotocamTests/` | 坐标、搜索排序、罚单字段、JSON/GPX、提醒距离的单元测试 |
| `Package.swift` | 只为了在 Mac 或 Linux 上跑 `swift test`，不是 App 的依赖方式 |
| `Config/` | 签名团队、高德 Key、社区地址的 xcconfig |
| `server/` | 社区上报服务 |
| `docker-compose.yml` | 在仓库根目录启动后端 |
| `android/` | Android 工程，包名 `com.example.xiaomoto` |

## 坐标约定

库存 **WGS-84**。这是 Core Location 给出的坐标，也是 JSON / GPX 的默认坐标。

- 高德 Web 服务返回的是 GCJ-02（`location` 字段为 `经度,纬度`）。保存前转成 WGS-84。
- 在中国大陆，iOS 的 MapKit 底图按 GCJ-02 显示。钉点和相机中心用 GCJ-02，避免和底图错开。
- Android 底图是高德 3D 地图，坐标系是 GCJ-02。钉点和定位圆点先把库存的 WGS-84 转成 GCJ-02 再画上去。地图不提供长按添加。
- iOS 长按地图得到的坐标按 MapKit 的坐标系理解，再转回 WGS-84 入库。Android 不能从地图长按得到坐标。
- 距离计算用 WGS-84。iOS 蓝色圆点交给系统。Android 的 GPS 来自 `LocationManager`，叠到高德地图前会先转成 GCJ-02。

转换用公开的近似公式（长半轴 6378245，偏心率平方 0.00669342162296594323）。中国大陆范围外（经度 72.004–137.8347、纬度 0.8293–55.8271 的矩形之外）原样返回。香港、澳门、台湾落在这个矩形里，会被错误平移，不要把境外点当精确坐标用。

JSON 可以声明 `"coord": "gcj02"`，导入时会转成 WGS-84。GPX 用扩展字段 `motocam:coord`，缺省也是 `wgs84`。

## 在 Mac 上打开工程

需要 Xcode 15 或更新（iOS 17 SDK）。

没有高德 Key 也可以编译。`Config/Debug.xcconfig` 和 `Config/Release.xcconfig` 用 `#include? "Secrets.xcconfig"`，文件不存在时不会让 `xcodebuild` 失败。要搜索地点时再复制示例并填 Key：

```bash
cp Config/Secrets.example.xcconfig Config/Secrets.xcconfig
open Motocam.xcodeproj
```

编译：

```bash
xcodebuild -project Motocam.xcodeproj -scheme Motocam \
  -destination 'generic/platform=iOS Simulator' build
```

跑单元测试（需要模拟器运行时，不能用 generic 目标）：

```bash
xcodebuild -project Motocam.xcodeproj -scheme Motocam \
  -destination 'platform=iOS Simulator,name=iPhone 16' test
```

没有开发者证书、只想在模拟器上确认能编过时，可以加 `CODE_SIGNING_ALLOWED=NO`。真机和上架仍用工程里的自动签名。

### 装到 iPhone

用数据线连上手机，在手机上信任这台 Mac，并打开开发者模式。查 UDID：

```bash
xcrun xctrace list devices
```

在仓库根目录编译（把 `<udid>` 换成上面的标识）。`-derivedDataPath` 把产物固定在 DerivedData 下面，避免 Xcode 自己加一串哈希：

```bash
xcodebuild -project Motocam.xcodeproj -scheme Motocam -configuration Debug \
  -destination 'id=<udid>' -allowProvisioningUpdates \
  -derivedDataPath "$HOME/Library/Developer/Xcode/DerivedData/Motocam" \
  DEVELOPMENT_TEAM=YOUR_APPLE_TEAM_ID build
```

编出来的 App：

```text
~/Library/Developer/Xcode/DerivedData/Motocam/Build/Products/Debug-iphoneos/Motocam.app
```

装到手机：

```bash
xcrun devicectl device install app --device <udid> \
  "$HOME/Library/Developer/Xcode/DerivedData/Motocam/Build/Products/Debug-iphoneos/Motocam.app"
```

也可以在 Xcode 里选这台 iPhone，直接 Run。签名是 Automatic，`DEVELOPMENT_TEAM` 来自 `Config/Shared.xcconfig`（命令行上的同名参数会盖掉它）。工程没有单独的 entitlements 文件：后台定位和语音只写在 `Info.plist` 的 `UIBackgroundModes`，没有推送、Associated Domains 这类要付费团队的能力。选罚单截图用的是系统 PhotosPicker，不申请整个相册权限，所以没有 `NSPhotoLibraryUsageDescription`。

要导出 ad-hoc 安装包时，先把 `ExportOptions.plist` 里的 `teamID` 换成你自己的 Team ID（仓库里是占位符 `YOUR_APPLE_TEAM_ID`）。某些 Xcode beta 在 Release 归档时编译器会崩溃，可以改用 Debug 归档再导出：

```bash
xcodebuild -project Motocam.xcodeproj -scheme Motocam -configuration Debug \
  -archivePath build/Motocam.xcarchive archive
xcodebuild -exportArchive \
  -archivePath build/Motocam.xcarchive \
  -exportPath build/ipa \
  -exportOptionsPlist ExportOptions.plist
```

不装 Xcode、只验证核心逻辑：

```bash
swift test
```

`Package.swift` 把 `Motocam/Core` 编成 `MotocamCore`。App 目标里同一份源文件属于模块 `Motocam`，测试用 `#if canImport(MotocamCore)` 切换。

### 签名团队

仓库里 `Config/Shared.xcconfig` 的 `DEVELOPMENT_TEAM` 留空。把你自己的 Team ID 写进 `Config/Secrets.xcconfig`（已被 git 忽略）：

```
DEVELOPMENT_TEAM = YOUR_APPLE_TEAM_ID
```

也可以在命令行传 `DEVELOPMENT_TEAM=...`。工程文件本身不写 Team ID。Bundle ID 是 `com.example.xiaomoto`，自己发布时请换成你自己的 Bundle ID。

免费个人证书大约 7 天过期，后台定位在真机上会跟着失效，需要重新安装。

### 高德 Key

1. 打开[高德开放平台](https://lbs.amap.com/)控制台，创建 **Web 服务** Key。不要用 iOS SDK Key，地点搜索走的是 REST `https://restapi.amap.com/v3/place/text`。
2. 复制 `Config/Secrets.example.xcconfig` 为 `Config/Secrets.xcconfig`（已被 git 忽略），填入 `AMAP_WEB_KEY`。
3. 工程通过 `Info.plist` 的 `AMapWebKey` 读这个值。设置页不再填写 Key。

手机直接请求高德。Key 如果开了 IP 白名单，必须放行手机当时的出口 IP，否则会返回 `INVALID_USER_IP`。日配额用尽是 `DAILY_QUERY_OVER_LIMIT`。不要把真实 Key 提交到 git。

不填 Key 时，地图、手动标点、导入导出、骑行提醒仍然可用，只是不能在线搜索。

### 后台定位要怎么授权

骑行提醒需要「始终」定位。`Info.plist` 的 `UIBackgroundModes` 有 `location` 和 `audio`：锁屏后继续算距离，语音只在一条提醒播出时打开音频会话，念完就关掉。系统弹出的说明就是这两段：

- 使用期间：小moto安全驾驶指南用你的位置在地图上标出当前位置，并计算和已记录的禁摩收费站、禁摩路段、抓拍点之间的距离。
- 始终：小moto安全驾驶指南在骑行时即使锁屏或切换到其他 App，也需要持续定位，以便在约 2 公里和 500 米处发出语音和通知。定位只在本机用来判断距离，不会上传行驶轨迹。

通知没有单独的 Info.plist 说明。开始骑行时会请求通知权限，包括时效性通知。提醒标题是点位名称，正文是要念的那一句，下面有「我知道了」和「不再提醒」。首次打开会有一页引导，标题是「小moto安全驾驶指南」。

请在系统设置里把定位改成「始终」。只给「使用期间」时，App 仍会在前台更新位置并提示你去打开始终定位，但锁屏后不会继续提醒。

实现上：`desiredAccuracy` 为十米级，`distanceFilter` 50 米，活动类型为车载导航，并同时开显著位置变化，用来在被系统挂起后唤醒。靠近时外圈和内圈各提醒一次；离开外圈大约 300 米之后，下次再靠近还会提醒。已经开过去的点不提醒。点「不再提醒」的点位不再出声，设置里可以全部恢复。语音用 `AVSpeechSynthesizer`，语言 `zh-CN`，并压低其他音频。会话类别是 `playback` / `spokenAudio`，只在念这一句时 `setActive(true)`，念完、被打断或结束骑行时关掉。锁屏通知是时效性通知，标题是点位名称，下面有「我知道了」和「不再提醒」。App 被系统为了定位重新拉起时，会按上次的骑行开关恢复监听。

## Android（华为 nova 5i Pro / 荣耀）

包名 `com.example.xiaomoto`，桌面名称「小moto安全驾驶指南」。`minSdk` 29，`targetSdk` / `compileSdk` 36。定位用系统 `LocationManager` 的 GPS，不用 Fused Location。没有 Google Maps，也没有 Firebase 登录或推送。

### 高德地图

底图是高德 3D 地图 SDK（`com.amap.api:3dmap:10.0.600`）。路网是 GCJ-02。App 把库存的 WGS-84 转成 GCJ-02 再钉到图上。定位圆点用系统 `LocationManager` 的 GPS（没有卫星时才用网络定位，并提示精度较差），不用高德定位 SDK，也不用 Fused Location。地图右上角附近有「当前位置」，把镜头移到这个点。

地图**不能长按添加**。违法截图识别成功后会标到地图上，并列出名称、道路、违法行为和罚款记分。核对后点「上传」才提交。只有高德没有结果，或同名结果分不开时，才点一下地图，然后再核对、上传。

高德 Android 地图要单独申请 **Android 平台** Key，绑定包名 `com.example.xiaomoto` 和签名 SHA1。debug 签名和以后的正式签名 SHA1 不同，Key 没对上时底图鉴权失败。这个 Key 和地点搜索用的 **Web 服务** Key 不是同一个，不要混填。

debug SHA1：

```bash
keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android
```

Debug 包固定用仓库里的 `~/.android/debug.keystore`（标准 Android debug 证书，口令 `android`）。换一把证书就装不上覆盖更新。

Key 写在 gitignore 的 `android/secrets.properties`（`AMAP_ANDROID_KEY` 管地图，`AMAP_WEB_KEY` 管搜索），不会进 git，设置页也不再填写：

```bash
cp android/secrets.properties.example android/secrets.properties
```

查询清洗、公路编号加减分、JSON / GPX 字段和 iOS 同一套，单测用例也对齐。

### 编译和安装

需要 JDK 17 和 Android SDK 36（Android Studio 打开 `android/` 时会提示安装）。

```bash
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

手机先打开「开发者选项」和「USB 调试」。`adb devices` 能看到设备后再装。

HarmonyOS 3 / MagicOS 的**纯净模式**会拦截侧载 APK。安装被拦时，点开风险提示里的详情，选「仍要安装」；或到 **设置 → 系统和更新 → 纯净模式** 关掉后再装。nova 5i Pro 上也有人从 **设置 → 安全 → 更多安全设置 → 纯净模式** 进去。

### 权限（否则锁屏后没有提醒）

1. 定位选 **始终允许**。系统弹窗如果只有「仅使用期间允许」，再到 **设置 → 应用和服务 → 权限 → 位置信息 → 小moto安全驾驶指南 → 始终允许**。只给前台权限时，锁屏后不再算距离。
2. 允许通知。2 公里和 500 米各有一条通知，骑行中还有一条常驻通知。
3. 关闭电池优化。App 里「骑行」页可以请求加入白名单。
4. **手机管家 → 应用启动管理**（荣耀上可能仍叫启动管理）找到本应用，关掉自动管理，打开 **允许自启动、允许关联启动、允许后台活动**。骑行页会尝试打开这个页面；打不开时按这句手动进。
5. 开始骑行后再划掉界面，前台服务会继续。厂商仍可能杀掉它，所以上面几项都要开。

很多华为、荣耀出厂没有中文语音引擎。App 会检查 `TextToSpeech` 是否支持简体中文。没有的话，骑行页会说明原因，提醒改成提示音（安装包里的短音频）加上通知里的整句文字。可在系统里安装中文语音数据后再开始一次骑行。

罚单识别用 ML Kit 中文文字识别的**随包模型**（`com.google.mlkit:text-recognition-chinese`，模型在 APK 的 `assets/mlkit-google-ocr-models`）。公开的 `TextRecognition.getClient` 在没有 Google 服务的手机上会去下载可选模块，所以这里直接调用安装包里的 `BundledTextRecognizerCreator`。相册选择用系统的 `ACTION_GET_CONTENT`，图片只打开一次，并按 EXIF 转正。界面只写一句：只识别交警12123违法截图。其他图片不予识别。识别成功后按违法地点在高德里自动定位并标到地图上，列出结果供核对，点「上传」才提交。一张图里的每条记录都会处理。高德完全找不到时，截图和地点文字会交给后台核对，不用在地图上点。

## 怎么用

1. **地图**。四种点用不同颜色：禁摩收费站、禁摩路段、抓拍摄像头、其他。社区点是另一种样式（针上有「社」），和本机点相距约 80 米以内时只显示本机点。点进详情可以看到名称、道路编号、备注、罚款、记分和来源（手动 / 搜索 / 截图识别 / 社区）。用户上报并公开的点会另外标「用户上传」，出现在提醒标题、锁屏通知和点位详情里。
2. **添加点**。iOS 可以长按地图，再补名称和类型。Android 禁止长按添加。违法截图会在高德里自动定位并标点，核对后上传。高德没找到时，截图直接交给后台，不用在地图上点。
3. **搜索**。编译进工程的高德 Web 服务 Key 用来按名称搜收费站。罚单里的地点会先去掉「进口 / 出口 / 内广场 / 外广场」一类广场用语，抽出公路编号（如 `G25`）和「长深高速」这类路名，再用剩下的关键词去搜。结果按路号、路名、城市或省份、收费站类型排序。例如搜「赤石收费站」时，高德可能把广东 G15 深汕的赤石站排在前面；带上 `G25` 或「长深高速」后，G25 会压过 G15。城市只作偏好，不锁死在该城市。手动搜索仍要点一下候选才入库。违法截图会自动取排序后最匹配的一条，先标在地图上。
4. **截图识别**。只识别交警 12123 违法截图，其他图片不予识别。识别出地点后，先按省市在高德搜隧道、收费站、服务区、互通、桥这类设施，不行再做地理编码。定位成功就标在地图上，核对卡片写「高德匹配：地名」，确认后点「上传」。上传会带上原始地点文字和高德匹配名。高德完全找不到时提示「高德没找到这个地点，已保存待后台核对」，截图和文字照样提交，坐标留空。号牌不入库。上传失败先存本机并排队，有网再补传。首页「更新」、启动时的自动更新都拉取全国已公开点位，不看当前城市或地图范围。成功后会写明点数，例如「已更新，共 3 个点位」。地图缩到能看见全中国时，这些点的标记仍然显示。
5. **骑行**。打开骑行模式后按距离提醒。设置里只改提醒距离。靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。点「不再提醒」的点位不再出声。
6. **导入 / 导出**。代码里仍能读写 JSON 和 GPX，设置页不再提供入口。按点的 `id` 合并，同 id 以导入的为准。
7. **社区**。服务器地址从编译配置读取：iOS 用 `Config/Shared.xcconfig` 的 `COMMUNITY_API_BASE_URL`，Android 用 `gradle.properties` 或 `-PCOMMUNITY_API_BASE_URL=`。当前 Android 默认是 `https://example.invalid`（域名确认后只改这一处，以及服务器的 `PUBLIC_BASE_URL`）。iOS 只用这个编译进去的地址，设置里不能改。Android 设置页长按标题约 3 秒可以改地址，平时看不到。违法截图核对后再上传位置。详情里仍可以只传位置，或先在截图上拖一个矩形盖住号牌再传。客户端会压成 JPEG 并去掉 EXIF；服务端会再解码重编码一次。默认一条上报就会公开。
8. **账号**。Android 打开就要注册或登录，邮箱收 6 位激活码，勾选《用户协议与免责声明》后才能进入地图。iPhone 按 App Store 5.1.1 不在启动时强制登录：没账号也能看地图，并用手机上已经有的点位提醒。点「更新」、自动同步点位，以及上传截图或上报，都要先登录；没登录时点「更新」或上传，会看到「注册登录后才能更新违法点数据」，不会在打开 App 时弹出。协议正文从服务器拉，本机有一份备用。协议版本变了要再同意一次。设置里可以退出登录，也可以注销账号。登录凭证 Android 放在 Keystore 加密的偏好里，iPhone 放在钥匙串。请求带 `Authorization: Bearer` 和 `X-App-Build`。
9. **检查更新**。Android 设置页有一行「检查更新」。已登录时，启动后每天最多自动查一次。有新版本时提示「发现新版本 x.y」，可以「立即更新」或「以后再说」；强制更新没有「以后再说」。Android 在应用里下载、核对 sha256，再交给系统安装（华为和荣耀会弹出自己的安装确认）。iOS 不在应用里下载或安装更新，也不每天向服务器查版本；设置页只显示当前版本。`AppUpdateLink.appStoreID` 为空时不出现「检查更新」。填上 App Store 数字 id 之后，这一行只打开 App Store 页面。Android 的 `versionCode`：`-PVERSION_CODE=` 优先于 `git rev-list --count HEAD`。这一包是 32，`versionName` 是 1.0。iOS 的 `CFBundleVersion` 是 33。没有这个字段的旧安装包仍能拉点。

## 数据格式

iOS 和 Android 共用这一份。字段名、`type` / `source` 取值、GPX 命名空间都不要分叉。

导出的 JSON：

```json
{
  "version": 1,
  "coord": "wgs84",
  "points": [
    {
      "id": "……",
      "name": "赤石收费站",
      "type": "banned_toll",
      "road_code": "G25",
      "road_name": "长深高速",
      "notes": "",
      "latitude": 22.8,
      "longitude": 115.0,
      "fine_yuan": 100,
      "penalty_points": 1,
      "violation": "驾驶机动车违反禁令标志指示的",
      "source": "ocr",
      "province": "广东",
      "city": "河源",
      "created_at": "2026-01-15T06:20:00Z",
      "updated_at": "2026-01-15T06:20:00Z",
      "is_community": false
    }
  ]
}
```

`type`：`banned_toll` 禁摩收费站，`banned_segment` 禁摩路段，`camera` 抓拍摄像头，`other` 其他。

本机 `source`：`manual` 手动，`search` 搜索，`ocr` 截图识别，`community` 社区。公开点另外有可选字段 `source`：`user_upload` 表示这条来自用户上报，只有这个值会在提醒、锁屏通知和详情里显示「用户上传」。`import` 表示 `server/import_points.py` 导入的点，不显示这个标签。客户端把公开的 `source` 记在 `upload_source`，不写进本机的 `source` 枚举，所以旧版本忽略它也能解码。

`latitude` / `longitude` 在 `coord` 为 `wgs84` 时是 WGS-84。`coord` 还可以是 `gcj02`。

GPX 1.1，`wpt` 的 lat/lon 默认 WGS-84，扩展命名空间 `urn:motocam:gpx:1`，字段有 `motocam:id`、`type`、`source`、`roadCode`、`roadName`、`violation`、`fineYuan`、`penaltyPoints`、`province`、`city`、`coord`。

iOS 本机文件在 Application Support：`points.json` 与 `community.json`。Android 写在应用私有目录 `files/xiaomoto/`，文件名相同。靠近时每个圈提醒一次，离开外圈大约 300 米后重新计数。这段记录和「不再提醒」的点位都写在本机：iOS 在 UserDefaults，Android 在 `xiaomoto` 的 SharedPreferences，结束骑行也不会清掉。

测试夹具 `MotocamTests/Fixtures/ticket_chishi.txt` 是识别后的文字，不是图片。字段是一张示例通知：地点「G25长深高速赤石收费站进口内广场」，行为「驾驶机动车违反禁令标志指示的」，罚款 100 元，记 1 分，时间 2026-01-15 14:20（北京时间，即 `2026-01-15T06:20:00Z`）。仓库里没有号牌。

## 后端

`server/` 是一个进程：SQLite（WAL）+ 本地图片目录。默认监听 `:8787`。

```bash
cd server
go test ./...
go run ./cmd/motocam
```

或在仓库根目录：

```bash
cp server/.env.example server/.env   # 改掉 ADMIN_TOKEN
docker compose up --build
```

数据卷 `motocam-data` 挂到容器 `/data`。健康检查是 `GET /healthz`。`ADMIN_TOKEN` 默认为空，这时所有管理接口返回 401。要用管理接口时设置一个长随机串（例如 `openssl rand -hex 32`）。

### 接口

坐标一律 WGS-84。设备用请求头 `X-Device-Id`（UUID）。

登录用邮箱激活码，不设密码。`users`、`email_codes`、`sessions`、`auth_limits` 在进程启动时自动建表（`CREATE TABLE IF NOT EXISTS`，旧库补 `reports.user_id`）。协议文件是 `DATA_DIR/legal/terms.md`，没有就写入程序里的文本；改这个文件即可换文案，不用发新版 App。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| POST | `/v1/auth/request-code` | 正文 `{"email":"..."}`。同一邮箱 60 秒 1 次、1 小时 10 次；同一 IP 1 分钟 5 次、1 小时 30 次。激活码 10 分钟、最多错 5 次。成功只返回 `{"ok":true}`，不返回验证码 |
| POST | `/v1/auth/verify` | 正文 `{"email","code","termsVersion"}`。`termsVersion` 必须等于当前协议版本。新邮箱会建账号。返回 `token`（约 180 天）、`expiresAt`、`user` |
| POST | `/v1/auth/logout` | 作废当前 token |
| GET | `/v1/me` | 当前账号，含 `termsAccepted` |
| DELETE | `/v1/me` | 注销账号和 token。已公开的点保留，关联上报的 `device_id` 改成 `deleted` |
| GET | `/v1/legal/terms` | `{version,title,markdown,updatedAt}` |
| POST | `/v1/legal/accept` | 正文 `{"version"}`，已登录用户补记同意 |

`Authorization: Bearer <token>`。`X-App-Build` 是客户端版本号。没带头或小于 30 的旧安装包，拉点和检查更新仍放行。带了无效 token 一律 401。`POST /v1/reports` 从这一版起必须有有效 token。`X-App-Build` 大于等于 30 时，`GET /v1/points` 和 `GET /v1/app/latest`、安装包下载也要有效 token。iOS 系统自己去拉的 `manifest.plist` 和 IPA 不带头，仍放行，避免装不上。停用账号返回 403 `账号已停用`。

邮件走网易 163：`SMTP_HOST=smtp.example.com`、`SMTP_PORT=465`、`SMTP_TLS=ssl`、`SMTP_USER` 和 `SMTP_FROM` 都是 `you@example.com`。`SMTP_PASS` 是 163 的 SMTP 授权码，只放在服务器的 `server.env`，不要写进仓库。主题是「小moto安全驾驶指南 激活码」。没配 SMTP 且 `AUTH_DEV` 不是 `1` 时，获取激活码返回 503，`smtpConfigured` 为 false。`AUTH_DEV=1` 只把激活码写进服务日志，公网不要开。`REVIEW_EMAIL` 和 `REVIEW_CODE` 给审核用：这个邮箱不发信、不占频率限制，验证时仍要同意当前协议。不要在界面上写成审核账号。`TERMS_VERSION` 可选，用来覆盖文件里的版本号。

`POST /v1/reports`  
JSON，或 `multipart/form-data`（图片字段名 `image`，仅 JPEG/PNG，默认最大 5 MB）。字段：`name`、`type`、`road_code`、`road_name`、`notes`、`latitude`、`longitude`、`fine_yuan`、`penalty_points`、`violation`，以及可选的 `ocr_location`（罚单上的原始地点）、`amap_name`（高德匹配到的名字）。`located` 为 false，或表单里 `located=0` 且不带坐标时，表示高德没定位到，只入库待审，不会公开成地图点。旧客户端仍然只发坐标，不带这些字段，行为不变。成功 `201`，`status` 为 `pending` 或已公开时带 `public_point_id`。必须带用户 token。带了罚单文字、桩号，或置信度是 low / none 的上报会先待审：服务端用道路编号和公里桩、高德地理编码或驾车路线、以及 OSM 隧道几何互相核对。几处结果在约 150 米内一致才自动公开，并在点上写入 `source=user_upload`。隧道洞口要驾车路线和 OSM 洞口都对上才算核对通过；对不上、超时或队列满时保留上报坐标，`review_reason` 写明原因。没有罚单文字、也没有桩号的手动精确点仍按原逻辑立即公开。

`GET /v1/points?min_lat&min_lon&max_lat&max_lon&since&limit`  
不带范围时返回全部公开点。四个范围参数要一起给才会按矩形过滤，坐标是 WGS-84。`since` 为 RFC3339。`limit` 缺省或大于 100000 时按 100000 返回。更早的服务端单次最多 5000 条；客户端请求 `limit=5000`，若本页满 5000 条，再用最后一条的 `updated_at` 作为 `since` 继续取，直到拿全。构建号大于等于 30 时要带用户 token。响应：

```json
{ "server_time": "…", "points": [ { "coord": "wgs84" } ], "removed_ids": [] }
```

管理接口需要 `Authorization: Bearer <ADMIN_TOKEN>`：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/v1/admin/reports?status=pending` | 待审列表，也可查 `merged` / `rejected` / `archived`。含 `review_result` 和 `review_reason` |
| GET | `/v1/admin/points` | 已公开点，含 `source`、`review_result`、`review_reason` |
| GET | `/v1/admin/reports/{id}/image` | 审核用 JPEG |
| POST | `/v1/admin/reports/{id}/approve` | 通过并公开 |
| POST | `/v1/admin/reports/{id}/reject` | 拒绝 |
| POST | `/v1/admin/reports/merge` | 正文 `{"report_ids":["…"]}`，合并成一个公开点 |
| DELETE | `/v1/admin/points/{id}` | 下线公开点（写入删除记录，关联上报归档） |
| POST | `/v1/admin/app` | 发布 Android APK 或 iOS IPA，见下面的 curl |

发布安装包（把 `versionCode` 换成这次包里的版本号，APK 用 `platform=android`，IPA 用 `platform=ios`）：

```bash
curl -X POST https://example.invalid/v1/admin/app \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -F platform=android \
  -F versionCode=28 \
  -F versionName=1.0 \
  -F notes='应用内更新' \
  -F force=false \
  -F file=@app-debug.apk
```

`force` 为 `1`、`true` 或 `yes` 时，客户端不提供「以后再说」。文件存在 `DATA_DIR/app/<platform>/`，每个平台只保留最近 3 个版本。`GET /v1/app/latest` 返回 `versionCode`、`versionName`、`notes`、`url`、`sha256`、`size`、`force`。下载的 Content-Type：APK 是 `application/vnd.android.package-archive`，IPA 是 `application/octet-stream`。iOS 描述文件里的包名是 `com.example.xiaomoto`，标题是「小moto」。绝对地址来自环境变量 `PUBLIC_BASE_URL`（不要带末尾斜杠或路径）。没设时才退回请求自己的 Host，真机安装请务必设置。

同一设备默认每小时 200 次、每天 1000 次。罚款金额限制在 0–20000，记分 0–12。

自动公开：默认 **1 条上报**、同一类型、约 **200 米**内、道路编号不冲突，就取质心建成公开点。交警 12123 截图核对后上传的点因此会直接公开。空的道路编号可以和一条已知路号合并；如果附近同时有 G25 和 G15，空路号的上报不会把两条高速粘成一个点。同一设备重复上报不增加计数。被拒绝或已归档的上报不计数。`PROMOTE_MIN_REPORTS` 小于 1 时会抬到 1。决定做出之后，服务器删掉对应图片。

### 部署到轻量服务器

常驻机器是一台 Linux 服务器。进程听 `127.0.0.1:8787`，Caddy 做 HTTPS 反代，数据目录 `DATA_DIR=/opt/xiaomoto-app/data`。示例单元文件和 Caddy 配置在 `server/deploy/`。`server.env` 至少要有 `ADMIN_TOKEN`、`DATA_DIR=/opt/xiaomoto-app/data`、`ADDR=127.0.0.1:8787`、`PUBLIC_BASE_URL=https://example.invalid`。从这一版起还要配 SMTP：`SMTP_HOST=smtp.example.com`、`SMTP_PORT=465`、`SMTP_TLS=ssl`、`SMTP_USER=you@example.com`、`SMTP_FROM=you@example.com`。`SMTP_PASS` 填 163 的授权码，只放在 `server.env`。审核账号用 `REVIEW_EMAIL` 和 `REVIEW_CODE`（都不填就没有审核账号）。公开页面 `/privacy`、`/terms`、`/support` 上的联系邮箱用 `CONTACT_EMAIL`。表会在启动时自己补上，不用手工迁移。来源校正只在第一次启动时做：上报全部来自 `import_points.py` 那个固定设备号的点写成 `source=import`（包括以前被标成 `user_upload` 的），已经写成其他非空来源的不动；至少有一条别的设备上报的仍是 `user_upload`。之后再启动不会把清空的 `source` 填回去。协议在 `/opt/xiaomoto-app/data/legal/terms.md`，第一次启动会写入 1.0 文本，之后改这个文件即可。域名确认后只改 `PUBLIC_BASE_URL` 和 App 里的同一处编译配置。

1. 安装 Docker。国内机器拉 `golang` / `debian` 镜像可能要配镜像加速。
2. 在仓库根目录设置 `ADMIN_TOKEN`，执行 `docker compose up -d --build`。
3. 前面加 Nginx 或 Caddy，只暴露 HTTPS。iOS 不再设置 `NSAllowsArbitraryLoads`，只走系统默认的 HTTPS。
4. 把服务器根地址写进 Android 的 `COMMUNITY_API_BASE_URL`，或 iOS 的 `Config/Shared.xcconfig`。不要带路径。iOS 设置里不能改这个地址。Android 设置页长按标题约 3 秒可以改。这个地址要和 `PUBLIC_BASE_URL` 相同，点位下载用它。Android 检查更新也用它。

在中国大陆**对公众**提供用户上传服务，需要 ICP 备案，以及和主体匹配的许可。个人开发者可以先把服务放在只有自己能访问的机器上试用，不一定先备案。备案完成前不要把地址写进公开发布的安装包。

图片存在 SQLite 旁边的本地目录，没有接 COS。要换对象存储时，替换 `server/internal/store` 里写文件和删文件的部分即可。

## App 备案与上架

在中国大陆 App Store 上架，除了 Apple 的开发者账号，还需要 [App 备案](https://developer.apple.com/cn/support/app-store-review/app-filing/)。备案主体、域名和后端如果要对用户开放，要和上面的 ICP 备案一起考虑。免费 Personal Team 不能用来提交 App Store。

隐私清单 `Motocam/PrivacyInfo.xcprivacy` 声明了粗略定位、设备标识（社区上报的 `X-Device-Id`）、相册读取和 UserDefaults。选照片用系统 PhotosPicker，没有额外的相册用途字符串。`ITSAppUsesNonExemptEncryption` 为 false。

## 已知限制

- 本仓库在 Linux 上用 Swift 6 跑过 `swift test`，用 Go 跑过 `go test ./...`，并用 `./gradlew :app:testDebugUnitTest :app:assembleDebug` 编过 Android debug 包。这里没有 Xcode，**没有**执行 `xcodebuild`。也没有华为或荣耀真机，**没有**在无 Google 服务的手机上点过地图、OCR、语音和前台服务。
- Android 底图依赖高德 Android 平台 Key。Key、包名或 SHA1 不一致时地图鉴权失败。这里没有华为或荣耀真机，没有在真机上打开过高德底图。
- GCJ 矩形包含港澳台，境外点会被误偏。
- 高德 Web 服务 Key 和 iOS SDK Key 不是同一种；IP 白名单会导致手机直连失败。
- 后台语音可能不响，通知是兜底。
- 自动公开可以被多个伪造的 `X-Device-Id` 凑数，所以默认仍建议人工看一眼图片；管理接口没有第二因素。
- 号牌遮挡靠上传前手动画框，服务端只保证去掉 EXIF 并重编码，不会自动找号牌。
- 个人免费证书约 7 天失效。
- 搜索排序是启发式的，仍要人确认候选，尤其是同名收费站。
