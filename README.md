# Discord Research Brief Bot

`Discord_ResearchBriefBot` は、reinforcement learning / agent systems と personalized mental-health care が直接交わる新着論文・技術資料を毎週最大5件選び、Discord Bot APIへ投稿するGoアプリケーションです。既定スケジュールは毎週金曜日18:00 JSTです。

運用時にOpenAI、Anthropic、GeminiなどのLLM API、有料検索API、有料news API、Web UI自動操作は一切使いません。arXiv、NCBI E-utilities、Crossref、運用者が明示的に設定した公開RSS/AtomとDiscord公式REST APIだけを使用するため、Bot用インフラを除き完全無料で運用できます。一般記事ページとGoogle Scholarは取得しません。

このBotの要約はabstractから文を選ぶ抽出的要約です。evidence qualityと「学部研究への意味」は固定ルールです。ChatGPT等による文脈的・生成型評価より機械的であり、入力が同じなら結果も同じです。**このBotによるevidence qualityや研究上の重要性評価は、人間のsystematic reviewや専門家評価の代替ではありません。**

## データソース

- arXiv API: RL、bandit、agent、JITAI、patient simulation等とmental-health語群をAND結合して検索します。APIへの連続requestは3秒以上空けます。
- PubMed / NCBI E-utilities: API keyなしで使い、request間隔を350ms以上空けます。`NCBI_TOOL`と任意の`NCBI_EMAIL`を送信します。
- Crossref REST API: DOIを持つ候補でjournalまたはpublication dateが不足している場合だけ、1 run最大8件を1秒間隔で補完します。`CROSSREF_MAILTO`は任意です。
- RSS / Atom: [config/sources.yaml](config/sources.yaml) に記載したHTTPS feedだけを取得します。例は無効です。利用規約、公式性、MIME、更新頻度を確認してから `enabled: true` にしてください。

記事本文は取得しません。feed内のlinkは表示用であり、自動的な追加fetchには使いません。

## 選定パイプライン

最初に直近7日を検索し、関連性があり未通知の候補が5件未満なら直近30日で取り直します。30日でも足りなければ件数を水増ししません。対象期間はDiscord冒頭に表示されます。

重複は次の順に識別し、査読版を優先して統合します。

1. 正規化DOI
2. version suffixを除いたarXiv ID
3. PMID
4. NFKC・case fold・記号/空白正規化済みtitle
5. title trigram Jaccard similarity 0.90以上

査読版へ統合した場合もarXiv URLをmetadataとして保持します。通知履歴のrevisionはidentityと`updated_at`から決めます。新しいDOIの査読版や、更新日時が変わったarXiv revisionは再通知可能です。arXivの「大幅な」改訂かどうかを本文比較で判断はせず、更新日時の変化を保守的なproxyにします。

### Ranking rule

全て整数の決定論的scoreです。

```text
total = relevance + source_quality + evidence + freshness + technical_relevance
```

- `relevance` (最大48): mental-health語を少なくとも1つ含み、technical score 6以上であることが必須です。contextual bandit、offline/safe/constrained RL、JITAI、patient simulator、longitudinal personalization等を強く評価します。日本語の関連資料は+3です。
- `source_quality` (最大25): 査読済み25、arXiv 14、technical report 10〜14、research feed 5〜9です。feedの1〜5のweightはこの範囲だけに影響し、venueブランド名だけでは加点しません。
- `evidence` (20 / 12 / 4): 下記の高 / 中 / 低に対応します。
- `freshness` (10 / 8 / 5 / 0): 7日以内、14日以内、30日以内、それより古い順です。
- `technical_relevance` (最大10): 一致した技術topic数を加点します。

同点はsource quality、publication date、正規化titleの順で決めます。

### Evidence quality rule

abstractとpublication metadataに明記された語だけを使います。`randomized`、RCT、prospective、retrospective、participants、patients、`N=`、simulation、synthetic、benchmark、clinician、expert/human evaluation、external validation、systematic review、meta-analysis、longitudinal、pilot、feasibility等を検出します。人数は明示的な表現だけから抽出します。

- 高: RCT / randomized trial / systematic review / meta-analysis、明記されたNが200以上の査読human study、査読済みexternal validation、または比較を伴うclinician validation。
- 中: 査読済みprospective / retrospective / human study、benchmark、expert/human evaluation、実人数と比較が明記されたstudy、またはvalidated simulation。synthetic-onlyはここへ上げません。
- 低: 上記の検証が確認できないconceptual paper、prototype、small pilot、synthetic-only、LLM-as-a-judgeのみ、technical blog等。査読済みという事実だけでは高にしません。

Discordにはlabelだけでなく、確認できたsource種別、study design、sample size、simulation/synthetic等の根拠を併記します。記載がない事実は推測しません。

### Extractive summaryと学部研究テンプレート

目的・方法・結果のcue語、topic語、数値、文位置を固定score化し、abstractから最大3文を選びます。文を生成・翻訳しません。Discord limitに合わせた末尾切詰め以外は元文を保持します。

「学部研究への意味」はtopic順で最大2個の固定テンプレートを連結します。patient simulation、JITAI/intervention timing、offline RL、safe/constrained RL、longitudinal personalization、multi-agent、digital phenotyping等に対応します。これはBotのルールベース評価であり、著者の主張ではありません。

## セットアップ

Go 1.25以上が必要です。SQLite driverはpure Goの`modernc.org/sqlite`で、CGOは不要です。

直接dependencyはversion固定した3件だけです。標準libraryにないNFKC正規化へBSD系licenseの`golang.org/x/text`、extended grapheme cluster境界へMIT licenseの`github.com/clipperhouse/uax29/v2`、再投稿抑止とtransactionalな監査履歴へBSD-3-Clauseの`modernc.org/sqlite`を使います。SQLiteは推移dependencyとbinary sizeを増やします（Windows/amd64の確認buildは約16 MiB）が、CGOなしでLinux/Windowsの同一実装を維持できます。feed/parser、cron、Discord REST clientには追加libraryを使いません。versionとchecksumは`go.mod` / `go.sum`で固定し、`go mod verify`、`go list -m all`、`go mod graph`で確認します。

```powershell
go mod download
go build -o discord-research-brief-bot.exe ./cmd/researchbot
```

`.env.example` を参照して環境変数を設定してください。このアプリは `.env` を自動読込みしません。`DISCORD_BOT_TOKEN`は標準出力、JSON log、SQLiteに保存しません。channel IDもlogやSQLiteへ平文保存せず、通知履歴のscopeにはdomain-separated SHA-256 keyだけを使います。

Discord Developer PortalでBotを作成し、対象channelへ次だけを許可します。

- View Channels
- Send Messages
- Embed Links

Administrator、Message Content Intent、Gateway接続は不要です。tokenとchannel IDを環境変数 `DISCORD_BOT_TOKEN`、`DISCORD_CHANNEL_ID`へ設定します。

## 実行方法

```powershell
# 1回収集して投稿
go run ./cmd/researchbot --once

# 実データを取得し、投稿せずMarkdownをstdoutへ出力
go run ./cmd/researchbot --once --dry-run

# networkを一切使わないfixture実行
go run ./cmd/researchbot --once --dry-run --fixture ./testdata

# 内部scheduler（既定: 金曜18:00 JST）
go run ./cmd/researchbot
```

`--fixture` は `--once` を必須とし、自動的にdry-runになります。fixtureの基準時刻は `testdata/manifest.json` に固定されるため、実行日によらず同じ結果です。

## 環境変数

| 変数 | 必須 | 既定値 / 用途 |
|---|---:|---|
| `DISCORD_BOT_TOKEN` | 投稿時 | 空。Discord Bot token |
| `DISCORD_CHANNEL_ID` | 投稿時 | 空。投稿channel snowflake |
| `APP_TIMEZONE` | いいえ | `Asia/Tokyo` |
| `BRIEF_CRON` | いいえ | `0 18 * * 5`。秒なし5-field cron |
| `NCBI_TOOL` | いいえ | `discord-research-brief-bot` |
| `NCBI_EMAIL` | いいえ | NCBI連絡先 |
| `CROSSREF_MAILTO` | いいえ | Crossref polite pool連絡先 |
| `DATABASE_PATH` | いいえ | `data/researchbot.db` |
| `HTTP_USER_AGENT` | いいえ | project名とURLを含む既定値 |
| `LOG_LEVEL` | いいえ | `debug`, `info`, `warn`, `error` |

cron parserは`*`、数値、range、list、stepを扱い、day-of-weekは0または7をSundayとして扱います。

## GitHub Actionsで無料運用

[.github/workflows/brief.yml](.github/workflows/brief.yml) はpublic repositoryのGitHub-hosted runnerで金曜09:00 UTC（18:00 JST）に `go run ./cmd/researchbot --once` を実行し、`workflow_dispatch`にも対応します。repository Secretsへ次を設定してください。

- `DISCORD_BOT_TOKEN`
- `DISCORD_CHANNEL_ID`

任意の連絡先はSecretsではなくrepository Variables `NCBI_EMAIL`、`CROSSREF_MAILTO`に設定できます。OpenAI等のAPI secretは不要です。SQLite通知履歴は`actions/cache`へ引き継ぎますが、cacheの保持期限・eviction・失敗したjobでは更新されない点に注意してください。cacheが消えた場合は過去30日内の文献が再通知される可能性があります。GitHub Actionsのscheduled workflowは負荷状況により開始が遅れることがあります。

## systemd

binaryを `/opt/discord-research-brief-bot/bin/discord-research-brief-bot` に置き、専用user/group `discord-research-brief` を作成します。[deploy/discord-research-brief-bot.service](deploy/discord-research-brief-bot.service) をinstallし、rootだけが読める `/etc/discord-research-brief-bot.env` を作ります。

```text
DISCORD_BOT_TOKEN=
DISCORD_CHANNEL_ID=
DATABASE_PATH=/var/lib/discord-research-brief-bot/researchbot.db
APP_TIMEZONE=Asia/Tokyo
BRIEF_CRON=0 18 * * 5
NCBI_TOOL=discord-research-brief-bot
NCBI_EMAIL=
CROSSREF_MAILTO=
LOG_LEVEL=info
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now discord-research-brief-bot.service
```

unitは`Restart=on-failure`、`NoNewPrivileges`、`ProtectSystem=strict`、private tmp/devices、capability削除等を有効にします。`StateDirectory`だけが書込み可能です。

## SecurityとDiscord制限

全outbound HTTPは単一client境界を通り、request timeout、展開後response size、Content-Type、redirect回数、context cancellationを検証します。URLはHTTPS限定で、DNS事前検査とdial直前の再解決を行い、loopback、private、link-local、multicast、CGNAT、documentation range等が1つでも混ざるanswerを拒否します。proxy環境変数は継承しません。feed entryのURLを追加fetchしないため、untrusted link経由のSSRFとrobots.txt対象の本文crawlはありません。

Discordは公式REST API v10を使い、429の`retry_after`と5xxをbounded retryします。message content 2000、embed title 256、field 1024、1 messageのembed合計6000、embed最大10をUTF-16 unitで検証し、extended grapheme cluster境界で切ります。code fenceを無効化してから分割し、全payloadに空の`allowed_mentions`を付け、`@everyone`、`@here`、user/role mentionを無効化します。nonceとSQLite予約により曖昧な送信失敗時の安易な再送を防ぎます。

## 永続化

SQLiteにはrun history、source/window別の取得成否と件数、title・URL・source・publication/update date・DOI・arXiv ID・PMID、score、selected flag、delivery state、message ID、通知日時を保存します。abstractや記事本文は保存しません。各設定feedは独立collectorとして失敗を隔離します。migrationは埋込みSQLをchecksum検証し、transactionでappend-only適用します。

## Test

全testはfixture、fake、`httptest`だけを使い、実arXiv、PubMed、Crossref、RSS、Discord、DNSへ接続しません。

```powershell
$goFiles = @(rg --files -g '*.go')
gofmt -w $goFiles
go mod tidy
go mod verify
go test ./...
go vet ./...
go test -race ./...
$env:CGO_ENABLED = "0"
go test ./...
go build ./cmd/researchbot
go run ./cmd/researchbot --once --dry-run --fixture ./testdata
git diff --check
```

## 既知の制約

- LLM、全文翻訳、意味理解は行わず、keyword/cueに現れない関係を拾えません。否定表現も完全には解釈できません。
- abstract/metadataだけを使うため、本文にしかないstudy limitation、risk of bias、数値は評価しません。
- PubMed publication typeまたはCrossrefの`journal-article`等を査読済みのproxyとして扱います。個々のvenueの査読方針までは自動検証しません。
- PubMedで日が提供されない月単位publication dateは月初として扱うため、境界付近で保守的に除外される場合があります。
- Crossref補完は不足metadataを持つDOIの先頭8件までです。
- arXiv / PubMedは各検索windowの先頭100件までを取得するため、非常に件数が多い週は低順位候補を取得前に取りこぼす可能性があります。
- fuzzy title dedupは高いthresholdでも誤統合・未統合の可能性があります。
- `config/sources.yaml` parserは記載例のflatなscalar subsetを対象とし、anchor、multiline scalar等の高度なYAML構文には対応しません。
- RSS/Atom sourceの品質・利用条件・type・weightは運用者のreviewに依存します。
- evidence labelと学部研究テンプレートはsystematic reviewや専門家判断ではありません。
