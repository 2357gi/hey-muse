package voice

import (
	"regexp"
	"strings"
)

type applianceCommand struct{ target, action, label string }
type voiceAppliance struct {
	id, name, kind string
	names          []string
}

var defaultLightOff = regexp.MustCompile(`^(?:電気|でんき)(?:を)?[、 ]?(?:消して|けして)$`)

var voiceAppliances = []voiceAppliance{
	{"living", "リビングの照明", "power", []string{"リビングの電気", "リビングの照明", "リビングのライト", "リビングの明かり", "リビングの灯り"}},
	{"ac", "エアコン", "power", []string{"エアコン", "クーラー"}},
	{"fan", "サーキュレーター", "power", []string{"サーキュレーター", "サーキュレータ", "扇風機"}},
	{"plug", "プラグミニ", "power", []string{"プラグミニ", "スマートプラグ"}},
	{"vacuum", "掃除機", "power", []string{"掃除機"}},
	{"curtain", "カーテン", "curtain", []string{"カーテン"}},
	{"monitor", "モニター", "monitorToggle", []string{"モニター", "モニタ"}},
	{"projector", "プロジェクター", "toggle", []string{"プロジェクター", "プロジェクタ"}},
}

func applianceAction(kind, verb string) (action, description string) {
	switch kind {
	case "power":
		switch verb {
		case "つけて", "付けて", "点けて", "入れて", "オンにして", "onにして":
			return "on", "オン"
		case "消して", "けして", "切って", "オフにして", "offにして":
			return "off", "オフ"
		}
	case "curtain":
		switch verb {
		case "開けて", "あけて", "開いて":
			return "open", "開く操作"
		case "閉めて", "しめて", "閉じて":
			return "close", "閉じる操作"
		}
	case "monitorToggle":
		switch verb {
		case "つけて", "付けて", "点けて", "消して", "けして":
			return "toggle", "電源切替"
		}
		fallthrough
	case "toggle":
		if verb == "切り替えて" || verb == "切替えて" || verb == "切りかえて" {
			return "toggle", "電源切替"
		}
	}
	return "", ""
}

func parseAppliance(text string) (applianceCommand, bool) {
	if direction := hallDirection(text); direction != "" {
		label := "廊下の点灯"
		if direction == "off" {
			label = "廊下の消灯"
		}
		return applianceCommand{"hall", direction, label}, true
	}
	phrase := strings.ToLower(strings.TrimSpace(text))
	phrase = strings.TrimRight(phrase, "。.!！")
	phrase = strings.TrimSuffix(strings.TrimSuffix(phrase, "ください"), "下さい")
	if defaultLightOff.MatchString(phrase) {
		return applianceCommand{"living", "off", "リビングの照明のオフ"}, true
	}
	for _, device := range voiceAppliances {
		for _, name := range device.names {
			tail, ok := strings.CutPrefix(phrase, name)
			if !ok {
				continue
			}
			if device.kind != "curtain" {
				tail = strings.TrimPrefix(tail, "の電源")
			}
			verb := strings.TrimLeft(strings.TrimPrefix(tail, "を"), "、 ")
			if device.id == "fan" && verb == "回して" {
				verb = "つけて"
			}
			if (device.id == "fan" || device.id == "ac") && verb == "止めて" {
				verb = "消して"
			}
			if device.id == "living" && verb == "点灯して" {
				verb = "つけて"
			}
			if device.id == "living" && verb == "消灯して" {
				verb = "消して"
			}
			action, description := applianceAction(device.kind, verb)
			if action != "" {
				return applianceCommand{device.id, action, device.name + "の" + description}, true
			}
		}
	}
	return applianceCommand{}, false
}
