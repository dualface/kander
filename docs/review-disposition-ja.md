# レビューの処置と完了ゲート

> Historical translation: the report-format section below describes the former strict protocol. For current schema 2 behavior, `review interpret`, and pending-interpretation gates, use the [English specification](review-disposition.md#receiver-interpretation). Existing schema 0/1 evidence is unchanged.

本文書では、Kander のコードレビューライフサイクル、構造化された指摘事項（Findings）の管理、作成者による処置（Author Disposition）の追跡、および完了ゲート（Completion Gate）の検証メカニズムについて説明します。

`internal/board` はデータモデル、永続化の解析、制御された公開、純粋な構造検証を担当し、`internal/review` は CLI コマンド、Reviewer プロンプトの組み立て、Git 検証を提供します。`board` が `review` に逆依存することはありません。

---

## 1. 概要とワークフローパイプライン

アクティブなすべてのタスクカードは、`done` に移動する前に明示的なレビュー計画を完了する必要があります。レビューをスキップまたは適用外とする場合でも、明示的な評価記録を封印する必要があります。

```text
+-------------+      +---------------+      +-------------------+
| レビュー計画 | ---> | Reviewer 実行 | ---> | 構造化指摘の抽出  |
| Review Plan |      | Reviewer Run  |      | Extract Findings  |
+-------------+      +---------------+      +-------------------+
                                                      |
                                                      v
+-------------+      +---------------+      +-------------------+
| バッチクローズ| <--- | 対象コミット前進| <--- |  作成者の処置/修正|
| Batch Close |      | Advance Target|      | Author Disposition|
+-------------+      +---------------+      +-------------------+
      |
      v
+-------------+
| "done" へ移動|
+-------------+
```

1. **レビュー計画（Review Plan）**：必須ロール（`PMQA`、`Security`）とバッチのコードベースラインを策定。
2. **Reviewer 実行（Reviewer Run）**：独立した Agent がレビューを実行し、構造化された指摘ブロックを出力。
3. **割り当て（Assignment）**：各指摘事項を特定のタスクカードに明示的に割り当て。
4. **作成者の処置（Author Disposition）**：タスクの所有者（OWNER）が修正または確認を行い、Commit SHA と検証証拠を提出。
5. **対象コミットの前進（Target Advance）**：修正コミットに伴いバッチの対象コミットを前進。
6. **バッチクローズ（Batch Close）**：全要件の合格または機械的検証が完了したらバッチを閉じ、`move done` をアンロック。

---

## 2. 実行サイクルと計画

カードを `done` に移動するには、封印済み（`sealed: true`）のレビュー計画が必要です。空のレビューインデックスは決して合格を意味しません。

### 計画管理コマンド

```sh
kander review plan <絶対作業ディレクトリ> <絶対パス-plan.json>
kander review extend-plan <絶対作業ディレクトリ> <絶対パス-extension.json>
kander review progress <絶対作業ディレクトリ> <task-id>
```

### 単一バッチ計画の例

```json
{
  "schema": 1,
  "sealed": true,
  "plan_id": "implementation-cycle",
  "author": "coordinator",
  "basis": "確定済みタスクおよび当リポジトリの AGENTS.md 規約",
  "cwd": "/absolute/group-worktree",
  "report_language": "ja",
  "task_ids": ["20260907-example-task"],
  "batches": [{
    "batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<完全な-base-sha>",
    "target_commit": "<完全な-target-sha>",
    "requirements": {
      "PMQA": "required",
      "Security": "N/A: 当リポジトリの AGENTS.md により Security ロールは免除"
    }
  }]
}
```

- **ロール構成**：標準的な計画では `PMQA` と `Security` の 2 ロールを使用します。過去の 4 ロールおよび 6 ロールの計画も引き続き読み取りとクローズが可能です。
- **N/A の取り扱い**：ロールが適用外の場合は、根拠を明記する必要があります（`N/A: <理由と規約上の根拠>`）。
- **非 Git プロジェクト**：すべてのロールが `N/A` の場合、`base` と `target_commit` は `"N/A"` と記述できます。いずれかのロールが `required` の場合は、完全な 40 桁 SHA が必須です。

### サイクルの再バインド（Rebinding）

`kander move <id> working --owner <agent>` によって `STARTED_AT` が変更された場合、計画全体の状態は `requirements-needed` になります。
- `kander review progress` は変更されたメンバー全員の `rebind_cycles` を出力します。
- `extend-plan` に `rebind_cycles` を渡すことで、新しいサイクルをアトミックに再バインドします。過去の実行履歴や作成者の処置記録はすべて保持されます。

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "新しい OWNER が指定されました。グループ全体のレビュー義務はすべて保持されます",
  "rebind_cycles": {"20260907-example-task": "<progress が返した現在のサイクルダイジェスト>"}
}
```

### 段階的なマルチバッチ計画（Multi-Batch Planning）

段階的に納品する場合：
1. 初期化時は `"sealed": false` とし、第 1 バッチのみを指定します。
2. `batch-one` のクローズ後、`extend-plan` を呼び出して `batch-two` を追加します。
3. 最終バッチで `"seal": true` を指定して計画を封印します。

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "前のバッチがクローズされたため、次のバッチの納品を受け入れます",
  "seal": true,
  "batch": {
    "batch_id": "batch-two",
    "previous_batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<前のバッチでクローズされた target-sha>",
    "target_commit": "<次のバッチの target-sha>",
    "requirements": {
      "PMQA": "required",
      "Security": "N/A: プロジェクト規約"
    }
  }
}
```

---

## 3. 構造化された指摘とレガシーレポートのマッピング

各 Reviewer Agent のレポート末尾には、以下の独立したコードブロックを必ず 1 つ出力する必要があります：

````text
```kander-findings
{
  "FINDINGS": [{
    "id": "PM-01",
    "tier": "medium",
    "text": "指摘事項の完全な原文",
    "evidence": "file.go:42; 具体的な発生条件と影響"
  }],
  "NON_BLOCKING": []
}
```
````

### 検証ルール

- **配列の必須性**：`FINDINGS` と `NON_BLOCKING` の両配列が必須です。
- **重要度レベル**：
  - `FINDINGS`（ブロッキング障害）：`blocking`, `high`, `medium` のみ。
  - `NON_BLOCKING`（非ブロッキング提案）：`low`, `recommend`, `suggest` のみ。
- **ID の一意性**：両配列を通じて ID はグローバルに一意でなければなりません。
- **系譜の追跡（Lineage）**：前のラウンドから引き継がれた指摘は、直前の親を明示する必要があります：
  ```json
  "lineage": {"run_id": "前回の run_id", "finding_id": "PM-01"}
  ```
- **機械的修正**：オプションで `mechanical: "documentation" | "dead-code" | "redundant-test"` を指定可能。

### レガシーな非構造化レポートのマッピング

旧形式のレポート（`findings_schema = 0`）は、以下のコマンドで手動マッピングを行います（元ファイルは上書きされません）：

```sh
kander review map-legacy <絶対作業ディレクトリ> <マッピングファイル.json>
```

マッピング内の行番号と引用文は、元のレポートと一字一句一致している必要があります。

---

## 4. 指摘の割り当てと作成者の処置記録

```sh
kander review assign <絶対作業ディレクトリ> <割り当て.json>
kander review disposition <絶対作業ディレクトリ> <処置記録.json> <期待されるカード版番号>
kander review aggregate <絶対作業ディレクトリ> <batch-id>
```

### 割り当て（Assignment）

指摘事項は対象タスクカードに明示的に割り当てる必要があります：

```json
{
  "run_id": "pm-first",
  "batch_id": "batch-one",
  "author": "coordinator",
  "basis": "タスクの変更範囲に基づいて割り当て",
  "items": {
    "PM-01": ["20260907-example-task"]
  }
}
```

### 作成者の処置記録（Author Disposition）

タスクの現在の `OWNER` が、割り当てられた指摘に対して処置を提出します：

```json
{
  "record_id": "pm01-author-first",
  "run_id": "pm-first",
  "finding_id": "PM-01",
  "batch_id": "batch-one",
  "task_id": "20260907-example-task",
  "author": "codex",
  "report_hash": "<report.md の SHA-256>",
  "original": "指摘のテキストと完全一致する原文",
  "status": "fixed",
  "basis": "対象ソースコードおよび実際の呼び出し経路で検証済み",
  "fix_commit": "<完全な修正-sha>",
  "verification": "go test ./internal/auth -v"
}
```

### ステータス遷移ルール

| 処置ステータス | 適用対象 | バッチクローズをブロックするか？ | 説明 |
|---|---|---|---|
| `confirmed` | ブロッキング | **はい** | 確認済み、修正待ち。 |
| `unverifiable` | ブロッキング | **はい** | 再現不可。 |
| `fixed` | ブロッキング / 非ブロッキング | いいえ | `fix_commit`（target より厳密に後）と `verification` 検証結果が必須。 |
| `rejected` | ブロッキング / 非ブロッキング | 未解決リストへ移動 | 客観的事実に基づく理由が必要。 |
| `waived` | Security のみ | いいえ | `accepted-risk` または 15 分経過した `timed-out` 通知が必要。 |
| `deferred` | 非ブロッキングのみ | いいえ | 延期理由が必要。 |

---

## 5. 前進、増分レビュー、バッチクローズ

### 1. バッチ目標の前進（Advance）

修正コミット完了後、バッチの目標コミットを前進させます：

```sh
kander review advance <絶対作業ディレクトリ> <前進リクエスト.json>
```

リクエストには `{batch_id, expected_revision, advance: {previous_target, target, reason, deliveries}}` を含めます。Git 作業ツリーがクリーンである必要があります。

### 2. 増分レビュー（Incremental Review）

直前のラウンドを指定して増分レビューを実行します：

```sh
kander review run --task <task-id> --previous-run-id <run-id>
```

Reviewer のプロンプトには、前回のレポート、改ざん防止された処置記録、集約ビューが自動的に注入されます。

### 3. 機械的修正のクローズゲート（Mechanical Fix Closure）

残りの修正が**すべて**機械的（コメントのタイポ修正、不要コード削除など）である場合、ゲートは Reviewer Agent を再起動することなく `passed_at` を修正コミットまで進めることを許可します。

クローズリクエストには `mechanical` 検証配列を添付する必要があります：
- `diff_hash`：変更ファイルに対する生の Git diff の SHA-256。
- `facts`：ロジック変更がないことを示す客観的事実の証拠。

```sh
git diff --no-ext-diff --no-textconv --no-renames --binary --full-index --no-color <run-commit> <fix-commit> -- ':(literal)<path>'
```

### 4. バッチのクローズ（Close Batch）

すべての実行結果と処置記録を集約してクローズします：

```sh
kander review aggregate <作業ディレクトリ> <batch-id> > /tmp/batch-view.json
kander review close <作業ディレクトリ> <クローズリクエスト.json>
```

```json
{
  "batch_id": "batch-one",
  "expected_revision": 2,
  "view_hash": "<aggregate 出力の SHA-256>",
  "author": "coordinator",
  "roles": {
    "PMQA": {
      "run_id": "pmqa-fixed",
      "passed_at": "<完全な-sha>",
      "basis": "規約と品質判定を1項目ずつ検証"
    }
  },
  "resolved_failures": {},
  "opinions": []
}
```

---

## 6. 完了ゲートの検証基準

`kander move <task-id> done` 実行時の厳格な検証項目：
1. **計画の封印**：レビュー計画が `"sealed": true` であること。
2. **全バッチの完了**：すべてのバッチが有効なロール結論でクローズされていること。
3. **ブロッキング障害なし**：すべてのブロッキング指摘が `fixed` または正当な `waived` であること。
4. **Git 祖先関係**：合格コミットおよび修正コミットが最終納品コミットの直系の祖先であること。
5. **アーティファクトの整合性**：ローカル台帳のハッシュとコピーが完全一致していること。
