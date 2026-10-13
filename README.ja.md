# diff-tour

シニアエンジニアの読み方で、差分をたどる。

[English](README.md) | **日本語**

[![Release](https://img.shields.io/github/v/release/init-co-jp/diff-tour)](https://github.com/init-co-jp/diff-tour/releases)
[![CI](https://github.com/init-co-jp/diff-tour/actions/workflows/ci.yml/badge.svg)](https://github.com/init-co-jp/diff-tour/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

![クーポンを足す変更を tour で読んでいるところ](demo/demo-ja.gif)

<sub><a href="https://github.com/init-co-jp/diff-tour-example/tree/feature/coupons">クーポンを足すブランチ</a>のツアーです。何のための変更かを確かめ、利用回数の上限をどう守っているかを見て、問いを考え、答えを読みます（同じ注文に同じクーポンを 2 回当てると、2 回分使われてしまう）。</sub>

> [!NOTE]
> diff-tour は公開したばかりです。ツアーの形式や画面は、これからも変わることがあります。

## クイックスタート

git と、[Claude Code](https://claude.com/claude-code)、[Codex CLI](https://github.com/openai/codex)、[Gemini CLI](https://github.com/google-gemini/gemini-cli) のどれか 1 つ（ログイン済みのもの）が必要です。

```sh
curl -fsSL https://raw.githubusercontent.com/init-co-jp/diff-tour/main/install.sh | sh
cd your-repo
tour
```

実行すると、次の順に進みます。

1. **設定の画面**（初回だけ）：表示の言語と、ツアーを書かせる LLM を選びます。どちらもおすすめが選ばれているので、Enter を 2 回押せば次に進みます。
2. **変更の一覧**：ツアーにできる変更が並びます。いまいるブランチが選ばれているので、Enter を押します。
3. **ツアーを書く**：LLM がツアーを書きます。2〜5 分かかります。
4. **ツアーを見る**：**→** で進み、**←** で戻ります。**q** で終わります。

`tour` が見つからないと言われたら、インストールのスクリプトが表示した 1 行をシェルの設定に足してください。

ほかの入れ方は「[インストール](#インストール)」に、試しに使える例のリポジトリは「[初回の流れ](#初回の流れ)」にあります。

## どんなツールか

AI に頼めば、動くコードはできます。でも、なぜそう書いたのか、レビューする人が何を気にするのかまでは教えてくれません。

`tour` は、git の変更を、**→** と **←** の 2 つのキーで進むツアーにします。ツアーが見せるのは、シニアエンジニアが変更を読むときに見ているところです。

- **どう読むか**：何のための変更か、どこに時間を使うか、次にどのファイルへ飛ぶか（変更していないファイルも含む）。
- **何を問うか**：変更前のコードが守っていた約束を守っているか。失敗したら、2 回実行されたらどうなるか。安全に出せて、戻せるか。誰が、どんな入力で呼べるか。データが 100 倍になったらどうなるか。次の変更は楽になったか。コードが壊れたら、テストは本当に失敗するか。
- **どう判断するか**：どれが直すべきもので、どれが提案や細かい指摘か。危なそうに見えるが問題ないのはどこで、なぜか。良くできているのはどこか。全体として、コードベースを良くしたか。
- **どこで「待てよ」と手を止めるか**：問いとして出すので、答えを見る前に自分で考えられます。

この見方は、経験を積んだエンジニアがコードの読み方やレビューについて書き残してきたことを、[`internal/generate/lessons`](internal/generate/lessons) にまとめたものに基づきます。LLM はツアーを書くたびに、これを読みます。

ツアーを書くのは、ふだん使っている LLM（Claude Code、Codex CLI、Gemini CLI）です。`tour` は読み取り専用で、ファイルもリポジトリも書き換えません。

## 仕組み

```text
 your repository                          cache directory (deleted after writing)
 ───────────────                          ──────────────────────────────────────
 git archive <base>  ──────────────────▶  base/          files before the change
 git archive <head>  ──────────────────▶  head/          files after the change
 git diff            ──────────────────▶  change.diff
 git log             ──────────────────▶  commits.txt
 (built into tour)   ──────────────────▶  lessons/       what senior engineers have learned
                                                 │
                                                 ▼
                      Claude Code / Codex / Gemini, started in that directory
                      with read-only permissions. It reads, then writes the
                      tour as JSON.
                                                 │
                                                 ▼
 tour checks every file and line the JSON points to. If something is wrong,
 it asks the LLM to fix only that (up to 2 times). The tour is saved.
                                                 │
                                                 ▼
 tour shows the tour. The code on screen is read from git at the right commit,
 never copied from the LLM's reply.
```

1. **書き出す。** `tour` は `git archive`、`git diff`、`git log` でリポジトリを読み、キャッシュの中に作った新しいディレクトリへファイルを書き出します。書き出すのは git が追跡しているファイルだけです。未コミットの変更をツアーにするときは、無視されていない未追跡のファイルも書き出します。`.gitignore` にあるファイル（`.env` など）は書き出しません。
2. **書かせる。** そのディレクトリで LLM のコマンドラインツールを起動し、決まったプロンプトと読み取り専用の権限（下で説明します）で動かします。2〜5 分かかり、その間は LLM が読んでいるファイルが画面に流れます。
3. **確かめる。** ツアーは、ファイルと行番号を指す JSON です。`tour` は、指している場所が実在するかを 1 つずつ確かめます。誤りがあれば、そこだけを LLM に直させます（最大 2 回）。
4. **見せる。** 画面に出すコード、差分、スキーマは git から読みます。そのため、目にするコードは必ず、そのコミットの本物のコードです。リポジトリにないコード（素朴な書き方や、修正案の例）には、必ず「**例**」と付けます。

書き出したディレクトリは、ツアーを書き終えたら消します。できたツアーはキャッシュに保存するので、2 回目からはすぐに開けます。

## LLM にできること、できないこと

コードを LLM に渡すのは、軽く決められることではありません。何が起きるのかを、そのまま書きます。

**選んだ LLM に送られるもの：**

- 比べる 2 つのコミットでのリポジトリのファイル、差分、コミットメッセージ。git が追跡しているファイルだけです（未コミットの変更をツアーにするときは、無視されていない未追跡のファイルも含みます）
- [`internal/generate/prompts`](internal/generate/prompts) にあるプロンプト
- <kbd>?</kbd> を押したとき：あなたの質問と、ツアーの全体（ファイルは上と同じもの）

送り先は、そのツールの提供元（Anthropic、OpenAI、Google）で、あなたのアカウントの利用規約に従います。`tour` 自身はネットワークに一切つながず、サーバーも持たず、利用状況も集めません。

**LLM にはできないこと：**

- 書き出したディレクトリの外にあるファイルを読む（本物の作業ツリー、`.git`、ホームディレクトリを含む）
- `.gitignore` にあるファイルを読む（そもそも書き出していません）
- ファイルを書く、書き換える、消す
- 何かを変えるコマンドを実行する、ネットワークにつなぐ
- MCP サーバー、プラグイン、拡張機能、Web 検索を使う

これはプロンプトでお願いしているだけではありません。`tour` が各ツールに渡すフラグで、仕組みとして禁じています。

<details>
<summary><b>Claude Code</b>：使える道具を Read、Grep、Glob だけにする</summary>

```text
claude -p --restricted --strict-mcp-config --tools Read,Grep,Glob
       --permission-mode dontAsk --permission-prompts none --no-session-persistence
```

- `--restricted`：コマンド実行と Web の道具を外し、ファイルの道具を作業ディレクトリの中に閉じ込める
- `--strict-mcp-config`：MCP サーバーを 1 つも読み込まない
- `--tools Read,Grep,Glob`：読むだけの 3 つの道具だけを残す
- `--permission-mode dontAsk`：許可が要る操作は、聞かずに断る
- `--no-session-persistence`：セッションを保存しない

ソース：[`internal/generate/claude.go`](internal/generate/claude.go)
</details>

<details>
<summary><b>Codex CLI</b>：書き出したディレクトリしか読めないサンドボックス</summary>

Codex は `cat` や `rg` などのコマンドを実行してファイルを読みます。そのため、守りの要は、そのコマンドを動かすサンドボックスです。`tour` はコマンドラインで専用の権限の設定を作ります。コマンドが読めるのは、書き出したディレクトリと、`/usr` や `/etc` などのシステムの場所だけです。どこにも書き込めず、ネットワークも使えません。

- `--ignore-user-config` と `--ignore-rules`：`~/.codex` の設定とルールを読まない。あなたの MCP サーバーやプラグインも読み込まれない
- `-c approval_policy="never"`：禁じられた操作は、許可を求めずに失敗させる
- `-c web_search="disabled"` と、apps、plugins、multi_agent、browser_use などへの `--disable`：Web 検索、外部サービスとの連携、サブエージェント、ブラウザの道具を外す
- `--ephemeral` と `-c history.persistence="none"`：セッションも履歴も保存しない

Linux（bubblewrap）で確かめました。macOS では Codex は Seatbelt という別のサンドボックスを使い、こちらはまだ確かめていません。
ソース：[`internal/generate/codex.go`](internal/generate/codex.go)
</details>

<details>
<summary><b>Gemini CLI</b>：読むための 4 つの道具だけを許す管理者ポリシー</summary>

- `--admin-policy`：一時ファイルのポリシーで、`read_file`、`glob`、`grep_search`、`list_directory` を、書き出したディレクトリの中に限って許し、それ以外の道具はすべて禁じる。管理者ポリシーは、あなた自身のポリシーより優先される
- 書き出したディレクトリに置く一時的な設定で、道具をこの 4 つだけにし、MCP の自動検出、サブエージェント、フック、チェックポイント、テレメトリを切る
- `--extensions none`：拡張機能を読み込まない。`--allowed-mcp-server-names` には、どのサーバーにもない名前を渡すので、MCP サーバーは 1 つも起動しない

Gemini CLI は、セッションを必ず `~/.gemini/tmp` に保存します（既定では 30 日後に消えます）。これはコマンドラインからは止められません。
ソース：[`internal/generate/gemini.go`](internal/generate/gemini.go)
</details>

それでも LLM はあなたのコードを読みます。コードの中の文章が、LLM の書く内容をねじ曲げようとすることはありえます（プロンプトインジェクション）。上の権限があるので、LLM がそれに従って何かを実行することはできません。とはいえ、ツアーはあくまで説明で、保証ではありません。書いてあることは、画面のコードと照らし合わせて確かめてください。

## インストール

**macOS と Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/init-co-jp/diff-tour/main/install.sh | sh
```

OS と CPU に合う最新のリリースを落とし、`checksums.txt` と照合してから、`tour` を `~/.local/bin` に置きます。その場所に `PATH` が通っていなければ、足すための 1 行を表示します。置き場所やバージョンを選ぶときは、次のようにします。

```sh
curl -fsSL https://raw.githubusercontent.com/init-co-jp/diff-tour/main/install.sh | TOUR_INSTALL_DIR=/usr/local/bin TOUR_VERSION=v0.1.0 sh
```

**Go 1.27 以上がある場合**（どの OS でも）

```sh
go install github.com/init-co-jp/diff-tour/cmd/tour@latest
```

`tour` は `$(go env GOPATH)/bin`（ふつうは `~/go/bin`）に入ります。

**Windows**（PowerShell。Windows ではまだ試していません）

```powershell
$tag = (Invoke-RestMethod https://api.github.com/repos/init-co-jp/diff-tour/releases/latest).tag_name
$zip = "diff-tour_$($tag.TrimStart('v'))_windows_amd64.zip"
$dir = "$env:LOCALAPPDATA\Programs\tour"
Invoke-WebRequest "https://github.com/init-co-jp/diff-tour/releases/download/$tag/$zip" -OutFile "$env:TEMP\$zip"
Expand-Archive "$env:TEMP\$zip" -DestinationPath $dir -Force
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
```

終わったら、新しい端末を開いてください。ARM の場合は `windows_arm64` にします。

入ったかどうかは `tour --version` で確かめられます。

## 初回の流れ

git と、次のどれか 1 つ（インストールしてログイン済みのもの）が必要です。

| LLM | 入っているかの確認 | ログイン |
| --- | --- | --- |
| [Claude Code](https://claude.com/claude-code) | `claude --version` | `claude` を一度起動する |
| [Codex CLI](https://github.com/openai/codex) | `codex --version` | `codex login` |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini --version` | `gemini` を一度起動する |

Gemini CLI への対応は試験的なものです。読み取り専用で動くことは本物の Gemini CLI で確かめましたが、本物の Gemini のモデルでツアーを最後まで作ったことはまだありません。うまく動かないときは [Issue](https://github.com/init-co-jp/diff-tour/issues) で教えてください。

あとは、リポジトリの中で実行するだけです。

```sh
cd your-repo
tour
```

初回だけ、表示の言語（端末の設定から選びます）と LLM を聞きます。そのあと、ツアーにできる変更が並びます。いまいるブランチが選ばれているので、Enter を押せば始まります。

自分のコードを使わずに試すなら、GIF と同じ例のリポジトリを使ってください。

```sh
git clone https://github.com/init-co-jp/diff-tour-example
cd diff-tour-example
git checkout feature/coupons
tour
```

## ツアーにする変更の選び方

| コマンド | ツアーにするもの |
| --- | --- |
| `tour` | 一覧から選んだブランチ（既定のブランチと比べる）。未コミットの変更があれば、それを**含めた**いまのブランチが先頭に来る |
| `tour <branch>` | そのブランチ。`feature/x` のほか、他の人のブランチを読むなら `origin/feature/x` |
| `tour <commit>` | そのコミット 1 つ |
| `tour <base>..<head>` | その範囲 |
| `tour <file>.json` | 保存したツアーのファイル（例：[`tours/example-coupons.ja.json`](tours/example-coupons.ja.json)） |
| `tour setup` | 言語と LLM を選び直す |

## キー

| キー | 動き |
| --- | --- |
| <kbd>→</kbd> <kbd>Space</kbd> <kbd>Enter</kbd> | 次へ |
| <kbd>←</kbd> <kbd>Backspace</kbd> | 戻る |
| <kbd>?</kbd> | この手順について質問する（候補から選ぶか、自分で書く）。答えは次の手順になる |
| <kbd>o</kbd> | この手順のコードをエディタで開く |
| <kbd>↑</kbd> <kbd>↓</kbd> | スクロール（手順が画面に収まらないときだけ） |
| <kbd>q</kbd> | 終了 |

## 手順に出てくるもの

- コードと差分（正しいコミットの中身を git から読み、構文に色を付ける）
- リポジトリに**ない**コード（素朴な書き方や、修正案の例）。必ず「**例**」と付ける
- 処理の流れや時系列の図
- 変更の前と後のデータベースのスキーマ（マイグレーションをメモリ上の SQLite に当てて求める。いまは SQLite のマイグレーションだけに対応）
- 例のデータの表
- 答えを見る前に考えるための問い（答えは次の手順に出る）
- 手順について自分で聞いた質問への答え。ツアーと一緒に保存される（ツアーのファイルを再生しているときは、終了するまでしか残らない）

どの説明にも、どうしてそう言えるのかを示すラベルが付きます。

| ラベル | 意味 |
| --- | --- |
| **確かめた** | このリポジトリのコード、テスト、コミットメッセージで裏付けがある |
| **読み取れる** | コードの作りから推し量れる |
| **シニアの見方** | 一般的な設計の判断で、このリポジトリでは確かめていない |

## 設定

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| 設定 | `~/.config/tour/config.json` | `~/Library/Application Support/tour/config.json` | `%AppData%\tour\config.json` |
| 保存したツアー | `~/.cache/tour/tours/` | `~/Library/Caches/tour/tours/` | `%LocalAppData%\tour\tours\` |

Linux では `XDG_CONFIG_HOME` と `XDG_CACHE_HOME` に従います。キャッシュは消してもかまいません。必要になれば、ツアーはまた書かれます。

**エディタ。** <kbd>o</kbd> は `$VISUAL` か `$EDITOR` を使います。vim、Neovim、Emacs、nano、VS Code、Cursor、Zed、Helix、Sublime Text などでは、その行に飛んで開きます。ほかのエディタなら、ひな形を設定してください。

```sh
export TOUR_EDITOR='code -g {file}:{line}'
```

手元のファイルが、ツアーにしているコミットの中身と違うときは、キャッシュに置いた読み取り専用の写しを開きます。ツアーが説明しているコードを見られるようにするためです。

**言語。** 英語、日本語、簡体字中国語です。`tour setup` で変えられます。`tour --lang ja` や `tour --lang zh` なら、その 1 回だけ変わります。

## よくある質問

**1 回にいくらかかりますか？**
あなたの Claude Code、Codex、Gemini のプランの中で動きます。Claude Code で約 250 行の変更をツアーにしたときは、API の料金に換算して 1 回 $1.00〜$1.50 ほどでした。保存したツアーを開き直すのには、費用はかかりません。

**なぜ数分もかかるのですか？**
LLM は書き始める前に、差分だけでなく、変更のまわりのファイルも読みます。その下調べがあるから、変更していないコードまで説明できます。

**ツアーを人に渡せますか？**
はい。ツアーはキャッシュにある JSON のファイルです。同じリポジトリを持っている人なら、`tour <file>.json` で開けます。

**`tour` が向いていないのはどんなときですか？**
1 行だけの修正なら、差分を読む方が早いです。`tour` が向いているのは、差分を見ただけでは「なぜ」がわからない変更です。新しい機能、リファクタリング、そして AI が書いて、これからあなたが面倒を見ることになるコードです。

## 開発

```sh
go test ./...
go run ./cmd/tour tours/example-coupons.en.json --repo path/to/diff-tour-example --dump --cols 140 --rows 44 --plain
```

`--dump` は、決まった画面の大きさで全部の手順を出力します。端末を使わずに見た目を確かめるときに便利です。[`demo/`](demo) には GIF を録るための台本が、[`experiments/generate`](experiments/generate) にはプロンプトを調整したときの記録があります。

## ライセンス

[MIT](LICENSE)
