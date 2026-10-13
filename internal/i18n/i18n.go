// Package i18n holds the UI messages for each language. Tour content is shown in the language of the tour data as is.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Lang string

const (
	EN Lang = "en"
	JA Lang = "ja"
	ZH Lang = "zh"
)

// Langs lists the supported languages. It is also the order of the choices.
var Langs = []Lang{EN, JA, ZH}

func (l Lang) Name() string {
	switch l {
	case ZH:
		return "简体中文"
	case JA:
		return "日本語"
	default:
		return "English"
	}
}

func Parse(s string) (Lang, bool) {
	switch strings.ToLower(s) {
	case "zh-cn", "zh_cn", "zh-hans":
		return ZH, true
	}
	for _, l := range Langs {
		if strings.EqualFold(s, string(l)) {
			return l, true
		}
	}
	return EN, false
}

var current = EN

func Set(l Lang) { current = l }

func Current() Lang { return current }

// Detect picks a language from the terminal's locale settings (LC_ALL -> LC_MESSAGES -> LANG).
// If the language isn't supported, it uses English. It also returns which variable it read from.
func Detect() (Lang, string) {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(key)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		code := strings.ToLower(strings.SplitN(strings.SplitN(v, ".", 2)[0], "_", 2)[0])
		if strings.HasPrefix(strings.ToLower(v), "zh") {
			return ZH, key + "=" + v
		}
		l, ok := Parse(code)
		if !ok {
			l = EN
		}
		return l, key + "=" + v
	}
	return EN, ""
}

// T returns the message for the current language, falling back to English if that language lacks it.
func T(key string, args ...any) string {
	m, ok := messages[current][key]
	if !ok {
		m, ok = messages[EN][key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(m, args...)
	}
	return m
}

var messages = map[Lang]map[string]string{
	EN: {
		"key.back":    "← Back",
		"key.next":    "→ Next",
		"key.last":    "Last step",
		"key.open":    "o Open in editor",
		"key.quit":    "q Quit",
		"scroll.more": "▼ More  ↑↓",
		"scroll.top":  "▲ ↑↓",
		"chapter":     "Ch. %d  %s",
		"question":    "Question",
		"think":       "Think it over, then press → to see the answer",

		"basis.fact":     "Verified",
		"basis.inferred": "From the code",
		"basis.opinion":  "Senior view",

		"tag.example":        "Example",
		"diff.summary":       "diff  +%d −%d",
		"schema.title":       "Schema",
		"schema.desc":        "%s applied at %s and at %s",
		"schema.newTable":    "new table",
		"change.added":       "added",
		"change.removed":     "removed",
		"change.changed":     "changed",
		"change.same":        "unchanged",
		"schema.column":      "Column",
		"schema.type":        "Type",
		"schema.constraints": "Constraints",
		"schema.index":       "index",
		"schema.unique":      "unique index",
		"rows.added":         "Green columns were added by this change",
		"target.branched":    "  (branched from %s)",
		"target.summary":     "%d commits · %d files changed (+%d −%d)",

		"err.fileNotFound": "File not found: %s (%s)",
		"err.lineRange":    "Lines out of range: %s L%d–%d (the file has %d lines)",
		"err.noDiff":       "No diff found: %s",
		"err.noHunk":       "Hunk not found: %s %v",
		"err.noTable":      "Table not found: %s",
		"err.unsafeSQL":    "Not executed for safety: %s",
		"err.migration":    "Cannot apply migration: %s (%v)",
		"err.schema":       "Cannot read the schema: %v",

		"editor.copy": "Opened a read-only copy: the working tree differs from %s",
		"editor.fail": "Cannot start the editor: %v",
		"needTTY":     "tour needs to run in a terminal",

		"picker.title":           "Which change do you want to tour?",
		"picker.uncommitted":     "uncommitted changes",
		"picker.uncommittedNote": "includes uncommitted changes",
		"picker.current":         "current branch",
		"picker.commits":         "%d commits",
		"picker.cached":          "ready",
		"picker.advanced":        "To tour a commit or a range: tour <ref>  /  tour <base>..<head>",
		"picker.info":            "Compared with %s · Tours are written by %s (change with tour setup)",
		"picker.keys":            "↑↓ Select   Enter Start   q Quit",
		"picker.empty":           "No local branch has commits that are not in %s.",
		"time.now":               "just now",
		"time.minutes":           "%d min ago",
		"time.hours":             "%d hours ago",
		"time.days":              "%d days ago",
		"gen.title":              "Writing a tour of %s (from %s)",
		"gen.reading":            "%s is reading the code (%s)",
		"flow.changed":           "changed",
		"flow.new":               "new",
		"role.core":              "core",
		"role.mechanical":        "mechanical",
		"role.test":              "test",
		"role.config":            "config",
		"role.docs":              "docs",
		"err.noChanges":          "no changed files",
		"key.ask":                "? Ask",
		"asked":                  "Your question",
		"ask.title":              "Ask about this step",
		"ask.help":               "↑↓ choose · type your own · Enter ask · Esc close",
		"ask.placeholder":        "Type a question…",
		"ask.question":           "Q:",
		"ask.answering":          "%s is reading the code to answer (%s)",
		"ask.added":              "The answer is the next step ($%.2f)",
		"ask.notSaved":           "The answer could not be saved: %v",
		"ask.failed":             "Could not answer: %s",
		"ask.canceled":           "canceled",
		"ask.unavailable":        "To ask questions, choose an LLM with tour setup",
		"gen.repairing":          "Fixing problems in the tour (attempt %d of %d, %s)",
		"gen.files":              "Files read: %d",
		"gen.search":             "search: %s",
		"gen.note":               "This usually takes 2 to 5 minutes. The tour is saved, so next time it opens instantly.",
		"gen.keys":               "q Cancel",
		"gen.saved":              "Tour saved (about $%.2f at API prices)",
		"gen.failed":             "Could not write the tour",
		"gen.failKeys":           "q Quit",
		"err.noLLM":              "No LLM is selected. Run tour setup to choose one.",
		"err.unsupported":        "%s is not supported. Choose another LLM with tour setup.",
		"err.notRepo":            "Not a git repository: %s",
		"err.badRef":             "Unknown commit or branch: %s",
		"err.noBase":             "Could not find a branch to compare with (main, master, ...). Use tour <base>..<head>.",
		"setup.welcome":          "Welcome to tour. Two quick choices before we start.",
		"setup.lang":             "Language",
		"setup.detected":         "detected from %s",
		"setup.llm":              "LLM that writes the tours",
		"setup.found":            "found",
		"setup.notFound":         "not installed",
		"setup.sendsCode":        "The code of the repository you tour will be sent to the selected LLM.",
		"setup.none":             "No LLM is available yet. You can choose one later with `tour setup`.",
		"setup.keys":             "↑↓ Select   Enter Confirm   ← Back   q Quit",
		"setup.saved":            "Saved settings to %s",
	},
	JA: {
		"key.back":    "← 戻る",
		"key.next":    "→ 次へ",
		"key.last":    "最後の手順です",
		"key.open":    "o エディタで開く",
		"key.quit":    "q 終了",
		"scroll.more": "▼ 続きあり  ↑↓",
		"scroll.top":  "▲ ↑↓",
		"chapter":     "第%d章  %s",
		"question":    "問い",
		"think":       "考えてから → で次へ",

		"basis.fact":     "確かめた",
		"basis.inferred": "読み取れる",
		"basis.opinion":  "シニアの見方",

		"tag.example":        "例",
		"diff.summary":       "差分  +%d −%d",
		"schema.title":       "スキーマ",
		"schema.desc":        "%s を %s と %s で当てた結果",
		"schema.newTable":    "新しいテーブル",
		"change.added":       "追加",
		"change.removed":     "削除",
		"change.changed":     "変更あり",
		"change.same":        "変更なし",
		"schema.column":      "列",
		"schema.type":        "型",
		"schema.constraints": "制約",
		"schema.index":       "索引",
		"schema.unique":      "一意の索引",
		"rows.added":         "緑の列は今回の変更で増えた列",
		"target.branched":    "（%s から分岐）",
		"target.summary":     "%d コミット · %d ファイル変更（+%d −%d）",

		"err.fileNotFound": "ファイルが見つかりません: %s（%s）",
		"err.lineRange":    "行の指定が範囲外です: %s L%d–%d（全 %d 行）",
		"err.noDiff":       "差分が見つかりません: %s",
		"err.noHunk":       "指定したハンクがありません: %s %v",
		"err.noTable":      "テーブルがありません: %s",
		"err.unsafeSQL":    "安全のため実行しません: %s",
		"err.migration":    "マイグレーションを当てられません: %s（%v）",
		"err.schema":       "スキーマを読めません: %v",

		"editor.copy": "作業ツリーが %s と違うため、読み取り専用のコピーを開きました",
		"editor.fail": "エディタを起動できません: %v",
		"needTTY":     "tour は端末の中で実行してください",

		"picker.title":           "どの変更をツアーしますか？",
		"picker.uncommitted":     "未コミットの変更",
		"picker.uncommittedNote": "未コミットの変更を含む",
		"picker.current":         "いま作業中",
		"picker.commits":         "%d コミット",
		"picker.cached":          "作成済み",
		"picker.advanced":        "コミットや範囲を指定するとき:  tour <ref>  /  tour <base>..<head>",
		"picker.info":            "比べる先: %s · ツアーを作る LLM: %s（tour setup で変更）",
		"picker.keys":            "↑↓ 選ぶ   Enter 開始   q 終了",
		"picker.empty":           "%s にないコミットを持つローカルのブランチがありません。",
		"time.now":               "たった今",
		"time.minutes":           "%d 分前",
		"time.hours":             "%d 時間前",
		"time.days":              "%d 日前",
		"gen.title":              "%s のツアーを作っています（%s から）",
		"gen.reading":            "%s がコードを読んでいます（%s）",
		"flow.changed":           "変更",
		"flow.new":               "新規",
		"role.core":              "中心",
		"role.mechanical":        "機械的",
		"role.test":              "テスト",
		"role.config":            "設定",
		"role.docs":              "文書",
		"err.noChanges":          "変更したファイルがない",
		"key.ask":                "? 質問",
		"asked":                  "あなたの質問",
		"ask.title":              "この手順について質問する",
		"ask.help":               "↑↓ 選ぶ・自由に入力・Enter 質問する・Esc 閉じる",
		"ask.placeholder":        "質問を入力…",
		"ask.question":           "質問：",
		"ask.answering":          "%s がコードを読んで答えています（%s）",
		"ask.added":              "答えを次の手順に足しました（$%.2f）",
		"ask.notSaved":           "答えを保存できませんでした: %v",
		"ask.failed":             "答えられませんでした: %s",
		"ask.canceled":           "取り消しました",
		"ask.unavailable":        "質問するには、tour setup で LLM を選んでください",
		"gen.repairing":          "ツアーの誤りを直しています（%d / %d 回目、%s）",
		"gen.files":              "読んだファイル: %d",
		"gen.search":             "検索: %s",
		"gen.note":               "ふつう 2〜5 分かかります。できたツアーは保存するので、次からはすぐに開けます。",
		"gen.keys":               "q 中止",
		"gen.saved":              "ツアーを保存しました（API の価格で約 $%.2f）",
		"gen.failed":             "ツアーを作れませんでした",
		"gen.failKeys":           "q 終了",
		"err.noLLM":              "LLM が選ばれていません。tour setup で選んでください。",
		"err.unsupported":        "%s には対応していません。tour setup で別の LLM を選んでください。",
		"err.notRepo":            "git のリポジトリではありません: %s",
		"err.badRef":             "コミットやブランチが見つかりません: %s",
		"err.noBase":             "比べる先のブランチ（main、master など）が見つかりません。tour <base>..<head> で指定してください。",
		"setup.welcome":          "tour へようこそ。最初に 2 つだけ決めます。",
		"setup.lang":             "表示の言語",
		"setup.detected":         "端末の設定（%s）から選びました",
		"setup.llm":              "ツアーを作る LLM",
		"setup.found":            "見つかりました",
		"setup.notFound":         "入っていません",
		"setup.sendsCode":        "ツアーにするリポジトリのコードが、選んだ LLM に送られます。",
		"setup.none":             "使える LLM がまだありません。あとで `tour setup` で選べます。",
		"setup.keys":             "↑↓ 選ぶ   Enter 決定   ← 戻る   q 終了",
		"setup.saved":            "設定を %s に保存しました",
	},
	ZH: {
		"key.back":               "← 返回",
		"key.next":               "→ 下一步",
		"key.last":               "最后一步",
		"key.open":               "o 在编辑器中打开",
		"key.quit":               "q 退出",
		"scroll.more":            "▼ 更多  ↑↓",
		"scroll.top":             "▲ ↑↓",
		"chapter":                "第 %d 章  %s",
		"question":               "疑问",
		"think":                  "先想一想，再按 → 查看答案",
		"basis.fact":             "已验证",
		"basis.inferred":         "从代码推断",
		"basis.opinion":          "资深视角",
		"tag.example":            "示例",
		"diff.summary":           "diff  +%d −%d",
		"schema.title":           "表结构",
		"schema.desc":            "%s 分别在 %s 和 %s 应用后的结果",
		"schema.newTable":        "新表",
		"change.added":           "新增",
		"change.removed":         "删除",
		"change.changed":         "已修改",
		"change.same":            "未修改",
		"schema.column":          "列",
		"schema.type":            "类型",
		"schema.constraints":     "约束",
		"schema.index":           "索引",
		"schema.unique":          "唯一索引",
		"rows.added":             "绿色列是本次改动新增的列",
		"target.branched":        "  （从 %s 分出）",
		"target.summary":         "%d 个 commit · %d 个文件改动（+%d −%d）",
		"err.fileNotFound":       "找不到文件：%s（%s）",
		"err.lineRange":          "行号超出范围：%s L%d–%d（共 %d 行）",
		"err.noDiff":             "找不到 diff：%s",
		"err.noHunk":             "找不到差异块：%s %v",
		"err.noTable":            "找不到表：%s",
		"err.unsafeSQL":          "为安全起见未执行：%s",
		"err.migration":          "无法应用迁移：%s（%v）",
		"err.schema":             "无法读取表结构：%v",
		"editor.copy":            "工作区与 %s 不同，已打开只读副本",
		"editor.fail":            "无法启动编辑器：%v",
		"needTTY":                "请在终端中运行 tour",
		"picker.title":           "想查看哪项改动的导览？",
		"picker.uncommitted":     "未提交的改动",
		"picker.uncommittedNote": "含未提交的改动",
		"picker.current":         "当前分支",
		"picker.commits":         "%d 个 commit",
		"picker.cached":          "已生成",
		"picker.advanced":        "指定 commit 或范围：tour <ref>  /  tour <base>..<head>",
		"picker.info":            "对比 %s · 导览由 %s 编写（用 tour setup 更改）",
		"picker.keys":            "↑↓ 选择   Enter 开始   q 退出",
		"picker.empty":           "没有本地分支包含 %s 中没有的 commit。",
		"time.now":               "刚刚",
		"time.minutes":           "%d 分钟前",
		"time.hours":             "%d 小时前",
		"time.days":              "%d 天前",
		"gen.title":              "正在编写 %s 的导览（从 %s 开始）",
		"gen.reading":            "%s 正在阅读代码（%s）",
		"flow.changed":           "已修改",
		"flow.new":               "新增",
		"role.core":              "核心",
		"role.mechanical":        "机械改动",
		"role.test":              "测试",
		"role.config":            "配置",
		"role.docs":              "文档",
		"err.noChanges":          "没有文件改动",
		"key.ask":                "? 提问",
		"asked":                  "你的问题",
		"ask.title":              "询问当前步骤",
		"ask.help":               "↑↓ 选择 · 自行输入 · Enter 提问 · Esc 关闭",
		"ask.placeholder":        "输入问题…",
		"ask.question":           "问：",
		"ask.answering":          "%s 正在阅读代码以回答问题（%s）",
		"ask.added":              "答案已加入下一步（$%.2f）",
		"ask.notSaved":           "无法保存答案：%v",
		"ask.failed":             "无法回答：%s",
		"ask.canceled":           "已取消",
		"ask.unavailable":        "提问前，请用 tour setup 选择 LLM",
		"gen.repairing":          "正在修复导览问题（第 %d 次，共 %d 次，%s）",
		"gen.files":              "已读文件：%d",
		"gen.search":             "搜索：%s",
		"gen.note":               "通常需要 2 到 5 分钟。导览会保存，下次可直接打开。",
		"gen.keys":               "q 取消",
		"gen.saved":              "导览已保存（按 API 价格约 $%.2f）",
		"gen.failed":             "无法生成导览",
		"gen.failKeys":           "q 退出",
		"err.noLLM":              "尚未选择 LLM。请运行 tour setup 选择。",
		"err.unsupported":        "不支持 %s。请用 tour setup 选择其他 LLM。",
		"err.notRepo":            "不是 git 仓库：%s",
		"err.badRef":             "未知的 commit 或分支：%s",
		"err.noBase":             "找不到用于对比的分支（main、master 等）。请用 tour <base>..<head> 指定。",
		"setup.welcome":          "欢迎使用 tour。开始前先做两项选择。",
		"setup.lang":             "语言",
		"setup.detected":         "根据 %s 检测",
		"setup.llm":              "编写导览的 LLM",
		"setup.found":            "已找到",
		"setup.notFound":         "未安装",
		"setup.sendsCode":        "导览所用仓库的代码将发送给选中的 LLM。",
		"setup.none":             "暂无可用的 LLM。稍后可用 `tour setup` 选择。",
		"setup.keys":             "↑↓ 选择   Enter 确认   ← 返回   q 退出",
		"setup.saved":            "设置已保存到 %s",
	},
}
