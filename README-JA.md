# Kander

[English](README.md) | [简体中文](README-CN.md) | **日本語**

[![Kander - 複数 AI Agent のカンバン調整](docs/star-please.png)](https://github.com/dualface/kander)

一人でカンバンを使って複数の AI Agent をスケジュールする。

![Kander ワークフロー](docs/workflow-ja.svg)

## 1. クイックスタート

実行には Git と、Codex、Claude、Grok、Cursor のうち少なくとも 1 つが必要です。

[Releases](https://github.com/dualface/kander/releases) から最新の kander バイナリをダウンロードしてそのまま実行してください。初回起動時にまだインストールされていなければ対話ウィザードが始まります。

インストールが完了すればすぐに使えます。

4 ステップで始められます:

1. Agent セッションを新規に開き、そこで要件やタスクを議論し、目標と受け入れ条件を明確にします。Agent の Plan モードの利用を推奨します。
2. タスクが確定すると、Agent がカンバンフローでタスクを起動するか確認します。承認すればタスクは自動的に起動します。
3. 要件が複数ある場合は、要件ごとにステップ 1-2 を繰り返し、継続的にタスクを手配・起動します。
4. コマンドラインインターフェースでタスクの状態を確認します:

```sh
kander
```

![ターミナルカンバン](docs/kanban-screenshot-01.png)

> 上図のカンバンの内容は私の実プロジェクト [https://quicktui.ai](https://quicktui.ai) のものです。QuickTUI はコンピュータ上のさまざまな Agent をリモート操作するツールで、iOS/Android/macOS/Linux/Windows に対応し、無料で使えます。

さらに詳しく: スライド [タスクを効率的に進める方法](docs/how-to-advance-tasks-efficiently-ja.pdf) (PDF) を参照してください。

## 2. ライセンス

本プロジェクトは MIT License を使用しています。[LICENSE](LICENSE) を参照してください。
