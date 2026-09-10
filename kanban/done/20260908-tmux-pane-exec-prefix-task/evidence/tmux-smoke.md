# tmux 真机验证证据

提交：b4f634edeb46daa695b73fbe433ce034c7d096ec

使用自建临时看板、临时配置和独立 tmux server；实际 Codex CLI。

```text
started: 20260908-exec-smoke-task	scale=small	agent=codex	session=kb-kander-tmux-smoke-pob9dizy-7bc66389	window=@2
view: tmux attach -t kb-kander-tmux-smoke-pob9dizy-7bc66389
$ /tmp/kander-tmux-smoke-pob9dizy/bin/tmux show-options -g default-shell 
default-shell /bin/sh

$ /tmp/kander-tmux-smoke-pob9dizy/bin/tmux list-panes -a -F #{pane_id} #{pane_pid} #{pane_current_command} #{pane_start_command} 
%2 895760 codex "exec /home/dualf/.local/bin/codex --model gpt-6-astra --config 'model_reasoning_effort=\"high\"' --dangerously-bypass-approvals-and-sandbox 'Execute Kanban task 20260908-exec-smoke-task; full instructions are in the UTF-8 task file at /tmp/kander-20260908-exec-smoke-task-start-2369282886.md; read the complete file first and follow it exactly.'"
%0 891268 sh 

$ /tmp/kander-tmux-exec check 20260908-exec-smoke-task 
liveness: 20260908-exec-smoke-task	Agent=codex	status=alive	channel=tmux-session	container=kb-kander-tmux-smoke-pob9dizy-7bc66389:@2	observed_at=2026-09-08T02:21:30.361975389Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks

$ /tmp/kander-tmux-exec notify 20260908-exec-smoke-task --message 本次为 tmux exec 前缀真机直投验证。请仅回复 smoke notified，保持会话等待退出。 
delivered; waiting for acknowledgement: channel=tmux-direct
notified: 20260908-exec-smoke-task	channel=tmux-direct	acknowledgement=confirmed	message-file=/tmp/kander-notify-6ac3faa9474cf3c3d8135f8f4d8c8fc8/message.txt

$ /tmp/kander-tmux-exec check 20260908-exec-smoke-task 
liveness: 20260908-exec-smoke-task	Agent=codex	status=alive	channel=tmux-session	container=kb-kander-tmux-smoke-pob9dizy-7bc66389:@2	observed_at=2026-09-08T02:21:38.02497838Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks

$ /tmp/kander-tmux-smoke-pob9dizy/bin/tmux list-panes -a -F #{pane_id} #{pane_pid} #{pane_current_command} 
%2 895760 codex
%0 891268 sh

```

附加观察：notify 命令返回 tmux-direct / acknowledgement=confirmed，但正文仍在 Codex 输入框；补发一次 Enter 后，Agent 输出两个 ACK 并最终回复 smoke notified。仅据命令成功断言路由直投，不能据此声称无需手动输入即可完成正文处理。

清理：临时卡完成后调用 dismiss，/exit 同样需要追加 Enter；实际 Agent 退出且任务 pane/window 消失，dismiss 返回 `tmux pane probe returned an invalid response`。独立 monitor session 随后移除，list-sessions 确认该独立 server 已无会话。未强杀 Agent。

上述输入/退出探测行为作为本机验证观察保留。base 至交付提交的 internal/notify、internal/probe、internal/takeover 没有改动；本卡不变更这些协议，后续可单独排查。
