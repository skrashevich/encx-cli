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
	return scenarioDomain(raw, current, "source_domain")
}

func scenarioDomain(raw, current, parameter string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, current) {
		return current, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s must be an Encounter hostname such as svk.en.cx", parameter)
	}
	host := strings.ToLower(u.Hostname())
	label := strings.TrimSuffix(host, ".en.cx")
	if label == host || label == "" || strings.Contains(label, ".") || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return "", fmt.Errorf("%s must be an Encounter hostname such as svk.en.cx", parameter)
	}
	if strings.EqualFold(host, current) {
		return current, nil
	}
	return host, nil
}

func agentScenarioSource(ctx context.Context, cfg *config, current *encx.Client, rawDomain string) (*config, *encx.Client) {
	return agentScenarioClient(ctx, cfg, current, rawDomain, "source")
}

func agentScenarioClient(ctx context.Context, cfg *config, current *encx.Client, rawDomain, role string) (*config, *encx.Client) {
	domain, err := scenarioDomain(rawDomain, cfg.domain, role+"_domain")
	if err != nil {
		fatal("%v", err)
	}
	domainCfg := *cfg
	domainCfg.domain = domain
	if domain == cfg.domain {
		requireAdminAuth(ctx, &domainCfg, current)
		return &domainCfg, current
	}
	// Credentials and cookies are domain-scoped; never reuse the current
	// domain's explicit credentials or cookie jar on another host.
	domainCfg.login, domainCfg.password = "", ""
	domainClient := encx.New(domain, appendEncOpts(&domainCfg)...)
	if !loadSession(&domainCfg, domainClient) {
		fatal("No saved session for %s domain %s. Log into that domain first", role, domain)
	}
	if err := domainClient.VerifyAdminSession(ctx); err != nil {
		fatal("%s domain %s authentication failed; log into that domain: %v", role, domain, err)
	}
	return &domainCfg, domainClient
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
