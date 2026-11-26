package manager

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/models"
	"golang.org/x/text/language"
)

// SupportedAttachmentLangs lists the attachment-section languages the sender
// can render. The campaign API and admin UI only accept these values.
var SupportedAttachmentLangs = []string{"en", "zh-CN", "zh-TW", "es", "fr", "de", "pt", "it", "ru", "ja", "ko"}

// attachmentLabels holds the recipient-facing strings of the appended
// attachment section.
type attachmentLabels struct {
	title    string
	view     string
	download string
	count    func(int) string
}

// NormalizeAttachmentLang maps user input to a supported language code. An
// empty value selects the default language (English); regional variants such
// as en-US or zh_HK resolve to the canonical code; unknown values report false.
func NormalizeAttachmentLang(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" {
		return models.CampaignAttachmentLangDefault, true
	}
	if _, err := language.Parse(s); err != nil {
		return "", false
	}
	switch {
	case s == "zh" || s == "zh-cn" || s == "zh-hans" ||
		strings.HasPrefix(s, "zh-cn-") || strings.HasPrefix(s, "zh-hans-"):
		return "zh-CN", true
	case s == "zh-tw" || s == "zh-hk" || s == "zh-mo" || s == "zh-hant" ||
		strings.HasPrefix(s, "zh-tw-") || strings.HasPrefix(s, "zh-hk-") || strings.HasPrefix(s, "zh-hant-"):
		return "zh-TW", true
	}
	for _, lang := range SupportedAttachmentLangs {
		if s == lang || strings.HasPrefix(s, lang+"-") {
			return lang, true
		}
	}
	return "", false
}

func attachmentLabelsFor(lang string) attachmentLabels {
	if labels, ok := attachmentLangLabels[lang]; ok {
		return labels
	}
	return attachmentLangLabels[models.CampaignAttachmentLangDefault]
}

func pluralCount(one, many string) func(int) string {
	return func(n int) string {
		if n == 1 {
			return one
		}
		return strings.Replace(many, "%d", strconv.Itoa(n), 1)
	}
}

var attachmentLangLabels = map[string]attachmentLabels{
	"en": {
		title: "Attachments", view: "View", download: "Download",
		count: pluralCount("1 file", "%d files"),
	},
	"zh-CN": {
		title: "附件资料", view: "查看", download: "下载",
		count: func(n int) string { return fmt.Sprintf("%d 个文件", n) },
	},
	"zh-TW": {
		title: "附件資料", view: "檢視", download: "下載",
		count: func(n int) string { return fmt.Sprintf("%d 個檔案", n) },
	},
	"es": {
		title: "Archivos adjuntos", view: "Ver", download: "Descargar",
		count: pluralCount("1 archivo", "%d archivos"),
	},
	"fr": {
		title: "Pièces jointes", view: "Afficher", download: "Télécharger",
		count: pluralCount("1 fichier", "%d fichiers"),
	},
	"de": {
		title: "Anhänge", view: "Ansehen", download: "Herunterladen",
		count: pluralCount("1 Datei", "%d Dateien"),
	},
	"pt": {
		title: "Anexos", view: "Visualizar", download: "Baixar",
		count: pluralCount("1 arquivo", "%d arquivos"),
	},
	"it": {
		title: "Allegati", view: "Visualizza", download: "Scarica",
		count: pluralCount("1 file", "%d file"),
	},
	"ru": {
		title: "Вложения", view: "Просмотр", download: "Скачать",
		count: russianCount,
	},
	"ja": {
		title: "添付ファイル", view: "表示", download: "ダウンロード",
		count: func(n int) string { return fmt.Sprintf("%d件のファイル", n) },
	},
	"ko": {
		title: "첨부 파일", view: "보기", download: "다운로드",
		count: func(n int) string { return fmt.Sprintf("파일 %d개", n) },
	},
}

func russianCount(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return fmt.Sprintf("%d файл", n)
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14) {
		return fmt.Sprintf("%d файла", n)
	}
	return fmt.Sprintf("%d файлов", n)
}
