# タスク永続ディスパッチプロトコル

永続ディスパッチ（Durable Dispatch）プロトコルは、非同期 Agent 協調において、最大 1 回の確実な配信保証、決定論的な受領追跡、および監査可能な完了レシートを提供します。

ディスパッチのアイデンティティは `dispatch_id` によって一意に決定され、端末のエコーやカラムの変更から曖昧に推測されることはありません。

---

## 1. ディスパッチライフサイクルとステートマシン

```text
[kander notify / dispatch prepare]
               |
               v
         +-----------+
         |  prepared | (意図がディスクへ永続保存)
         +-----------+
               |
               v
     +------------------+
     | delivery-unknown | (端末への送信を試行)
     +------------------+
               |
      (kander move working --dispatch-id --execution-epoch)
               |
               v
         +-----------+
         |  accepted | (実行者が受領確認；120秒の受領期限内)
         +-----------+
               |
      (kander move review / done --delivery-commit)
               |
               v
         +-----------+
         | completed | (完了レシートがカード状態・版数と共にコミット)
         +-----------+
```

1. **`prepared`**：意図が `dispatches/<id>/intent.json` に永続化された状態。
2. **`delivery-unknown`**：端末に送信された状態。クラッシュ等が発生しても指示ファイルは安全に維持されます。
3. **`accepted`**：実行側 Agent が明示的に `kander move <id> working --dispatch-id <id> --execution-epoch <epoch>` を実行した**時にのみ**生成されます。
4. **`completed`**：作業が完了し、`review` または `done` へ遷移した際に不可逆なレシートが確定します。

---

## 2. ディスパッチの分類と証拠のバインド

すべてのディスパッチは `kind` を宣言し、セマンティックな証拠を添付する必要があります：

```sh
# 修正ディスパッチの送信
kander notify <task-id> --kind fix --base <40桁-SHA> \
  --evidence-file <証拠ファイル.json> --message-file <UTF8メッセージ>

# 未受領ディスパッチの再送
kander resume <task-id> --dispatch-id <id> --message-file <UTF8メッセージ>

# ディスパッチ状態の確認
kander dispatch show <task-id> <id>
```

| 種別 | 状態遷移 | 必要なバインド証拠 |
|---|---|---|
| `fix` | `working` $\to$ `review` | バッチ ID、指摘 ID、既存の作成者処置記録。 |
| `sync` | `working` $\to$ `review` | ブランチのリベースや同期用。レビュー指摘のバインドは不可。 |
| `wrap-up` | `working` $\to$ `done` | レビュー完了コミット、Git 統合コミット、develop 参照。 |

---

## 3. `fix` レビュー証拠のバインド

修正ディスパッチは、実行者が指定された構造化指摘に対して正確に対応することを強制します：

```json
{
  "fix": {
    "batch_id": "batch-one",
    "findings": [
      {
        "run_id": "pm-round-two",
        "finding_id": "PM-02",
        "previous_run_id": "pm-round-one"
      }
    ],
    "authors": [
      {
        "finding": {"run_id": "pm-round-one", "finding_id": "PM-01"},
        "record_id": "author-one",
        "author": "codex",
        "artifact": {
          "task_id": "20260908-example-task",
          "path": "reviews/pm-round-one/dispositions/author-one.json"
        }
      }
    ]
  }
}
```

- **系譜の正当性**：指摘が指定バッチに実在し、自カードに割り当てられていることを自動検証。
- **冪等性**：同一 `dispatch_id` でのリトライは既存バインドを継承。同一 ID でのバインド改ざんはエラーとなります。

---

## 4. `wrap-up` 統合バインドと代理後片付け（Wrap-up on Behalf）

### 通常の Wrap-up

レビュー済みの最終コミットと develop 統合コミットをバインドします：

```json
{
  "wrap_up": {
    "git": {
      "cwd": "/absolute/worktree",
      "source_commit": "<40桁レビュー完了-SHA>",
      "target_commit": "<40桁develop統合-SHA>",
      "target_ref": "refs/remotes/origin/develop",
      "author": "coordinator",
      "basis": "ユーザー承認によるプッシュおよびローカル同期完了"
    }
  }
}
```

システム検証：
1. `source_commit` が封印されたレビュー計画の最終コミットと一致すること。
2. `source_commit` が `target_commit` の祖先であること。
3. `target_commit` が指定の `target_ref` に含まれること。

### 代理 Wrap-up 例外（プロセス停止時）

元の実行 Agent が異常終了した際、コーディネーターは専用エポックを申請して代理処理を行えます：

```sh
kander dispatch authorize-wrap-up <リクエスト.json>
```

- **停止確認**：プロセスプローブにより対象プロセスが確実に停止していることを検証。
- **権限の厳格な制限**：ワークスペースの掃除と `## WRAP_UP_RECORDS` の追記**のみ**が許可され、コード変更や処置記録の偽造は遮断されます。

---

## 5. 実行者側のアトミックレシート

実行 Agent はアトミックレシートコマンドを用いて作業を受領および完了します：

```sh
# 1. 受領確認
kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>

# 2. spec 更新（実行権限も同時検証）
kander update <task-id> --document spec.md --file <ファイル> \
  --expect-revision <版数> --dispatch-id <id> --execution-epoch <epoch>

# 3. 修正完了の提出
kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> \
  --delivery-commit <修正-sha> --disposition <添付相対パス>

# 4. wrap-up 完了の提出
kander move <task-id> done --result completed --dispatch-id <id> \
  --execution-epoch <epoch> --delivery-commit <統合-sha>
```

- **リプレイセーフ**：受領コマンドの再試行は `replayed: true` と元のレシートを返し、余計な処理や版数増加を起こしません。
- **認可ガード**：不整合なディスパッチ ID や古いエポックでは状態遷移をコミットできません。
