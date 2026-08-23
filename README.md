# devbox

特定の相手に、一時的にファイルを渡すためのツールです。相手ごとにこちらでユーザー名・パスワード・専用ディレクトリを指定しておき、そのディレクトリに置いた `index.md`（案内文）とファイルを、ログインした相手だけに見せることができます。Cloudflare Tunnel を使って一時的に公開して使うなどすると便利です。

## 主な機能

- こちらが指定したユーザー名・パスワードによるログイン認証（Cookieセッション）
- 相手ごとの `index.md` を案内ページとして表示
- ページ内のリンクから、相手専用ディレクトリに置いたファイルを渡せる（パストラバーサル対策込み）

## 必要要件

- Go 1.27 以上

## セットアップ / ビルド方法

```bash
go build -o devbox .
./devbox devbox.toml
```

設定ファイルのパスを省略した場合は、カレントディレクトリの `devbox.toml` が使われます。

```bash
go run . devbox.toml
```

## devbox.toml の書き方

```toml
data_dir = "data"
addr = ":8080"
session_days = 7

[[users]]
user = "demo"
pass = "demo"
directory = "demo"
```

| キー | 説明 | 省略時のデフォルト |
| --- | --- | --- |
| `data_dir` | ユーザーデータを格納するルートディレクトリ | `data` |
| `addr` | 待受アドレス | `:8080` |
| `session_days` | セッションの有効日数（全ユーザー共通） | `14` |
| `[[users]]` | ログインユーザーの定義（複数可） | 必須（1件以上） |
| `users.user` | ログインユーザー名 | - |
| `users.pass` | ログインパスワード | - |
| `users.directory` | `data_dir` 配下のユーザー専用ディレクトリ名 | - |

## ディレクトリ構成

```
data/
  <directory>/
    index.md        # トップページとして表示されるMarkdown
    <任意のファイル>  # /download/<ファイル名> で相手に渡せる
```

`directory` には `devbox.toml` の `users.directory` で指定した値を使います。

## 使い方

1. サーバーを起動し、ブラウザで `http://<addr>/login` にアクセス
2. `devbox.toml` に設定したユーザー名・パスワードでログイン
3. ログイン後、そのユーザーの `data/<directory>/index.md` が変換されたページが表示される
4. `index.md` 内のリンクから `/download/<ファイル名>` にアクセスすると、`data/<directory>/` 直下のファイルをダウンロードできる

## 既知の制約

- パスワードは `devbox.toml` に平文で保存されます。リポジトリに含めて公開しないよう注意してください
- セッションはインメモリで管理されており、サーバーを再起動すると全セッションが失効します
- 現状、表示できるページは `index.md` 1ページのみです。`index.md` から他の `.md` ファイルへリンクしても、Markdown として変換表示されることはありません（`/download/` はファイルをそのまま渡すだけです）
