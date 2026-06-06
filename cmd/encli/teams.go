package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/skrashevich/encx-cli/encx"
)

func cmdTeamInfo(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	teamID := teamIDArg(args)
	info, err := client.GetTeamManagementInfo(ctx, teamID)
	if err != nil {
		fatal("Failed to get team info: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(info)
		return
	}
	if info.TeamName != "" {
		fmt.Printf("Team: %s (ID: %d)\n", info.TeamName, info.TeamID)
	} else {
		fmt.Printf("Team ID: %d\n", info.TeamID)
	}
	if len(info.PendingInvitations) > 0 {
		fmt.Println("\nPending invitations:")
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "User ID\tLogin")
		for _, inv := range info.PendingInvitations {
			fmt.Fprintf(w, "%d\t%s\n", inv.UserID, inv.Login)
		}
		w.Flush()
	}
	if len(info.Actions) > 0 {
		fmt.Println("\nActions:")
		for action := range info.Actions {
			fmt.Printf("  %s\n", action)
		}
	}
}

func cmdTeamInvitations(ctx context.Context, cfg *config, client *encx.Client) {
	invitations, err := client.GetTeamInvitations(ctx)
	if err != nil {
		fatal("Failed to get team invitations: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(invitations)
		return
	}
	if len(invitations) == 0 {
		fmt.Println("No incoming team invitations")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Team ID\tTeam")
	for _, inv := range invitations {
		fmt.Fprintf(w, "%d\t%s\n", inv.TeamID, inv.Name)
	}
	w.Flush()
}

func cmdTeamInvite(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: encli team-invite <team-id> <login>")
	}
	teamID := parsePositiveIntArg("team ID", args[0])
	login := strings.TrimSpace(args[1])
	if err := client.InviteTeamMember(ctx, teamID, login); err != nil {
		fatal("Failed to invite team member: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID, "login": login}, "Invitation sent")
}

func cmdTeamRemoveInvitation(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: encli team-remove-invitation <team-id> <user-id>")
	}
	teamID := parsePositiveIntArg("team ID", args[0])
	userID := parsePositiveIntArg("user ID", args[1])
	if err := client.RemoveTeamInvitation(ctx, teamID, userID); err != nil {
		fatal("Failed to remove invitation: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID, "user_id": userID}, "Invitation removed")
}

func cmdTeamAccept(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	teamID := teamIDArg(args)
	if err := client.AcceptTeamInvitation(ctx, teamID); err != nil {
		fatal("Failed to accept invitation: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID}, "Invitation accepted")
}

func cmdTeamReject(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	teamID := teamIDArg(args)
	if err := client.RejectTeamInvitation(ctx, teamID); err != nil {
		fatal("Failed to reject invitation: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID}, "Invitation rejected")
}

func cmdTeamJoinRequest(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) == 0 {
		fatal("Usage: encli team-join-request <team-name>")
	}
	name := strings.Join(args, " ")
	if err := client.RequestTeamMembership(ctx, name); err != nil {
		fatal("Failed to request team membership: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team": name}, "Join request sent")
}

func cmdTeamLeave(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	teamID := teamIDArg(args)
	if err := client.LeaveTeam(ctx, teamID); err != nil {
		fatal("Failed to leave team: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID}, "Left team")
}

func cmdTeamRename(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: encli team-rename <team-id> <new-name>")
	}
	teamID := parsePositiveIntArg("team ID", args[0])
	name := strings.Join(args[1:], " ")
	if err := client.RenameTeam(ctx, teamID, name); err != nil {
		fatal("Failed to rename team: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID, "name": name}, "Team renamed")
}

func cmdTeamSetSite(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: encli team-set-site <team-id> <url>")
	}
	teamID := parsePositiveIntArg("team ID", args[0])
	site := strings.Join(args[1:], " ")
	if err := client.SetTeamSite(ctx, teamID, site); err != nil {
		fatal("Failed to set team site: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID, "site": site}, "Team site updated")
}

func cmdTeamSetForum(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	if len(args) < 2 {
		fatal("Usage: encli team-set-forum <team-id> <url>")
	}
	teamID := parsePositiveIntArg("team ID", args[0])
	forum := strings.Join(args[1:], " ")
	if err := client.SetTeamForum(ctx, teamID, forum); err != nil {
		fatal("Failed to set team forum: %v", err)
	}
	printTeamOK(cfg, map[string]any{"success": true, "team_id": teamID, "forum": forum}, "Team forum updated")
}

func teamIDArg(args []string) int {
	if len(args) == 0 {
		fatal("Missing team ID")
	}
	return parsePositiveIntArg("team ID", args[0])
}

func parsePositiveIntArg(label, value string) int {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		fatal("Invalid %s: %s", label, value)
	}
	return id
}

func printTeamOK(cfg *config, payload map[string]any, message string) {
	if cfg.jsonOutput {
		outputJSON(payload)
		return
	}
	fmt.Println(message)
}
