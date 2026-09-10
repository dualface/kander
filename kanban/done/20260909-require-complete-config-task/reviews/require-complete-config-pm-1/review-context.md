Fixes included this round: 无（首轮）。
Review focus: (1) 操作命令是否一律 Load(false) 且缺文件/非法 JSON/schema 失败立即退出；(2) doctor 是否仍 Repair；(3) TUI 选项面板 loadErr 与按 s 启动失败是否留在弹层、不认领。
Verification records: git diff --check 干净；go test ./... -count=1 于 a8d5a6f86e8a53e7b10826e392c70903875835d9 通过，21 个包 ok。
Environment gaps: 无。