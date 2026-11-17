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

- **RSS feeds**: `/rss add` follows an RSS 2.0 or Atom feed and posts new items
  into a channel. Posts keep the feed's text and re-upload its images and short
  videos, so media survives expiring CDN links and hotlink blocks. Media over
  the size cap stays on the source post, which the embed links to. bdsmlr posts
  are resolved through bdsmlr's post API, because the feed can advertise an
  original GIF that now exists only as an MP4.
- **Reddit feeds**: reddit retired its `.rss` endpoints, so `/rss add` also
  accepts subreddit and user URLs and reads their newest-first listing
  directly. Every Reddit post gets a **Load full post** or **Load gallery**
  button that fetches the complete gallery or video on demand, then removes
  the placeholder post. See [Reddit feeds](#reddit-feeds).
- `/rss add url:<url> [channel] [backfill]` - follow a feed, posting the newest
  three items right away
- `/rss remove feed:<feed>` - stop following a feed
- `/rss list` - show each feed's username, domain, channel, last check, and last error
- `/rss test url:<url>` - preview the newest item without saving the feed
- `/rss check` - poll every feed now

Feeds are polled once an hour. Each item is recorded in SQLite by GUID, so a
restart, a reconnect, or a duplicate feed entry never posts twice. Feed posts
need a channel where the bot can View Channel, Send Messages, Embed Links, and
Attach Files. Sources are often NSFW, so an age-restricted channel is
recommended.

### Reddit feeds

Reddit no longer serves RSS for subreddits or users, and its listing pages now
require a signed-in session. Set `REDDIT_SESSION` to the `reddit_session`
cookie from a logged-in browser:

1. Sign in at `https://old.reddit.com`.
2. Open DevTools, then Application, then Cookies, then `https://old.reddit.com`.
3. Copy the `reddit_session` value into `REDDIT_SESSION`.

Both the bare value and a full `reddit_session=...` cookie header are
accepted. The cookie is never logged. Without it, Reddit feeds fail with a
clear error and `/rss list` shows it.

Supported URLs include the plain page, the `.rss` shapes, listing sorts, and
user profiles:

```
https://reddit.com/r/cats
https://www.reddit.com/r/cats/new
https://www.reddit.com/r/cats/.rss
https://www.reddit.com/user/someone
https://www.reddit.com/u/someone/submitted.rss
```

All of them are normalized to `https://www.reddit.com/r/<name>` or
`https://www.reddit.com/user/<name>`, and a feed always follows the newest
posts, regardless of the sort in the URL. Search pages and post permalinks are
rejected.

`REDDIT_INCLUDE_NSFW` defaults to `true`, so NSFW communities and profiles can
be followed. It sends Reddit's `over18` cookie and `include_over_18=on`.
Turn it off to keep the bot out of NSFW listings entirely.

Every Reddit card names the poster as `u/<name>` in its footer. The first post
shows what the listing carries: the full image for image posts, the cover for
galleries, and a still frame for videos and link posts. Gallery posts are
labelled **Gallery** in the footer and get a **Load gallery** button instead of
**Load full post**. Press the button to fetch the post's detail and re-upload
every gallery image or the video's MP4. The button is available to every
member; after a successful load the bot removes the original post, so the
channel keeps only the complete one. Reddit's MP4 fallback is video-only, so
loaded videos have no audio.

Commands are limited to server admins for now: members need **Manage Server**
or **Administrator**. The welcome confirmation button stays open to every
member.

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
| `RSS_POLL_INTERVAL` | no | `1h` | Feed poll interval (`30m`, `2h`) |
| `RSS_MAX_ATTACHMENT_BYTES` | no | `10485760` | Largest media file re-uploaded to Discord; accepts `10MiB` |
| `REDDIT_SESSION` | for Reddit feeds | | `reddit_session` cookie from a logged-in browser |
| `REDDIT_USER_AGENT` | no | Firefox desktop | User-Agent for Reddit requests; Reddit rejects generic agents |
| `REDDIT_INCLUDE_NSFW` | no | `true` | Allow NSFW subreddits and profiles (`over18`, `include_over_18=on`) |

Copy `.env.example` to `.env` for local development. Real environment
variables override `.env`, and database settings override the seed values
after the first `/welcome setup`.

## Discord setup

1. Create a role for members, for example `Member`.
2. Invite the bot with scopes `bot applications.commands` and permissions
   Manage Roles, Manage Messages, Send Messages, Embed Links, Attach Files,
   Read Message History, View Channels:
   `https://discord.com/oauth2/authorize?client_id=<CLIENT_ID>&scope=bot%20applications.commands&permissions=268561408`
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
