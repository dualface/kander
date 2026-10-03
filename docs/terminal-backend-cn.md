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
 (herdr, luvus, tmux, tmux-session) (foreground, console)
```

1. **禁止直接拼装终端命令**：调用者（`launch`、`liveness`、`notify`、`takeover`、`focus`、`tui` 等）**绝不**构建命令参数列表，**绝不**直接调用终端二进制可执行文件。统一通过 `terminal.Lookup`、`terminal.ParseWindow`、`terminal.ResolveAuto` 获取后端实例并调用其接口方法。
2. **基于能力位（Capabilities）降级**：调用层基于 `terminal.Capabilities` 判定功能支持，绝不按名称硬编码字符串比较（例如检测是否支持容器使用 `caps.Has(terminal.CapContainer)`，而非判断 `launcher == "herdr"`）。
3. **单向依赖层次**：`internal/terminal` 绝不反向导入任何上层编排包。

---

## 2. 包职责划分

| 包路径 | 核心职责 |
|---|---|
| `internal/terminal` | 定义 `Backend` 接口、`Address`、`Target`、`PaneFacts`、`Topology` 及 `DeclarativeBackend` 引擎。 |
| `internal/terminal/builtin` | 注册内置声明式定义（`definitions/herdr.json`、`luvus.json`、`tmux.json`）与核心 Go 钩子。 |
| `internal/terminal/herdr` | 承载 herdr 套接字会话握手与面板聚焦专用钩子。两者按 `HERDR_SOCKET_PATH` 拨同一个控制通道：POSIX 为 unix socket，Windows 为命名管道 `\\.\pipe\` 加该路径。 |
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
| `luvus` | `<pane-id>:<pane-id>` | `Container` = `Pane` = luvus pane id (`7`) |
| `tmux` | `<session-id>:<window-id>:<pane-id>` | `Session` = `$0`, `Container` = `@1`, `Pane` = `%1` |
| `tmux-session` | `<session-name>:<window-id>:<pane-id>` | `Session` = 会话名, `Container` = `@1`, `Pane` = `%1` |
| 声明式用户定义 | 由定义 schema 中 `address` 字段以冒号拼接 | 映射为命名的定义地址字段 |
| `foreground` / `console` | *(无)* | 纯启动器名称；不可拆分 |

---

## 4. 能力标志位（Capabilities）

后端通过 `terminal.Capabilities` 显式声明其支持的功能：

| 能力标志 | 说明 | herdr | luvus | tmux / tmux-session | foreground / console |
|---|---|:---:|:---:|:---:|:---:|
| `Container` | 支持分配隔离的标签页或窗口 | 支持 | 支持 | 支持 | 不支持 |
| `Focus` | 支持将焦点切到指定窗口/面板 | 支持 | 支持 | 支持 | 不支持 |
| `PaneMetadata` | 支持读取窗格自定义变量 | 不支持 | 不支持 | 支持 | 不支持 |
| `ForegroundProcess` | 支持检查当前前台运行的 PID 与进程名 | 不支持 | 不支持 | 支持 | 不支持 |
| `AgentIdentity` | 支持 Agent 握手汇报身份校验 | 支持 | 支持 | 不支持 | 不支持 |
| `SessionReport` | 支持双向会话握手通道 | 支持 | 不支持 | 不支持 | 不支持 |
| `WaitOutput` | 支持原生屏幕输出模式等待 | 支持 | 不支持 *(轮询)* | 不支持 *(轮询模拟)* | 不支持 |
| `POSIXOnly` | 仅支持 POSIX 环境 | 不支持 | 支持 | 支持 | 不支持 |
| `Detached` | 脱离托管的后台独立进程 | 不支持 | 不支持 | 不支持 | 仅 console |

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
| `PaneFacts` | 检查窗格存活、前台进程、标记元数据及上报的会话身份（`AgentSession` 值与 `AgentSessionKind` 种类）。 |
| `ReadOutput` | 读取窗格当前屏幕缓冲区文本。 |
| `WaitOutput` | 等待窗格屏幕输出匹配指定模式。 |
| `DeliverText` | 向窗格输入文本并追加回车。 |
| `Focus` | 将指定标签页/窗格切换到用户前台展示。 |
| `CloseContainer` | 干净关闭容器标签页或窗口。 |

---

## 6. 会话身份解析

具备 `AgentIdentity` 的后端以“种类 + 值”上报窗格会话（`AgentSessionKind`、`AgentSession`）。缺失种类按兼容语义视为直接 ID。所有消费端共享唯一的三态判定 `terminal.MatchAgentSession(ctx, agent, kind, value, reference)`：

- **匹配**：上报身份解析为卡片记录的会话引用。`id`（或缺失 kind）按字符串原样比较，从不按 UUID 解析。
- **不同**：已解析身份指向另一会话。
- **不确定**：身份缺失、种类未知或无法解析——既不能证明匹配也不能证明失配，绝不作为 stopped 证据。

`kind: "path"` 经 agent 定义中的 `session.file` 声明解析：文件只经 `internal/fs` 安全边界打开（拒绝符号链接/reparse/特殊文件；POSIX 叶节点以非阻塞方式打开，FIFO 失败而非挂起），在声明的 `max_bytes` 内读取首行，JSON 头必须携带声明的 `id_field`，以及配置了时的 `type_field`/`type_value` 匹配值。格式不支持、文件缺失或不可读、头超长、头不合规、agent 未声明，一律返回带可诊断原因的不确定。文件内容从不执行、从不越首行扫描，读取在既有探测预算内并检查调用方上下文。

`ReverseLookup` 遍历终端上报的全部行（不限于记录的 tab/workspace）：通过 `expect`、`checks` 与声明式 `match`（agent 过滤）的行成为候选，按同一规则解析其会话身份。恰好一个解析匹配即结果；确定的零匹配返回 `terminal.MatchError{Matches:0}`；多个确定匹配返回带计数的 `MatchError`；任一不可判定的同 agent 候选返回 `terminal.IncompleteLookupError`——未完成的查找，调用方必须按未知处理，绝不能当作唯一匹配或已证缺席。

---

## 7. 错误分类体系

后端操作返回结构化的 `*terminal.CommandError` 错误：

1. **`KindExec`**：进程无法启动（找不到二进制或 context 超时）。
2. **`KindExit`**：命令退出码非 0，包含原始 `Code` 和 `Stderr`。
3. **响应解析错误**：`KindNotJSON`、`KindNotObject`、`KindMissingResult`、`KindInvalidResponse` 标识声明式步进输出不合规。
4. **容器已消失**：窗格退出属于客观事实（`PaneFacts.Gone = true`），绝不作为未捕获异常抛出。
5. **不支持的操作**：调用后端未声明能力的方法将明确返回 `terminal.ErrUnsupported`。
6. **查找结果**：`terminal.MatchError`（已完成查找、零或多个匹配）与 `terminal.IncompleteLookupError`（存在不可判定候选、查找未完成）区分缺席、歧义与不可判定。

---

## 8. 内置 luvus launcher

内嵌的 [`luvus.json`](../internal/terminal/builtin/definitions/luvus.json) 提供 `luvus` launcher,用于在 [luvus](https://github.com/RizRiyz/luvus) pane 中运行 Kander。仅支持 POSIX。

- **前置条件**:`luvus` 在 `PATH` 中,`LUVUS_ENV=1` 且 `LUVUS_PANE_ID` 非空(两者由 luvus 在其 pane 中设置)。`LUVUS_ENV=1` 时 `auto` 以优先级 150 选中 luvus,介于 herdr(200)和 tmux(100)之间。
- **最低版本**:`pane split` 支持 `--cwd` 的首个 luvus 正式版(上游 PR #480);luvus 0.14.3 及更早版本不支持。luvus server 也必须是该版本:升级后请重启 server。
- **容器**:`pane split <LUVUS_PANE_ID> --no-focus --cwd <cwd>`,然后 `pane move --new-tab`、用卡片标签 `pane name`、再 `pane focus` 回到调用方 pane。容器就是 pane 本身,所以 `WINDOW` 为 `luvus:<pane>:<pane>`。
- **目录校验**:旧版 CLI 会静默忽略 `--cwd`,旧版 server 会忽略新 CLI 传来的目录;两种情况下 split 都成功,但新 pane 开在锚 pane 的目录。因此 `create_container` 用 `agent get` 读回新 pane(在目录上报前短暂轮询),只有上报目录等于请求目录或其解析符号链接后的形式时才接受。否则关闭新 pane 并以升级提示失败,`start` 把卡片回滚到 `todo/`,不启动 agent。
- **session 身份**:luvus 通过自己的集成 hook 上报 agent 的 session。每个 agent 执行一次 `luvus integration install <agent>`,`pane_facts` 和反查才能拿到 session id。Kander 不绑定自己的 session hook。
- **已知限制**:新 pane 移到新 tab 时调用方 pane 会短暂失去焦点。没有 luvus 集成 hook 的 agent(如 cursor、pi)不上报 session:liveness 仍能确认记录的 pane 中的 agent 与状态,但 `notify` 无法直接投递,pane 地址变化后的反查保持未完成(`unknown`),不会判为 `stopped`。普通 shell pane 的 agent 字段是 shell 名。tmux 与 luvus 嵌套时,`auto` 按上述优先级选择。
- 全局或项目 share 目录中名为 `luvus.json` 的用户定义会整体替换这个内嵌定义;要使用内置定义,请删除旧的手写副本。
