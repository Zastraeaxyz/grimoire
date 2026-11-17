package bot

import (
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
