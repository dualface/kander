# 终端后端与容器隔离

Kander 严格且仅通过 `internal/terminal` 包访问终端会话（如 `herdr`、`tmux` 或直接进程启动器）。

本文档阐明 `terminal.Backend` 接口定义、`WINDOW` 地址契约、后端能力标志位（Capabilities）以及杜绝调用者直接拼装命令行字符串的架构铁律。

---

## 1. 终端隔离的架构铁律

为确保跨异构终端复用器与操作系统的绝对稳定性，Kander 确立了严格的分层隔离边界：

```text
[ launch / liveness / notify / takeover / focus / tui ]
                         |
                         | (Backend 抽象接口与 Capabilities 标志)
                         v
                internal/terminal
                         |
       +-----------------+-----------------+
       |                                   |
[ 声明式后端 Declarative ]          [ 直接后端 Direct ]
 (herdr, tmux, tmux-session)       (foreground, console)
```

1. **禁止直接拼装终端命令**：调用者（`launch`、`liveness`、`notify`、`takeover`、`focus`、`tui` 等）**绝不**构建命令参数列表，**绝不**直接调用终端二进制可执行文件。统一通过 `terminal.Lookup`、`terminal.ParseWindow`、`terminal.ResolveAuto` 获取后端实例并调用其接口方法。
2. **基于能力位（Capabilities）降级**：调用层基于 `terminal.Capabilities` 判定功能支持，绝不按名称硬编码字符串比较（例如检测是否支持容器使用 `caps.Has(terminal.CapContainer)`，而非判断 `launcher == "herdr"`）。
3. **单向依赖层次**：`internal/terminal` 绝不反向导入任何上层编排包。

---

## 2. 包职责划分

| 包路径 | 核心职责 |
|---|---|
| `internal/terminal` | 定义 `Backend` 接口、`Address`、`Target`、`PaneFacts`、`Topology` 及 `DeclarativeBackend` 引擎。 |
| `internal/terminal/builtin` | 注册内置声明式定义（`definitions/herdr.json`、`tmux.json`）与核心 Go 钩子。 |
| `internal/terminal/herdr` | 承载 herdr 套接字会话握手与面板聚焦专用钩子。 |
| `internal/terminal/direct` | 承载无容器后端（`foreground`、`console`）。所有容器分配方法均返回 `terminal.ErrUnsupported`。 |
| `internal/terminal/terminaltest`| 将测试二进制作为 Fake 终端的测试夹具。 |

---

## 3. `WINDOW` 地址契约

任务卡通过 `WINDOW` 字段记录其绑定的终端会话地址，固定格式为：

$$\text{WINDOW} = \langle\text{launcher}\rangle{:}\langle\text{opaque}\rangle$$

仅有负责该启动器的后端实现才有权解析与格式化 opaque 不透明部分：

| 启动器 | 不透明格式 | 解析后的 `Address` 结构体 |
|---|---|---|
| `herdr` | `<tab-id>:<pane-id>` | `Container` = 标签页 (`w0:...`), `Pane` = 面板 |
| `tmux` | `<session-id>:<window-id>:<pane-id>` | `Session` = `$0`, `Container` = `@1`, `Pane` = `%1` |
| `tmux-session` | `<session-name>:<window-id>:<pane-id>` | `Session` = 会话名, `Container` = `@1`, `Pane` = `%1` |
| 声明式用户定义 | 由定义 schema 中 `address` 字段以冒号拼接 | 映射为命名的定义地址字段 |
| `foreground` / `console` | *(无)* | 纯启动器名称；不可拆分 |

---

## 4. 能力标志位（Capabilities）

后端通过 `terminal.Capabilities` 显式声明其支持的功能：

| 能力标志 | 说明 | herdr | tmux / tmux-session | foreground / console |
|---|---|:---:|:---:|:---:|
| `Container` | 支持分配隔离的标签页或窗口 | 支持 | 支持 | 不支持 |
| `Focus` | 支持将焦点切到指定窗口/面板 | 支持 | 支持 | 不支持 |
| `PaneMetadata` | 支持读取窗格自定义变量 | 不支持 | 支持 | 不支持 |
| `ForegroundProcess` | 支持检查当前前台运行的 PID 与进程名 | 不支持 | 支持 | 不支持 |
| `AgentIdentity` | 支持 Agent 握手汇报身份校验 | 支持 | 不支持 | 不支持 |
| `SessionReport` | 支持双向会话握手通道 | 支持 | 不支持 | 不支持 |
| `WaitOutput` | 支持原生屏幕输出模式等待 | 支持 | 不支持 *(轮询模拟)* | 不支持 |
| `POSIXOnly` | 仅支持 POSIX 环境 | 不支持 | 支持 | 不支持 |
| `Detached` | 脱离托管的后台独立进程 | 不支持 | 不支持 | 仅 console |

---

## 5. 核心操作接口

每个后端操作均接收 `terminal.Conn`（包含可执行路径与调用方 runner）：
- `ProbeRunner`：带有超时或默认配额，管理并收割进程树。
- `SpawnRunner`：启动普通子进程，仅遵守 context 超时。

| 操作方法 | 说明 |
|---|---|
| `Prepare` | 解析目标程序、平台兼容性、PATH 及运行环境变量。 |
| `CreateContainer` | 分配新标签页/窗口并返回其 `Address`。 |
| `WaitReady` | 等待容器终端就绪。 |
| `RunCommand` | 发送初始命令行给窗格执行。 |
| `SetSessionMarker` | 给窗格打上 Kander 静态会话标记。 |
| `ReportSession` | 汇报 Agent 启动身份至复用器套接字。 |
| `PaneFacts` | 检查窗格存活、前台进程与标记元数据。 |
| `ReadOutput` | 读取窗格当前屏幕缓冲区文本。 |
| `WaitOutput` | 等待窗格屏幕输出匹配指定模式。 |
| `DeliverText` | 向窗格输入文本并追加回车。 |
| `Focus` | 将指定标签页/窗格切换到用户前台展示。 |
| `CloseContainer` | 干净关闭容器标签页或窗口。 |

---

## 6. 错误分类体系

后端操作返回结构化的 `*terminal.CommandError` 错误：

1. **`KindExec`**：进程无法启动（找不到二进制或 context 超时）。
2. **`KindExit`**：命令退出码非 0，包含原始 `Code` 和 `Stderr`。
3. **响应解析错误**：`KindNotJSON`、`KindNotObject`、`KindMissingResult`、`KindInvalidResponse` 标识声明式步进输出不合规。
4. **容器已消失**：窗格退出属于客观事实（`PaneFacts.Gone = true`），绝不作为未捕获异常抛出。
5. **不支持的操作**：调用后端未声明能力的方法将明确返回 `terminal.ErrUnsupported`。
