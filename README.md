# devbox

特定の相手に、一時的にファイルを渡すためのツールです。相手ごとにこちらでユーザー名・パスワード・専用ディレクトリを指定しておき、そのディレクトリに置いた `index.md`（案内文）とファイルを、ログインした相手だけに見せることができます。Cloudflare Tunnel を使って一時的に公開して使うなどすると便利です。

## 主な機能

- こちらが指定したユーザー名・パスワードによるログイン認証（Cookieセッション）
- 相手ごとの `index.md` を案内ページとして表示
- ページ内のリンクから、相手専用ディレクトリに置いたファイルを渡せる（パストラバーサル対策込み）
- `log/slog` による構造化ログ（`text` / `json` 切り替え、ファイル出力、Cloudflare ヘッダー記録対応）

## 必要要件

- Go 1.27 以上

## セットアップ / ビルド方法

```bash
go build -o devbox .
./devbox .
```

引数には `devbox.toml` と `contents/` を格納した作業ディレクトリを指定します。省略した場合はカレントディレクトリ（`.`）が使われます。

```bash
go run . /path/to/work
```

ヘルプやバージョンを表示するには `-h` / `--help`、`-v` / `--version` を指定します。

```bash
./devbox --help
./devbox ./work --help
./devbox --version
```

`VERSION` ファイルを読み込んでバージョンを埋め込む場合は `Makefile` を利用します。

```bash
make build   # build/darwin/devbox を生成
make linux   # build/linux/devbox を生成
```

## devbox.toml の書き方

```toml
addr = ":8080"
session_days = 7
log_file = ""
log_format = "text"
cloudflare = false

[[users]]
user = "demo"
pass = "demo"
directory = "demo"
```

| キー | 説明 | 省略時のデフォルト |
| --- | --- | --- |
| `addr` | 待受アドレス | `:8080` |
| `session_days` | セッションの有効日数（全ユーザー共通） | `14` |
| `log_file` | ログ出力先ファイル（絶対パスを指定。空なら標準出力） | `""`（標準出力） |
| `log_format` | ログ形式（`text` または `json`） | `text` |
| `cloudflare` | `true` のとき `CF-Connecting-IP`・`CF-IPCountry`・`CF-Ray` を記録 | `false` |
| `[[users]]` | ログインユーザーの定義（複数可） | 必須（1件以上） |
| `users.user` | ログインユーザー名 | - |
| `users.pass` | ログインパスワード | - |
| `users.directory` | `contents/` 配下のユーザー専用ディレクトリ名 | - |

## ディレクトリ構成

```text
<作業ディレクトリ>/
  devbox.toml
  contents/
    <directory>/
      index.md        # トップページとして表示されるMarkdown
      <任意のファイル>  # /download/<ファイル名> で相手に渡せる
```

`directory` には `devbox.toml` の `users.directory` で指定した値を使います。

## 使い方

1. サーバーを起動し、ブラウザで `http://<addr>/login` にアクセス
2. `devbox.toml` に設定したユーザー名・パスワードでログイン
3. ログイン後、そのユーザーの `contents/<directory>/index.md` が変換されたページが表示される
4. `index.md` 内のリンクから `/download/<ファイル名>` にアクセスすると、`contents/<directory>/` 直下のファイルをダウンロードできる

## 既知の制約

- パスワードは `devbox.toml` に平文で保存されます。リポジトリに含めて公開しないよう注意してください
- セッションはインメモリで管理されており、サーバーを再起動すると全セッションが失効します
- 現状、表示できるページは `index.md` 1ページのみです。`index.md` から他の `.md` ファイルへリンクしても、Markdown として変換表示されることはありません（`/download/` はファイルをそのまま渡すだけです）
