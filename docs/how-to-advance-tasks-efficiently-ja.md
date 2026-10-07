# タスクを効率的に進める方法: kander を使って

著者: [dualface](https://github.com/dualface)

[English](how-to-advance-tasks-efficiently-en.md) | [中文](how-to-advance-tasks-efficiently-cn.md)

## kander とは?

- Markdown で定義するタスクカードシステム.
- ルール駆動のタスクオーケストレーションシステム.
- 使いやすい TUI も付属.

## インストール方法

1. <https://github.com/dualface/kander> から最新版をダウンロード.
2. `kander install` を実行.
3. プロジェクト内で `kander init` を実行し, `./kanban/` ディレクトリ構造を作成.

## タスクカードはどこから来るのか?

1. ユーザーが Agent と要件を話し合う, または Agent に GitHub issue の確認を依頼する.
2. ユーザーと Agent が対話し, 完了すべきタスクを確定する.
3. ユーザーが Agent にタスクカードの作成を依頼する.

## タスクカードには何があるのか?

- タスクの複雑さに応じて, タスクカードは 1 つ以上の Markdown ファイルで構成される.
- すべてのタスクカードはプロジェクトの `./kanban/` ディレクトリに保存される.
- Markdown にはタスク完了に必要な情報がすべて含まれる:
  - タスクの説明
  - タスクリスト
  - 受け入れチェックリスト

## タスクカードの開始方法?

- Agent は通常, タスクカードを開始するかを自ら確認する.
- ユーザーが明示的に Agent に開始を指示することもできる.

## タスクカードはどのように実行されるのか?

- Agent はルールに従い, `kander start` コマンドで別の Agent を起動し, カードに記述されたタスクを実行させる.
- ユーザーは `kander` コマンドで TUI を起動し, すべてのタスクの状態を確認できる.

## TUI

![kander TUI ボード: Backlog / Todo / Working / Review / Done の 5 列](images/how-to-advance-tasks/board.png)

## 実行 Agent の調整

- タスクの規模ごとに異なる Agent とモデルを割り当てる.
- 品質とコストのバランスを取る.

![実行設定: 大タスクと小タスクごとの Agent, モデル, 推論レベル](images/how-to-advance-tasks/execution-agents.png)

## レビュー Agent の調整

- レビュー役割ごとに異なる Agent とモデルを割り当てる.
- クロスレビューでより良い結果が得られる.

![レビュー設定: レビュー役割ごとの Agent, モデル, レビュー段階](images/how-to-advance-tasks/review-agents.png)

## 必要に応じてルールモジュールをカスタマイズ

- フルワークフローが最も高い品質をもたらす.
- 不要なルールモジュールは無効化できる.

![ルールモジュール設定: ワークフロープリセットと各モジュールのスイッチ](images/how-to-advance-tasks/rule-modules.png)

## オープンソース, 監査可能, カスタマイズ可能

- ルール定義を十分に監査する.
- カスタムルールの方が優先度が高い.
- kander のルールモジュールを無効化し, 自分のルールで置き換える.

## ありがとうございました

<https://github.com/dualface/kander>

Star をお願いします ;-)
