# Kander

[English](README.md) | [简体中文](README-CN.md) | **日本語**

[![Kander - 複数 AI Agent のカンバン調整](docs/star-please.png)](https://github.com/dualface/kander)

一人でカンバンを使って複数の AI Agent をスケジュールする。

![Kander ワークフロー](docs/workflow-ja.svg)

## 1. クイックスタート

実行には Git と、Codex、Claude、Grok、Cursor のうち少なくとも 1 つが必要です。

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
2. タスクが確定すると、Agent がカンバンフローでタスクを起動するか確認します。承認すればタスクは自動的に起動します。
3. 要件が複数ある場合は、要件ごとにステップ 1-2 を繰り返し、継続的にタスクを手配・起動します。
4. コマンドラインインターフェースでタスクの状態を確認します:

```sh
kander
```

![ターミナルカンバン](docs/kanban-screenshot-01.png)

`o` → インターフェース → テーマで **Tide**、**Dusk**、**Slate Dark**、**Slate Light** を選べます。Slate のダーク／ライトは herdr などのグレーブルーの端末に調和します。既存テーマと既定設定も引き続き利用できます。

> 上図のカンバンの内容は私の実プロジェクト [https://quicktui.ai](https://quicktui.ai) のものです。QuickTUI はコンピュータ上のさまざまな Agent をリモート操作するツールで、iOS/Android/macOS/Linux/Windows に対応し、無料で使えます。

さらに詳しく: スライド [タスクを効率的に進める方法](docs/how-to-advance-tasks-efficiently-ja.pdf) (PDF) を参照してください。

## 2. GitHub 連携

プロジェクトを GitHub リポジトリに紐づけるには [GitHub CLI](https://cli.github.com/)（`gh`）が必要です。Kander はトークンを要求・読み取り・保存せず、`gh` が管理する認証情報をそのまま利用します。

```sh
kander issue repo                                   # 現在のワークツリーのリポジトリを解決
kander issue repo --repo HOST/OWNER/REPO --json     # 参照を明示的に指定
kander issue list --state open --label bug          # Issue を一覧。--state open|closed|all、--label は複数指定可、--search、--limit、--json
kander issue show 42 --comments                     # 1 件の Issue とそのコメントを表示
kander issue import 42 --comments                   # backlog カードとして取り込み、本文とコメントも保存
```

`kander issue repo` はディレクトリ名を信用せず、GitHub に正規のリポジトリ識別情報を確認します。ワークツリーに複数の異なるリモートがある場合は推測せず、`--repo` または `gh repo set-default` を案内します。`kander doctor` は認証情報・リモート・アカウントを変更せずに、`gh` のパス、バージョン、ホストごとの認証状態を報告します。

`kander issue list` は状態・ラベル・検索語で絞り込み、取得する Issue 数を制限します。Pull Request は結果に混入しません。`kander issue show NUMBER` は 1 件の Issue を表示し、`--comments` は明示的な上限つきでコメントを読み込み、黙って切り捨てません。どちらのコマンドも `--json` に対応し、対処可能なエラー（`gh` がない、ホストが未認証、レート制限、リモートの曖昧さ）を報告します。

`kander issue import NUMBER` は Issue に紐づく通常の backlog カードを作成します。`--comments` で議論も保存し、`--type` はラベル由来の種別を上書きし、`--large` は規模を指定し、`--language` はカードの言語を固定し、`--json` はスクリプト向けの結果を出力します。Issue のタイトル・本文・コメントは `spec.md` の隣の `source/github-issue.json` と `source/github-issue.md` にそのまま保存され、カードの契約は確認済みのリポジトリ識別情報だけから生成されます。同じ Issue を再取り込みすると既存のカードを返し、ソースキーの一意性確認とカードの公開は同じボードトランザクションで行われるため、並行取り込みでも重複は生じません。上限を超える Issue は切り捨てずにヒント付きで拒否します。スナップショット形式とセキュリティモデルは [docs/github-issue-import.md](docs/github-issue-import.md) を参照してください。

端末ボードでは `g` が同じデータをオーバーレイで開きます。`Enter` で選択中の Issue とコメントを開き、`Tab` で状態を切り替え、`/` で検索、`l` でラベル絞り込み、`i` で Issue を backlog カードとして取り込み（取り込み済みなら対応カードへ移動、`I` はコメント付きで取り込み）、`r` で更新、`o` でブラウザ表示、`Esc`/`q` でボードを変えずに閉じます。取り込み済みの Issue はタスク ID を表示し、リモートが更新されていてもマークするだけでカードを上書きしません。すべてのリクエストはバックグラウンドで実行され、ボードはブロックしません。

## 3. ライセンス

本プロジェクトは MIT License を使用しています。[LICENSE](LICENSE) を参照してください。
