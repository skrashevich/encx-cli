package main

import (
	"strings"
	"testing"
)

// onboardingUIIDs — идентификаторы, к которым привязывается мастер первого
// запуска. Поведение живёт в webui/app.js, разметка — в webui/index.html;
// список держит их в согласии. Новый элемент мастера добавляется сюда одной
// строкой.
var onboardingUIIDs = []string{
	// Корень, заголовок, индикатор шагов.
	"onboarding",
	"onboarding-title",
	"onboarding-subtitle",
	"onboarding-steps",

	// Шаги.
	"onboarding-pane-welcome",
	"onboarding-pane-llm",
	"onboarding-pane-auth",

	// Шаг «Модель».
	"onboarding-llm-tab-codex",
	"onboarding-llm-tab-apikey",
	"onboarding-llm-pane-codex",
	"onboarding-llm-pane-apikey",
	"onboarding-codex-status",
	"btn-onboarding-codex-login",
	"onboarding-codex-progress",
	"onboarding-codex-code",
	"btn-onboarding-codex-code-submit",
	"onboarding-llm-base-url",
	"onboarding-llm-api-key",
	"onboarding-llm-model",
	"onboarding-llm-hint",
	"btn-onboarding-llm-test",
	"onboarding-llm-result",
	"onboarding-llm-overrides",

	// Шаг «Вход».
	"onboarding-auth-form",
	"onboarding-auth-domain",
	"onboarding-auth-login",
	"onboarding-auth-password",
	"btn-onboarding-auth-submit",
	"onboarding-auth-status",
	"btn-onboarding-auth-skip",

	// Подвал и повторный запуск мастера.
	"onboarding-error",
	"btn-onboarding-back",
	"btn-onboarding-next",
	"btn-onboarding-skip-all",
	"btn-onboarding-open",
}

// readOnboardingUIAsset читает файл через embed.FS, а не с диска: проверять
// нужно именно то, что уезжает внутрь собранного бинаря.
func readOnboardingUIAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := webUIFiles.ReadFile(name)
	if err != nil {
		t.Fatalf("webUIFiles.ReadFile(%q): %v", name, err)
	}
	if len(data) == 0 {
		t.Fatalf("webUIFiles.ReadFile(%q): пустой файл", name)
	}
	return string(data)
}

func TestOnboardingMarkupCarriesRequiredIDs(t *testing.T) {
	html := readOnboardingUIAsset(t, "webui/index.html")

	seen := make(map[string]bool, len(onboardingUIIDs))
	for _, id := range onboardingUIIDs {
		if seen[id] {
			t.Errorf("id %q перечислен в onboardingUIIDs дважды", id)
			continue
		}
		seen[id] = true

		attr := `id="` + id + `"`
		switch n := strings.Count(html, attr); n {
		case 1:
			// ok
		case 0:
			t.Errorf("webui/index.html: нет элемента с %s", attr)
		default:
			t.Errorf("webui/index.html: %s встречается %d раз, ожидается 1", attr, n)
		}
	}
}

func TestOnboardingMarkupWiring(t *testing.T) {
	html := readOnboardingUIAsset(t, "webui/index.html")

	required := []struct {
		what    string
		snippet string
	}{
		{"оверлей мастера скрыт до запуска", `<div class="onboarding-overlay" id="onboarding" hidden>`},
		{"диалог помечен как модальный", `role="dialog" aria-modal="true" aria-labelledby="onboarding-title"`},
		{"вкладка codex управляет своей панелью", `aria-controls="onboarding-llm-pane-codex"`},
		{"вкладка apikey управляет своей панелью", `aria-controls="onboarding-llm-pane-apikey"`},
		{"ошибка мастера объявляется ассистивным технологиям", `id="onboarding-error" role="alert" hidden`},
		{"логин подсказывается менеджером паролей", `autocomplete="username"`},
		{"пароль подсказывается менеджером паролей", `autocomplete="current-password"`},
		{"кнопка повторного запуска подписана", `aria-label="Мастер настройки"`},
	}
	for _, c := range required {
		if !strings.Contains(html, c.snippet) {
			t.Errorf("webui/index.html: %s — не найдено %q", c.what, c.snippet)
		}
	}

	// Шаги индикатора: JS ищет их по data-step.
	for _, step := range []string{"welcome", "llm", "auth"} {
		if !strings.Contains(html, `data-step="`+step+`"`) {
			t.Errorf("webui/index.html: в #onboarding-steps нет шага data-step=%q", step)
		}
	}

	// Каждое поле ввода мастера должно иметь настоящий <label for=...>.
	for _, id := range []string{
		"onboarding-llm-base-url",
		"onboarding-llm-api-key",
		"onboarding-llm-model",
		"onboarding-auth-domain",
		"onboarding-auth-login",
		"onboarding-auth-password",
	} {
		if !strings.Contains(html, `for="`+id+`"`) {
			t.Errorf("webui/index.html: у поля %q нет <label for=...>", id)
		}
	}

	// Надпись кнопки «Далее» лежит в отдельном span, JS меняет её на «Готово».
	if !strings.Contains(html, `id="btn-onboarding-next"><span id="onboarding-next-label">Далее</span>`) {
		t.Error(`webui/index.html: текст #btn-onboarding-next должен лежать в <span id="onboarding-next-label">`)
	}
}

func TestOnboardingMarkupIsStyled(t *testing.T) {
	css := readOnboardingUIAsset(t, "webui/style.css")

	for _, selector := range []string{
		".onboarding-overlay[hidden]",
		".onboarding-overlay:not([hidden])",
		".onboarding-pane[hidden]",
		".onboarding-step.is-active",
		".onboarding-step.is-done",
	} {
		if !strings.Contains(css, selector) {
			t.Errorf("webui/style.css: нет правила для %s — разметка мастера уедет без стилей", selector)
		}
	}

	// Мастер должен лежать выше модалки настроек LLM (z-index: 150).
	if !strings.Contains(css, "z-index: 160;") {
		t.Error("webui/style.css: у .onboarding-overlay должен быть z-index выше .modal-overlay (150)")
	}

	// Узкий экран: раскладка мастера обязана иметь свой брейкпоинт.
	if !strings.Contains(css, "@media (max-width: 480px)") {
		t.Error("webui/style.css: нет блока @media (max-width: 480px) для узких экранов")
	}
}
