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

`kander move <id> working --owner <agent> --decision <reference>` によって `STARTED_AT` が変更された場合、計画全体の状態は `requirements-needed` になります。`review/` からの回収には `--decision` が必要です。
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

メンバーの全バッチがクローズした後に再バインドすると、新しい実行サイクルをレビューするバッチがありません。計画は `cycle_batch_from` にメンバーと再バインド時のバッチ数を記録し、`kander move <id> done` は拒否します。`kander review progress` は `pending` に `cycle-batch:<task-id>` を、`task_id` 付きのアクション `append-cycle-batch` を示します。`extend-plan` の `batch` でバッチを 1 つ追加してください。封印済みの計画は、そのようなメンバーを含むバッチだけを受け付けます。このバッチも通常のチェーン規則に従います。`previous_batch_id` は最後のバッチで、`base` はそのクローズ時の目標です。実際にレビューするか、明示的な N/A の手順で全ロールを `N/A: <理由と規則の根拠>` にできます。メンバーのバッチが開いている間の再バインドには、新しいバッチは不要です。そのバッチは新しいサイクルでクローズするためです。

`done/` のメンバーは変わりません。他のメンバーの後続バッチ、そのメンバーを含まない未クローズのバッチ、他のメンバーの再バインドは、`check`、coordinator のミラー、完了済み wrap-up を変更しません。完了済み wrap-up は当時の計画の終点に結び付いたままです。

### メンバー除外（Member Exclusion）

`kander move` が plan または task group のメンバーを `archived/` か `trash/` に移すと、同じトランザクションで `exclusions/<task-id>.json` を書きます。内容は除外 ID、計画、task group、移動元と移動先の状態、結果、実行サイクル、理由、決定の参照です。このレコードを書くのはこの move だけで、どちらの状態にも戻り道がないため、レビューを逃れるために偽造できません。`done/` から `archived/` への移動は書きません。`done/` から `trash/` への移動は書きます。そのカードはもう納品済みとして数えないためです。

- 計画、バッチ、クローズ、割り当て、解釈、レガシーマップの書き込みは、除外メンバーと全アーカイブ済みメンバーのコピーをスキップし、検証もそれらのコピーを比較しません。既存のコピーはそのまま残ります。
- ゲートの免除は除外メンバーだけです。その実行サイクルは再バインド不要で、完了の検証は残りのメンバーを対象にします。`done/` の後にアーカイブされたメンバーは自身の事実を保ち、免除されません。
- 新しい run はバッチに残るメンバーを指定します。除外前に準備された run は除外メンバーにコピーを書かず（`skipped_members`）、他のメンバーについて確定します。メンバーのいないバッチはクローズも阻止もしません。チェーンはそれを飛び越え、wrap-up の証拠はメンバーのいる最後のバッチで終わります。
- 除外メンバーに割り当てられた finding は、`withdrawn` レコードが書かれるまで集約、前進、クローズを阻止します（「ステータス遷移ルール」を参照）。新しい割り当ては除外メンバーを拒否します。
- `kander coordinator reconcile` は move が書いた終了状態だけを受け付けます。`RESULT: completed` でアーカイブされたメンバーには `done/` の事実である `FINISHED_AT` が必要です。保存された実行チェーンを持つメンバーには、状態、結果、実行サイクルがカードと一致する除外レコードが必要です。保存されたチェーンのないメンバーは結果フィールドのみ検証します。

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
| `withdrawn` | 除外メンバーのみ | いいえ | オーケストレーターが書きます。`withdrawal.exclusion_id` と `withdrawal.decision` が必要。修正としては数えません。 |

`withdrawn` レコードは、除外カードのリビジョンを使って `kander review disposition` で提出します。オーケストレーターは `kander review progress <task-id>` から `exclusion_id` を取得します。その `pending` には未処理の `withdrawal:<run>/<finding>/<task>` も示されます。アーカイブ済みカードには書き込めないため、原本は台帳に保存します。集約ビューは `runs[].withdrawn` で、作成者の処置と分けて示します。

---

## 5. 前進、増分レビュー、バッチクローズ

### 1. バッチ目標の前進（Advance）

修正コミット完了後、バッチの目標コミットを前進させます：

```sh
kander review advance <絶対作業ディレクトリ> <前進リクエスト.json>
```

リクエストには `{batch_id, expected_revision, advance: {previous_target, target, reason, deliveries, foreign_commits}}` を含めます。Git 作業ツリーがクリーンである必要があります。

計画済みの後続バッチがある場合、どちらの入口も拒否します。後続バッチの base は変更できず、このバッチのクローズ時の目標と一致する必要があるためです。エラーは後続バッチを示します。`extend-plan` の `drop_batches` で削除してください。受け付けるのは run、前進、クローズのない末尾のバッチだけです。封印済みの計画は再び開き、バッチのレコードは残るため、同じ ID を別の base で戻すことはできません。クローズ後に新しい ID で次のバッチを追加し、再度封印してください。

このコマンドと `--advance-file` 付きの増分レビューは、割り当て済み finding に作成者の処置がない間は拒否します。機械的な項目も数えます。エラーは各 `task/run/finding` を示し、バッチも計画も変わりません。レコードのない割り当て済み finding は修正ラウンドが終わっていないことを意味し、目標の前進はそれを追い越してしまいます。

修正ディスパッチは通常 base 上の run に結び付きます。旧バイナリが修正ラウンドの終了前に前進させたバッチは回復できます。ディスパッチの base は現在の目標と一致し、参照する run はバッチの前進チェーン上の以前の目標にあってかまいません。チェーンは `batch.advances` のみから得られ、Git にはアクセスしません。

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

## 5.5 並行重複とルート実行の取り下げ

必須ロールの成功した実行はすべて結論チェーン（結論の実行とその `previous_run_id` の祖先）上になければなりません。唯一の例外は任意の `superseded_runs` に載せた実行で、並行重複または取り下げたルート実行です。

```json
"superseded_runs": {
  "pmqa-twin": {"replacement": "pmqa-fixed", "basis": "pmqa-first と同じコミットで並行起動"},
  "pmqa-old-root": {"replacement": "pmqa-fixed", "basis": "元の reviewer は使用不可", "decision": "<ユーザー決定の参照>"}
}
```

- `decision` がない場合、受け付けるのは並行重複だけです。チェーン上に同じコミット、reviewer、前任を持つ実行があるか、前任が取り下げ済みで、結論の実行と同じコミットと reviewer を持つ場合です。
- `decision` がある場合、チェーンに入れないルート実行と、前任がそのようなエントリを持つ実行も取り下げられます。各実行に個別のエントリが必要です。結論チェーンから分岐した実行は対象外です。Kander は参照を記録するだけで検証しません。
- 各エントリは、取り下げた実行のコミットが代替実行のコミットの祖先であるという Git の主張を追加します。
- エントリはクローズ記録に残ります。取り下げた実行の原本、finding、失敗レコードは残り、クローズを拘束し続けます。

---

## 6. 完了ゲートの検証基準

`kander move <task-id> done` 実行時の厳格な検証項目：
1. **計画の封印**：レビュー計画が `"sealed": true` であること。
2. **全バッチの完了**：すべてのバッチが有効なロール結論でクローズされていること。メンバーのいないバッチは除きます。全バッチのクローズ後に再バインドされたメンバーには後続のバッチが必要です。
3. **ブロッキング障害なし**：すべてのブロッキング指摘が `fixed` または正当な `waived` であること。
4. **Git 祖先関係**：合格コミットおよび修正コミットが最終納品コミットの直系の祖先であること。
5. **アーティファクトの整合性**：ローカル台帳のハッシュとコピーが完全一致していること。除外メンバーとアーカイブ済みメンバーのコピーは比較しません。
