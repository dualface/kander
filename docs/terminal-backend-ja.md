# ターミナルバックエンドとコンテナ定義

Kander は、`internal/terminal` パッケージを通じてのみ端末セッション（`herdr`, `tmux`, または直接プロセスのランチャー）にアクセスします。

本文書では、`terminal.Backend` の操作セット、`WINDOW` アドレス規約、機能フラグ（Capabilities）、および呼び出し元が生のターミナルコマンドラインを直接操作しないためのアーキテクチャルールについて解説します。

---

## 1. ターミナル分離の鉄則

多様なターミナルマルチプレクサや OS 環境において絶対的な安定性を保つため、Kander は以下の厳格な分離境界を設けています：

```text
[ launch / liveness / notify / takeover / focus / tui ]
                         |
                         | (Backend 抽象インターフェース & Capabilities)
                         v
                internal/terminal
                         |
       +-----------------+-----------------+
       |                                   |
[ 宣言型バックエンド Declarative ]   [ 直接バックエンド Direct ]
 (herdr, tmux, tmux-session)       (foreground, console)
```

1. **生のコマンドライン構築の禁止**：呼び出し側（`launch`, `liveness`, `notify`, `takeover`, `focus`, `tui` 等）は、コマンドライン引数を自前で組み立てたり、端末のバイナリを直接実行したり**しません**。`terminal.Lookup` 等を通じてバックエンドを取得し、インターフェース経由で呼び出します。
2. **Capabilities による機能判定**：ランチャー名でのハードコード比較ではなく、`terminal.Capabilities` による判定を行います（例：コンテナ対応の確認は `launcher == "herdr"` ではなく `caps.Has(terminal.CapContainer)`）。
3. **単方向の依存関係**：`internal/terminal` が上位のオーケストレーションパッケージに逆依存することはありません。

---

## 2. パッケージ構成

| パッケージ | 役割 |
|---|---|
| `internal/terminal` | `Backend` インターフェース、`Address`, `Target`, `PaneFacts`, `Topology`, `DeclarativeBackend` の定義。 |
| `internal/terminal/builtin` | 組み込みの宣言的定義（`herdr.json`, `tmux.json`）と Go フックの登録。 |
| `internal/terminal/herdr` | herdr ソケット通信とペインフォーカス用のフック実装。 |
| `internal/terminal/direct` | コンテナを持たないバックエンド（`foreground`, `console`）。コンテナ系メソッドは `terminal.ErrUnsupported` を返却。 |
| `internal/terminal/terminaltest`| テスト用モックターミナル。 |

---

## 3. `WINDOW` アドレス規約

タスクカードは、関連付けられた端末のアドレスを `WINDOW` フィールドに以下の形式で保持します：

$$\text{WINDOW} = \langle\text{launcher}\rangle{:}\langle\text{opaque}\rangle$$

opaque（不透明部分）のエンコードおよびデコードは、対応するバックエンドのみが行います：

| ランチャー | 不透明部分の形式 | パース後の `Address` 構造 |
|---|---|---|
| `herdr` | `<tab-id>:<pane-id>` | `Container` = タブ (`w0:...`), `Pane` = ペイン |
| `tmux` | `<session-id>:<window-id>:<pane-id>` | `Session` = `$0`, `Container` = `@1`, `Pane` = `%1` |
| `tmux-session` | `<session-name>:<window-id>:<pane-id>` | `Session` = セッション名, `Container` = `@1`, `Pane` = `%1` |
| 宣言型ユーザー定義 | 定義スキーマの `address` フィールドをコロン連結 | 名前付きアドレスフィールドへマッピング |
| `foreground` / `console` | *(なし)* | 単体のランチャー名。パース不可 |

---

## 4. 機能フラグ（Capabilities）

バックエンドは `terminal.Capabilities` によりサポート機能を明示します：

| 機能フラグ | 説明 | herdr | tmux / tmux-session | foreground / console |
|---|---|:---:|:---:|:---:|
| `Container` | 隔離されたタブ/ウィンドウを生成可能 | 対応 | 対応 | 非対応 |
| `Focus` | ウィンドウやペインを前面にフォーカス可能 | 対応 | 対応 | 非対応 |
| `PaneMetadata` | ペインの環境変数やオプションを読み取り可能 | 非対応 | 対応 | 非対応 |
| `ForegroundProcess` | フォアグラウンドの PID/プロセス名を特定可能 | 非対応 | 対応 | 非対応 |
| `AgentIdentity` | Agent による自己申告IDの検証に対応 | 対応 | 非対応 | 非対応 |
| `SessionReport` | ソケットを通じた双方向セッション報告に対応 | 対応 | 非対応 | 非対応 |
| `WaitOutput` | 画面出力のパターンマッチング待機に対応 | 対応 | 非対応 *(ポーリング)* | 非対応 |
| `POSIXOnly` | POSIX 環境のみに限定 | 非対応 | 対応 | 非対応 |
| `Detached` | 管理外の独立したバックグラウンドプロセス | 非対応 | 非対応 | console のみ |

---

## 5. 主な操作一覧

各操作は実行ファイルパスとランナーを内包した `terminal.Conn` を受け取ります：
- `ProbeRunner`：タイムアウト監視とプロセスツリー管理を提供。
- `SpawnRunner`：通常のプロセス起動を行い、コンテキストの期限のみを監視。

| メソッド | 説明 |
|---|---|
| `Prepare` | コマンド、プラットフォーム、PATH、環境変数の解決。 |
| `CreateContainer` | 新しいタブ/ウィンドウを生成し `Address` を返却。 |
| `WaitReady` | 端末が入力受付可能になるまで待機。 |
| `RunCommand` | ペインへ初期コマンド行を送信。 |
| `SetSessionMarker` | ペインに Kander の静的マーカーを設定。 |
| `ReportSession` | マルチプレクサのソケットへ Agent の起動情報を報告。 |
| `PaneFacts` | ペインの生存状態、プロセス情報、マーカーを取得。 |
| `ReadOutput` | ペイン画面のテキストバッファを取得。 |
| `WaitOutput` | 指定パターンの出力が現れるまで待機。 |
| `DeliverText` | テキストをペインへ送信し Enter を押下。 |
| `Focus` | 対象のタブやペインを最前面にフォーカス。 |
| `CloseContainer` | コンテナを正常終了。 |

---

## 6. エラー分類

バックエンドの戻り値は `*terminal.CommandError` に分類されます：

1. **`KindExec`**：バイナリが存在しない、またはコンテキストタイムアウト等の起動失敗。
2. **`KindExit`**：非ゼロ終了コード。`Code` と `Stderr` を保持。
3. **レスポンス構文エラー**：`KindNotJSON`, `KindNotObject`, `KindMissingResult`, `KindInvalidResponse`。
4. **ペイン消失**：ペインが閉じている場合はエラーではなく客観的事実（`PaneFacts.Gone = true`）として扱われます。
5. **未対応の操作**：提供されていない機能の呼び出しは `terminal.ErrUnsupported` を返します。
