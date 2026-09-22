package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func scenarioSourceDomain(raw, current string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, current) {
		return current, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("source_domain must be an Encounter hostname such as svk.en.cx")
	}
	host := strings.ToLower(u.Hostname())
	label := strings.TrimSuffix(host, ".en.cx")
	if label == host || label == "" || strings.Contains(label, ".") || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return "", fmt.Errorf("source_domain must be an Encounter hostname such as svk.en.cx")
	}
	if strings.EqualFold(host, current) {
		return current, nil
	}
	return host, nil
}

func agentScenarioSource(ctx context.Context, cfg *config, current *encx.Client, rawDomain string) (*config, *encx.Client) {
	domain, err := scenarioSourceDomain(rawDomain, cfg.domain)
	if err != nil {
		fatal("%v", err)
	}
	sourceCfg := *cfg
	sourceCfg.domain = domain
	if domain == cfg.domain {
		requireAdminAuth(ctx, &sourceCfg, current)
		return &sourceCfg, current
	}
	// Credentials and cookies are scoped to the source domain; never reuse the
	// target's explicit credentials or cookie jar on another host.
	sourceCfg.login, sourceCfg.password = "", ""
	source := encx.New(domain, appendEncOpts(&sourceCfg)...)
	if !loadSession(&sourceCfg, source) {
		fatal("No saved session for source domain %s. Log into that domain first", domain)
	}
	if err := source.VerifyAdminSession(ctx); err != nil {
		fatal("Source domain %s authentication failed; log into that domain: %v", domain, err)
	}
	return &sourceCfg, source
}

func readRemoteAgentScenario(ctx context.Context, source *encx.Client, gameID int) *scenario.Document {
	if gameID <= 0 {
		fatal("source_game_id must be positive")
	}
	doc, err := source.GetAdminGameScenario(ctx, gameID)
	if err != nil {
		fatal("Read source game %d scenario: %v", gameID, err)
	}
	if len(doc.Levels) == 0 {
		fatal("Source game %d has no scenario levels", gameID)
	}
	return doc
}
