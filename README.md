# Grimoire

Discord bot for the zxyz server, built on
[discordgo](https://github.com/bwmarrin/discordgo).

## Features

- **Welcome panel**: a message with an "I have read the rules" button.
  Pressing it grants the configured base role. The button uses a static custom
  ID and the grant always reads Discord's live member state, so members who
  leave and rejoin can confirm again.
- `/welcome setup` - post or update the panel (Manage Server only)
- `/welcome refresh` - repair or repost the panel
- `/welcome status` - show configuration and confirmation count

## Configuration

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `DISCORD_TOKEN` | yes | | Bot token |
| `GUILD_ID` | yes | | Single server the bot serves |
| `DATABASE_PATH` | no | `./data/grimoire.db` | SQLite file (use `/data/grimoire.db` on Railway) |
| `PORT` | no | `8080` | Health endpoint port |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `WELCOME_CHANNEL_ID` | no | | Seeds settings on first boot |
| `BASE_ROLE_ID` | no | | Seeds settings on first boot |

Copy `.env.example` to `.env` for local development. Real environment
variables override `.env`, and database settings override the seed values
after the first `/welcome setup`.

## Discord setup

1. Create a role for members, for example `Member`.
2. Invite the bot with scopes `bot applications.commands` and permissions
   Manage Roles, Manage Messages, Send Messages, Embed Links, Read Message
   History, View Channels:
   `https://discord.com/oauth2/authorize?client_id=<CLIENT_ID>&scope=bot%20applications.commands&permissions=268528640`
3. Drag the bot's role above the `Member` role in Server Settings > Roles.
4. Run `/welcome setup role:@Member channel:#welcome rules:#rules`.

No privileged gateway intents are required.

## Local development

```sh
go run .
```

The bot validates its token over REST before opening the gateway, so a bad
token fails fast instead of reconnect-looping.

## Tests

```sh
go test ./...
```

## Railway

The service is built from the local `Dockerfile`:

```sh
railway up --project zxyz --service grimoire
```

Railway builds the Dockerfile automatically. Mount a volume at `/data` and set
the variables above. The entrypoint fixes volume ownership at startup and then
drops to uid 10001.
