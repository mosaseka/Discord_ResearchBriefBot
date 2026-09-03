package researchrelevance

import "strings"

var undergraduateTemplates = map[string]string{
	"patient simulation":           "実患者で探索的RLを実行することが難しいため、安全な比較実験環境の設計例として参考になる。",
	"JITAI / intervention timing":  "介入タイミングや介入内容をactionとして扱う学部規模のbandit/RL実験へ直接つなげやすい。",
	"offline RL":                   "既存ログから方策を評価するため、実患者に探索行動をさせずに個別化研究を行う方法として参考になる。",
	"safe / constrained RL":        "reward最大化だけでなくrisk/cost制約を別に扱う研究設計の参考になる。",
	"longitudinal personalization": "個別化状態を単一ターンではなく長期履歴として設計する際の参考になる。",
	"multi-agent":                  "状態推定・介入選択・安全監督などを責務分離する設計へ応用できる。",
	"digital phenotyping":          "行動・時間・センサ情報をRL状態へどう組み込むかを検討する材料になる。",
	"reinforcement learning":       "状態・action・rewardを明示した個別化介入実験の設計例として検討材料になる。",
	"mental-health agent":          "mental-health agentの応答方針と安全評価を分離して設計する際の参考になる。",
}

func UndergraduateMeaning(topics []string) string {
	parts := make([]string, 0, 2)
	for _, topic := range topics {
		if value := undergraduateTemplates[topic]; value != "" {
			parts = append(parts, value)
		}
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "ルールに一致する学部研究向けテンプレートはありません。"
	}
	return strings.Join(parts, " ")
}
