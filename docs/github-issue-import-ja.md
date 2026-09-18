# GitHub Issue 取り込みと結果プロトコル

`kander issue import NUMBER` コマンドは、リモートの GitHub Issue をローカルの `backlog/` にある標準タスクカードへ変換し、編集されていない原文を専用の添付ファイルとして安全に保存します。

取り込み操作は**冪等**かつ**有界**であり、**単一の排他ボードトランザクション**内でアトミックに公開されるため、ソースが存在しない不完全なカードが作成されることはありません。

---

## 1. 取り込みコマンドとショートカット

```sh
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments]
                           [--type feature|bug|chore|research] [--large]
                           [--language 言語] [--json]
```

- `--repo`：対象リポジトリ（例: `github.com/owner/repo` または `owner/repo`）。ローカルの `gh` CLI 認証情報を直接利用し、Token を要求しません。
- `--comments`：Issue の会話コメントも同時に取得します。トラフィック節約のためデフォルトは無効です。
- `--type`：ラベルから自動判定される TYPE を上書き。
- `--large`：`SIZE: large` を明示指定。取り込まれたカードは常にディレクトリ形式で生成されます。
- `--language`：カードの `LANGUAGE` を指定（デフォルトは設定済みの `agent_language`）。
- `--json`：機械可読な JSON（`task_id`, `state`, `path`, `existing`, `source_key`, `source_url`, `comments_loaded`）を出力。

### ターミナルカンバンオーバーレイ（`g`）

ボード上で `g` キーを押すと GitHub Issues オーバーレイが開きます：
- `i` / `I`：選択された Issue を取り込み（`I` はコメント付き）。
- `g`：すでに取り込み済みの Issue である場合、該当のローカルカードへジャンプ。
- `s`：未バインドの Issue に対して調査セッション（Triage）を開始、または完了カードに対して[結果同期](github-issue-results.md)を開始。

---

## 2. ソースキー（Source Key）と冪等性

取り込みカードの唯一のバインドキーは、正規化された Source Key です：

```text
github://HOST/OWNER/REPO/issues/NUMBER
```

- **アトミックな一意性確認**：排他的な `board.lock` トランザクション内で検証。
- **並行取り込みの保護**：同一 Issue の多重取り込みは既存カード（`existing: true`）を安全に返却。
- **ID 衝突回避**：可読 ID が重複した場合は、source_key ハッシュの先頭 8 文字を自動付加。

---

## 3. 添付ファイルとセキュリティサニタイズ境界

取り込まれた各タスクディレクトリは、`spec.md` の隣に 2 つの専用ファイルを保持します：

```text
kanban/backlog/<task-id>/
  ├── spec.md
  └── source/
      ├── github-issue.json    (スキーマ版数付きの機械可読スナップショット)
      └── github-issue.md      (人間が閲覧可能な Markdown ミラー)
```

### セキュリティ境界：信頼できない外部入力

プロンプトインジェクションや権限昇格を防ぐため：
1. **メタデータ注入の完全遮断**：リモート本文がカードのヘッダーやレビューセクションに直接混入することはありません。悪意ある第三者が Issue 経由で `CARD_REVIEW` 等を偽造することは不可能です。
2. **タイトルのサニタイズ**：Issue タイトルは `spec.md` の `H1` 見出しとしてのみ使用され、1 行に強制整形されます。
3. **文字列の無害化**：制御文字、bidi 上書き文字、不正な UTF-8 は事前にすべて除去されます。

### ペイロード制限

| チェック項目 | 最大制限 | 超過時の動作 |
|---|---|---|
| Issue 本文 | 512 KiB | エラーで拒否 |
| 単一コメント | 64 KiB | エラーで拒否 |
| コメント件数 | 50 件 | 最新 50 件に切り詰め |
| 合計サイズ | 1 MiB | エラーで拒否 |

---

## 4. Issue スナップショットキャッシュ

オーバーレイは以下のパスにローカルキャッシュを保持します：

```text
<board>/.kander/caches/issues/v1/<sha256(source_key)>.json
```

- **高速描画**：選択された行はキャッシュから即座に描画され、ネットワーク更新はバックグラウンドで処理。
- **保護と容量制限**：`0600` 権限で保存され、最大 200 件または 16 MiB に制限（LRU 破棄）。

---

## 5. アンカーカードと兄弟カード（Anchor & Siblings）

1 つの巨大な Issue を複数のタスクカードに分割して作業する場合：

```text
[GitHub Issue #42]
        |
        v (kander issue import)
+-------------------------------+
| アンカーカード Anchor (実体を保持)|
+-------------------------------+
        |
        +---> 兄弟カード Sibling 1 (アンカーを参照)
        +---> 兄弟カード Sibling 2 (アンカーを参照)
```

- **アンカーカード（Anchor）**：`source/github-issue.*` を保持し `source_key` を所有する唯一の親カード。
- **兄弟カード（Sibling）**：Issue を再取り込みせず、`DISCUSSION` ブロックでアンカーを参照：
  ```text
  SOURCE_ISSUE: github://HOST/OWNER/REPO/issues/42 (anchor: <anchor-task-id>)
  ```
