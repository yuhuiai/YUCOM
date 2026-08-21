<p align="center">
  <img src="design/YUCOM-App-Icon.png" width="112" alt="YUCOM application icon">
</p>

<h1 align="center">YUCOM</h1>

<p align="center">
  A cross-platform serial port testing tool for Windows and Linux.<br>
  面向 Windows 与 Linux 的跨平台串口测试工具。
</p>

<p align="center">
  <a href="#中文">中文</a> · <a href="#english">English</a>
</p>

---

<a id="中文"></a>

## 中文

YUCOM 用于检查计算机、工控机、主板和外接设备上的串口能否正常识别、发送和接收。它不绑定特定主板、串口芯片或 PCB，也不会替代系统串口驱动。

Windows 版使用内嵌 WebView2 独立窗口，Linux 原生版使用 GTK3 独立窗口。串口数据由本地 YUCOM 进程处理，不会发送到互联网或局域网。

### 功能

- 自动识别板载、USB、PCIe 和多串口扩展设备
- Linux 支持 `/dev/serial/by-id` 稳定设备名称
- 支持 300～921600 波特率、5～8 数据位、1/2 停止位、奇偶校验和流控
- 文本及 HEX 收发、结束符、暂停显示、自动滚动和字节计数
- 标准文本测试帧与 512 字节原始数据硬件回环自检
- 定时发送和 Modem 信号显示
- 不保存配置、接收内容或测试日志
- Windows 与 Linux 原生版均为独立窗口；旧 Linux 网页模式仅监听本机地址

### 支持的平台

| 系统 | 架构 | 界面 |
| --- | --- | --- |
| Windows 10/11 | amd64、arm64 | 内嵌 WebView2 独立窗口 |
| 银河麒麟、Ubuntu、Debian 等 Linux 桌面系统 | amd64、arm64 | GTK3 原生窗口 |

> [!IMPORTANT]
> YUCOM 只负责测试串口，不负责安装硬件驱动。运行前请确认操作系统已经正确识别串口设备。

### 获取 YUCOM

当前仓库尚未发布预编译的 GitHub Release。请从源码构建；以后发布的可直接运行版本将出现在 [Releases](https://github.com/yuhuiai/YUCOM/releases) 页面。

#### Windows

需要 Go 1.25 或兼容版本。仓库提供了完整构建脚本：

```powershell
git clone https://github.com/yuhuiai/YUCOM.git
cd YUCOM
.\build.ps1
```

构建完成后，Windows 可执行文件位于 `dist/`。Windows 10/11 通常已经包含 WebView2 Runtime；如果系统提示无法加载 WebView2，请安装或修复 Microsoft WebView2 Runtime。

#### Linux / 银河麒麟

首次在联网开发机上构建 GTK3 原生版：

```bash
sudo apt-get install build-essential pkg-config libgtk-3-dev
git clone https://github.com/yuhuiai/YUCOM.git
cd YUCOM
bash "scripts/构建麒麟原生版.sh"
bash "scripts/启动YUCOM.sh"
```

如需为同版本、同架构的离线测试设备制作运行包：

```bash
bash "scripts/一键制作麒麟离线包.sh"
```

该脚本可能在联网开发机上通过 APT 安装构建依赖。生成的离线包包含运行程序、所需的非核心动态库、离线依赖包和操作说明；不会覆盖系统核心库。

### 快速测试

1. 连接已知正常的串口设备，或按照接口标准正确连接回环线。
2. 启动 YUCOM，点击串口号旁的刷新按钮扫描设备。
3. 选择端口，设置与对端一致的波特率、数据位、停止位、校验和流控。
4. 打开串口，使用“标准测试帧”验证双向收发。
5. 只有在电气标准和接线均已确认时，才执行“硬件回环自检”。

详细说明：

- [安装和使用步骤](01-安装和使用步骤.txt)
- [通用串口测试步骤](02-通用串口测试步骤.txt)

### 硬件安全

> [!WARNING]
> 回环接线前请关闭串口并给设备断电。TTL、RS232、RS422 和 RS485 的电气标准不同，不能直接混接。TTL 还必须确认 1.8V、3.3V 或 5V 等逻辑电压是否一致。接口不明时，请使用已知正常的外部设备进行双向测试，不要尝试回环。

RS485 常为两线半双工，单端口回环结果会受到方向控制和接收器使能影响，不能作为唯一判定依据。

### 隐私与系统改动

- 串口内容不会发送到互联网或局域网
- 程序不保存配置、接收内容或测试日志
- Windows 版不安装服务、不修改注册表
- YUCOM 不会自动修改 Linux 串口权限
- 安装 GTK3 开发依赖或离线系统包属于管理员操作，请先确认设备维护策略

### 参与项目

欢迎通过 [Issues](https://github.com/yuhuiai/YUCOM/issues) 报告问题或提出建议。提交问题时，建议提供操作系统、CPU 架构、串口设备路径或 COM 号、接口类型、串口参数、复现步骤和错误信息；请勿上传密码、令牌、客户数据或完整串口业务报文。

提交代码前请运行：

```bash
go test ./...
```

### 许可证状态

本仓库当前尚未选择开源许可证。源代码可以公开查看，但请不要把“公开可见”理解为已经获得复制、修改或分发许可。正式许可证将在项目所有者确认后另行添加。

---

<a id="english"></a>

## English

YUCOM is a cross-platform utility for verifying whether serial ports on computers, industrial PCs, motherboards, and external devices can be detected and can transmit and receive data correctly. It is hardware-agnostic and does not install or replace serial drivers.

The Windows build runs in a standalone embedded WebView2 window. The native Linux build uses GTK3. Serial data is processed locally by YUCOM and is not sent over the internet or the local network.

### Features

- Detection of onboard, USB, PCIe, and multi-port serial devices
- Stable `/dev/serial/by-id` device names on Linux
- Baud rates from 300 to 921600, 5–8 data bits, 1/2 stop bits, parity, and flow control
- Text and HEX transmit/receive modes, line endings, pause, auto-scroll, and byte counters
- Standard text frames and a 512-byte raw-data hardware loopback test
- Timed transmission and modem-signal display
- No saved configuration, received content, or test logs
- Standalone Windows and native Linux windows; the legacy Linux web mode is bound to localhost only

### Supported platforms

| Operating system | Architecture | Interface |
| --- | --- | --- |
| Windows 10/11 | amd64, arm64 | Standalone embedded WebView2 window |
| Desktop Linux, including Kylin, Ubuntu, and Debian | amd64, arm64 | Native GTK3 window |

### Build from source

No prebuilt GitHub Release is available yet. Future ready-to-run packages will be published on the [Releases](https://github.com/yuhuiai/YUCOM/releases) page.

On Windows with Go 1.25 or a compatible version:

```powershell
git clone https://github.com/yuhuiai/YUCOM.git
cd YUCOM
.\build.ps1
```

On a Linux development machine:

```bash
sudo apt-get install build-essential pkg-config libgtk-3-dev
git clone https://github.com/yuhuiai/YUCOM.git
cd YUCOM
bash "scripts/构建麒麟原生版.sh"
bash "scripts/启动YUCOM.sh"
```

### Hardware safety

> [!WARNING]
> Power off the device before changing loopback wiring. TTL, RS232, RS422, and RS485 use different electrical standards and must not be connected directly to one another. Verify TTL voltage levels before connecting pins. If the interface is uncertain, test with a known-good external device instead of using a loopback wire.

### Contributing

Issues and pull requests are welcome. Before submitting code, run `go test ./...`. Bug reports should include the operating system, CPU architecture, serial device path or COM number, interface type, serial settings, reproduction steps, and error messages. Do not include credentials, customer data, or sensitive serial payloads.

### License status

No open-source license has been selected for this repository yet. The source is publicly visible, but public visibility alone does not grant permission to copy, modify, or redistribute it. A license may be added after confirmation by the project owner.

