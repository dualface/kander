# Kander

[English](README.md) | [简体中文](README-CN.md) | **日本語**

[![Kander - 複数 AI Agent のカンバン調整](docs/star-please.png)](https://github.com/dualface/kander)

カンバンで複数の AI Agent を並行スケジューリング、独立レビューと品質ゲートを標準装備。

> **デモではなく、実際のソフトウェア開発のために。**  
> 2026 年 8 月以来、Kander は [QuickTUI](https://quicktui.ai) の本番開発で 1,000 件近い実タスクを完了してきました。実際の競合、バグ、不整合を解決しながら鍛え上げられた、堅牢な Agent スケジューリング、独立レビュー、自動リカバリを提供します。
> 
> 詳細レポート：[Kander 本番運用の振り返り](docs/KANDER_PRODUCTION_RETROSPECTIVE.md) ｜ [本番運用詳細レポート（英語深度分析）](docs/KANDER_PRODUCTION_RETROSPECTIVE_FULL_EN.md)

![Kander ワークフロー](docs/workflow-ja.svg)

### 主な機能

- **2 段階の独立レビューゲート**：実装とレビューを物理分離。PMQA が潜在的な仕様不整合やステートマシン欠陥を検知し、誤ったテスト通過（false-green）を阻止。
- **コンフリクト検知スケジューリング**：Git ブランチの重複や変更ファイル境界を静的解析し、書き込み衝突のない安全な並行実行を実現。
- **端末とワークスペースの完全分離**：Git Worktree と tmux/herdr コンテナを活用し、複数 Agent の完全並行実行を分離。
- **設定不要の GitHub 連携**：ローカルの `gh` 認証情報をそのまま利用。ボード内から Issue 閲覧、タスクカードへのワンクリック取り込み、進捗同期が可能。

## 1. クイックスタート

実行には Git と、Codex、Claude、Grok、Cursor、Pi のうち少なくとも 1 つが必要です。

**macOS** — Homebrew でインストールします：

```sh
brew install dualface/tap/kander

kander
```

**Linux** — バイナリを直接ダウンロードします：

```sh
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64
curl -fsSL "https://github.com/dualface/kander/releases/latest/download/kander-linux-${ARCH}.tar.gz" | tar xz

./kander
```

**Windows** — [Releases](https://github.com/dualface/kander/releases) から `kander-windows-amd64.zip` をダウンロードし、展開して `kander.exe` を実行してください。

初回起動時にまだインストールされていなければ対話ウィザードが始まります。インストールが完了すればすぐに使えます。

4 ステップで始められます:

1. Agent セッションを新規に開き、そこで要件やタスクを議論し、目標と受け入れ条件を明確にします。Agent の Plan モードの利用を推奨します。
2. タスクが確定すると、Agent（Kander ルール導入後）がカンバンフローでタスクを起動するか確認します。承認するとタスクが自動作成・起動されます。
3. 要件が複数ある場合は、要件ごとにステップ 1-2 を繰り返し、継続的にタスクを手配・起動します。
4. コマンドラインインターフェースでタスクの状態を確認します:

```sh
kander
```

![ターミナルカンバン](docs/kanban-screenshot-01.png)

> 上図のカンバンの内容は私の実プロジェクト [https://quicktui.ai](https://quicktui.ai) のものです。QuickTUI はコンピュータ上のさまざまな Agent をリモート操作するツールで、iOS/Android/macOS/Linux/Windows に対応し、無料で使えます。

### ターミナルカンバンのショートカット

| キー | 説明 |
|---|---|
| `Space` / `Enter` | タスク詳細の表示（Vim スタイルの移動に対応） |
| `m` | タスク操作メニュー（開始、移動、アーカイブ） |
| `g` | GitHub Issues オーバーレイ（閲覧・ワンクリック取り込み） |
| `c` | 独立した Chat セッション起動（タスク化前の簡易相談） |
| `/` | タスクカードのフィルタ・検索 |
| `r` | カンバンデータの最新化 |

さらに詳しく: スライド [タスクを効率的に進める方法](docs/how-to-advance-tasks-efficiently-ja.pdf) (PDF) を参照してください。

## 2. 主なコマンド

- `kander`：対話型ターミナルカンバンを開く。
- `kander doctor`：環境依存、Agent の利用可否、ランチャー、ルール設定の診断と修復。

## 3. GitHub 連携

プロジェクトを GitHub リポジトリに紐づけるには [GitHub CLI](https://cli.github.com/)（`gh`）が必要です。Kander はトークンを要求・読み取り・保存せず、`gh` が管理する認証情報をそのまま利用します。

端末ボードで `g` を押すと、GitHub Issue の一覧がオーバーレイで開きます。

## 4. 応用ドキュメント

- [レビュー機構と完了ゲート](docs/review-disposition.md)
- [カードトランザクションと障害復旧](docs/card-transactions.md)
- [ターミナルバックエンドとコンテナ定義](docs/terminal-backend.md)
- [タスク永続化ディスパッチプロトコル](docs/durable-dispatch.md)
- [GitHub Issue 取り込みと結果プロトコル](docs/github-issue-import.md)

## 5. ライセンス

本プロジェクトは MIT License を使用しています。[LICENSE](LICENSE) を参照してください。

## 6. 変更履歴

リリースノートは [CHANGELOG.md](CHANGELOG.md) を参照してください。
