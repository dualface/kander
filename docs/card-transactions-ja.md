# カードトランザクション、制御された更新、クラッシュリカバリ

本文書では、Kander のカードストレージモデル、ファイルレベルのトランザクションプロトコル、ロック順序階層、およびクラッシュリカバリの保証について定義します。

タスクの唯一の識別子は `task_id` です。ファイルパスは実行時の検索結果に過ぎません。`internal/board` はロックを保持した状態でパスを再解決し、非同期処理の完了後にキャッシュされた古いパスへ無条件に書き込むことは決してありません。

---

## 1. コア原則とトランザクションコマンド

すべてのカード変更は、制御された CLI コマンドまたは `board.WithTransaction` Go API を通じて実行されます：

```sh
# 詳細確認とドキュメント更新
kander show --json <task-id>
kander update <task-id> --document spec.md --file <UTF8ファイル> --expect-revision <版数>
kander update <task-id> --document plan.md --file <UTF8ファイル> --expect-revision <版数>

# ライフサイクル状態遷移
kander move <task-id> working --owner <agent>
kander move <task-id> done --result completed
kander move <task-id> archived --result cancelled --reason <理由> --decision <決定参照>
kander move <task-id> trash --result trashed --reason <理由> --decision <決定参照>
```

### 重要な不変条件

1. **ディレクトリカード構造**：すべてのカードはディレクトリ形式で保存され、内部に `spec.md` と任意の添付ファイルを持ちます。
2. **楽観的排他制御（CAS）**：変更には `--expect-revision` が必須です。書き込みが成功するごとに版数が 1 ずつインクリメントされます。
3. **状態の唯一の事実源**：タスクの状態は、それが存在する物理ディレクトリ（`backlog/`, `todo/`, `working/`, `review/`, `done/`, `archived/`, `trash/`）のみによって決定されます。
4. **契約の凍結**：`todo` を超えて移動した後は、タスクサイズ、グループ所属、コア契約の変更が禁止されます。変更には `--contract-decision-file` による決定記録が必須です。

---

## 2. ロック階層と分離性

複数タスクの並行処理によるデッドロックを防ぐため、ロックは以下の厳格な順序で取得されます：

```text
1. board.lock          (ボード全体ロック：読み書き共有、作成/移動/復旧時は排他)
       |
       v
2. group locks         (グループロック：group ID の辞書順)
       |
       v
3. task locks          (タスクロック：task ID の辞書順)
       |
       v
4. journal.lock        (ジャーナルロック：極短時間の排他、pending->committed の移動とクリーンアップ)
```

### 制御ファイルレイアウト（`kanban/.kander/`）

| パス | 用途 |
|---|---|
| `locks/board.lock` | グローバルボード排他ロック（POSIX `flock` / Windows `LockFileEx`）。 |
| `locks/<task-id>.lock` | タスクごとの安定したハンドル。リーダーは共有、ライターは排他。 |
| `locks/journal.lock` | WAL のアトミック公開およびパーティション移行を保護。 |
| `versions/<task-id>.json` | `{revision, operation_id, contract_frozen}` を保持。 |
| `operations/pending/` | 未コミットのトランザクションログを保管。 |
| `operations/committed/` | コミット済みの履歴ログを保管。 |

---

## 3. 2相コミット（2PC）とクラッシュリカバリ

複数ファイルにまたがる更新は、先行書き込みログ（WAL）を用いた 2 相コミットで処理されます：

```text
[1. 準備フェーズ - Prepare]
   操作記録を書き出し -> operations/pending/<operation-id>.json (phase: "prepared")
   ディスクへ同期 (internal/fs によるアトミック置換)

[2. 適用フェーズ - Apply]
   添付ディレクトリ作成 -> ファイル書き込み -> ディレクトリ移動 -> バージョン更新

[3. コミットフェーズ - Commit]
   journal.lock 排他保護下で：
   レコードを phase: "committed" にアトミック書き換え
   アトミックなリネーム：pending/<id>.json -> committed/<id>.json
```

### クラッシュシナリオと `kander init` による復旧

プロセスキル等で中断された場合：
- **`"prepared"` の pending レコード**：通常の読み取りコマンドは `pending-recovery` エラーを返し、処理を中断します。リーダーが自動修復することはありません。
- **復旧コマンド**：`kander init` が排他的 `board.lock` を取得し、pending レコードを解析して未完了の操作を最後まで再実行します。
- **`"committed"` の pending レコード**：実データと版数はコミット済みでリネームのみ未完了の状態です。`kander init` はリネームのみを完了させます。

---

## 4. ジャーナルの保持とパーティション管理

- **コミット済みログの保持**：コミットおよび復旧の完了後、`journal.lock` 下でベストエフォートなクリーンアップが走ります。定数 `committedJournalRetention = 100` により最新の 100 件が保持されます。
- **移行データの保護**：`.kander/migrations/<operation-id>` が存在するログは、100 件の制限に関わらず保持されます。
- **パーティション構成**：最新の Kander はログを `pending/` と `committed/` に分割します。旧形式のフラットなファイルが見つかった場合は `kander init --maintenance` を促します。

---

## 5. レビューおよび永続ディスパッチの拡張

1. **バイナリアーティファクト**：`Transaction.PutBytes` によりバイナリ添付ファイルを完全保存します。ログ内では `"binary": true` と base64 で記録されます。
2. **ディスパッチ認証**：ディスパッチ（Dispatch）からの更新には `--dispatch-id` と `--execution-epoch` が必須となり、カード版数と実行権限の両方が検証されます。
