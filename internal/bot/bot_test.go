package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCanUseBot(t *testing.T) {
	cases := []struct {
		name   string
		member *discordgo.Member
		want   bool
	}{
		{name: "no member", member: nil, want: false},
		{name: "no permissions", member: &discordgo.Member{}, want: false},
		{
			name:   "send messages only",
			member: &discordgo.Member{Permissions: discordgo.PermissionSendMessages},
			want:   false,
		},
		{
			name:   "manage server",
			member: &discordgo.Member{Permissions: discordgo.PermissionManageGuild},
			want:   true,
		},
		{
			name:   "administrator",
			member: &discordgo.Member{Permissions: discordgo.PermissionAdministrator},
			want:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{Member: tc.member},
			}
			if got := canUseBot(i); got != tc.want {
				t.Fatalf("canUseBot = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCommandDenial(t *testing.T) {
	newBot := func(lookup UserRoleLookup) *Bot {
		return &Bot{
			Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			userCommands: map[string]struct{}{"bdsmlr": {}},
			userRole:     lookup,
		}
	}
	roleLookup := func(context.Context) (string, error) { return "role-1", nil }
	interaction := func(member *discordgo.Member) *discordgo.InteractionCreate {
		return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Member: member}}
	}

	cases := []struct {
		name    string
		bot     *Bot
		member  *discordgo.Member
		command string
		want    string
	}{
		{
			name:    "admin on a user command",
			bot:     newBot(roleLookup),
			member:  &discordgo.Member{Permissions: discordgo.PermissionManageGuild},
			command: "bdsmlr",
			want:    "",
		},
		{
			name:    "member with the configured role",
			bot:     newBot(roleLookup),
			member:  &discordgo.Member{Roles: []string{"role-1"}},
			command: "bdsmlr",
			want:    "",
		},
		{
			name:    "member without the configured role",
			bot:     newBot(roleLookup),
			member:  &discordgo.Member{Roles: []string{"role-2"}},
			command: "bdsmlr",
			want:    "role-1",
		},
		{
			name:    "member on an admin-only command",
			bot:     newBot(roleLookup),
			member:  &discordgo.Member{Roles: []string{"role-1"}},
			command: "rss",
			want:    "Manage Server",
		},
		{
			name:    "no role configured",
			bot:     newBot(func(context.Context) (string, error) { return "", nil }),
			member:  &discordgo.Member{Roles: []string{"role-1"}},
			command: "bdsmlr",
			want:    "/settings command-role",
		},
		{
			name:    "no role lookup",
			bot:     newBot(nil),
			member:  &discordgo.Member{Roles: []string{"role-1"}},
			command: "bdsmlr",
			want:    "/settings command-role",
		},
		{
			name:    "role lookup failure",
			bot:     newBot(func(context.Context) (string, error) { return "", errors.New("db down") }),
			member:  &discordgo.Member{Roles: []string{"role-1"}},
			command: "bdsmlr",
			want:    "could not check permissions",
		},
		{
			name:    "no member",
			bot:     newBot(roleLookup),
			member:  nil,
			command: "bdsmlr",
			want:    "role-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.bot.commandDenial(context.Background(), interaction(tc.member), tc.command)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("commandDenial = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("commandDenial = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
