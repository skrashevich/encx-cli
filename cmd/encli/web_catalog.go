package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// webGameOption is one selectable game in the web UI.
type webGameOption struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Role  string `json:"role"` // player | admin | both
}

func fetchAuthorizedDomains(registry *AuthRegistry) []string {
	var out []string
	for _, st := range registry.ListStatus() {
		if st.HasSession {
			out = append(out, st.Domain)
		}
	}
	slices.Sort(out)
	return out
}

func fetchWebGames(ctx context.Context, registry *AuthRegistry, cfg *config, domain string) ([]webGameOption, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}
	client := registry.Get(domain, encOptsFromConfig(cfg))

	var list *encx.GameListResponse
	var admin []encx.AdminGame
	var listErr, adminErr error

	registry.WithDomainLock(domain, func() {
		list, listErr = client.GetGameList(ctx)
		admin, adminErr = client.AdminGetGames(ctx)
	})

	if listErr != nil && adminErr != nil {
		return nil, fmt.Errorf("games: %v; admin: %v", listErr, adminErr)
	}

	return mergeWebGameOptions(list, admin), nil
}

func mergeWebGameOptions(list *encx.GameListResponse, admin []encx.AdminGame) []webGameOption {
	byID := map[int]*webGameOption{}

	add := func(id int, title, role string) {
		if id <= 0 {
			return
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = fmt.Sprintf("Игра %d", id)
		}
		if ex, ok := byID[id]; ok {
			if ex.Role != role {
				ex.Role = "both"
			}
			if len(title) > len(ex.Title) {
				ex.Title = title
			}
			return
		}
		byID[id] = &webGameOption{ID: id, Title: title, Role: role}
	}

	if list != nil {
		for _, g := range list.ActiveGames {
			if g.Finished {
				continue
			}
			add(g.GameID, g.Title, "player")
		}
		for _, g := range list.ComingGames {
			if g.Finished {
				continue
			}
			add(g.GameID, g.Title, "player")
		}
	}
	for _, g := range admin {
		add(g.ID, g.Title, "admin")
	}

	out := make([]webGameOption, 0, len(byID))
	for _, g := range byID {
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b webGameOption) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}

func roleLabelRu(role string) string {
	switch role {
	case "admin":
		return "админ"
	case "both":
		return "игрок+админ"
	default:
		return "игрок"
	}
}
