# 第一组集成证据

创建锚点/集成前develop：4889d3fb93f8d2d639832b9da5588d668714d9bc。
最终组HEAD/集成后本地develop及origin/develop：251f5d89186730ea372053a401136030710efe84。
组rebase origin/develop无变化、无冲突、无提交重写；go test ./...全包通过。正常远端先push，fetch后主工作区ff成功。main未动，未部署，未迁移真实看板。

卡片最终提交无重写映射：
- S: a7fe54beb6ade00678e14655c0d385115eb950c8 -> a7fe54beb6ade00678e14655c0d385115eb950c8；是最终组HEAD祖先。
- D: 839f72119b22ce8b48811fc9819de9643a4028f8 -> 839f72119b22ce8b48811fc9819de9643a4028f8；是最终组HEAD祖先。
- A: 8241a49b1f50cbb99acc68b4c3dc58b5a803253e -> 8241a49b1f50cbb99acc68b4c3dc58b5a803253e；是最终组HEAD祖先。
- R: 251f5d89186730ea372053a401136030710efe84 -> 251f5d89186730ea372053a401136030710efe84；是最终组HEAD祖先。
