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
  the size cap stays on the source post, which the embed links to. A post with
  one image shows it inside the embed; galleries keep their images as message
  attachments so Discord's image viewer can step through the set, with extra
  messages once ten attachments are reached. bdsmlr posts
  are resolved through bdsmlr's post API, because the feed can advertise an
  original GIF that now exists only as an MP4. Every item's categories are
  listed as hashtags in the embed description.
- **Reddit feeds**: reddit retired its `.rss` endpoints, so `/rss add` also
  accepts subreddit, user, and search URLs and reads their newest-first
  results directly. Galleries are fetched and posted in full right away;
  videos and link posts show a still frame and link to the post. RedGifs
  videos are downloaded and attached so Discord can play them. See
  [Reddit feeds](#reddit-feeds).
- **bdsmlr invites**: `/bdsmlr invite email:<address>` sends a membership
  invitation through the account site. It needs `BDSMLR_SESSION` and the member
  command role, both covered in [bdsmlr invites](#bdsmlr-invites).
- **Stats reports**: `/settings stats channel:<channel>` points the bot at a
  channel where it keeps a bbdsmlr analytics dashboard. The dashboard is
  edited in place twice a day, or on demand with `/stats post`. It shows
  visitors and pageviews for today, yesterday, and the last week, plus the
  most visited blogs from the last 7 days. See
  [Stats reports](#stats-reports).
- **Roleplay keyholder**: an optional second Discord bot with its own token,
  its own persona, and its own commands. It answers mentions and replies and
  can read a member's ChastiSafe lock status. See
  [Roleplay bot](#roleplay-bot).
- `/rss add url:<url> [channel] [backfill]` - follow a feed, posting the newest
  three items right away
- `/rss remove feed:<feed>` - stop following a feed
- `/rss list` - show each feed's username, domain, channel, last check, and last error
- `/rss test url:<url>` - preview the newest item without saving the feed
- `/rss check` - poll every feed now
- `/settings command-role role:<role>` - choose the role that may run member
  commands, or omit `role` to limit them to server admins
- `/settings bot-meta channel:<channel>` - choose where the bot posts its build
  version whenever it starts, or omit `channel` to turn the posts off
- `/settings stats channel:<channel>` - choose where the analytics dashboard
  lives, or omit `channel` to turn scheduled reports off
- `/settings status` - show the member command role, metadata channel, and
  stats channel
- `/bdsmlr invite email:<address>` - confirm and send a bdsmlr invitation
- `/stats post` - refresh the analytics dashboard now
- `/stats preview` - show a report without posting it
- `/stats status` - show the schedule, next run, and last result
- `/chastity status` - show your ChastiSafe lock status (roleplay bot)

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

Supported URLs include the plain page, the `.rss` shapes, listing sorts, user
profiles, and searches:

```
https://reddit.com/r/cats
https://www.reddit.com/r/cats/new
https://www.reddit.com/r/cats/.rss
https://www.reddit.com/user/someone
https://www.reddit.com/u/someone/submitted.rss
https://www.reddit.com/search?q=cats
```

Subreddit and user URLs are normalized to `https://www.reddit.com/r/<name>` or
`https://www.reddit.com/user/<name>`, and a feed always follows the newest
posts, regardless of the sort in the URL. Post permalinks are rejected.

### Reddit searches

A search URL follows the newest matches for its query. Use Reddit's search
syntax in `q`, including `author:`, `subreddit:`, `site:`, `selftext:`, and
`nsfw:`:

```
https://old.reddit.com/search/?q=author%3AZinguBing+subreddit%3AFemdomOralCaptions+subreddit%3Akeyholdercaptions+subreddit%3Acuckoldcaptions&include_over_18=on&t=all&sort=new
```

The bot normalizes the URL to `old.reddit.com/search`, forces `sort=new` and
`t=all`, and keeps `q`, `include_over_18`, and `type`. Paging cursors are
dropped so the feed always starts at the newest result. A
`/r/<name>/search?q=...` URL is folded into a `subreddit:<name>` term, so
`https://www.reddit.com/r/golang/search?q=generics` becomes a search for
`subreddit:golang generics`. Search results use the same card, linked poster,
up-front galleries, and still-frame videos as other Reddit feeds.

`REDDIT_INCLUDE_NSFW` defaults to `true`, so NSFW communities and profiles can
be followed. It sends Reddit's `over18` cookie and `include_over_18=on` unless
the URL already sets `include_over_18` itself. Turn it off to keep the bot out
of NSFW listings entirely.

Every Reddit card links the poster's `u/<name>` author line to their profile
and names the feed in its footer. Image posts show their full image. Gallery
posts are completed up front: the bot reads the post's detail and posts every
image as one attachment gallery, labelled **Gallery** in the footer, with extra
messages once ten attachments are reached. Videos and link posts start with a
still frame that links to the original post. RedGifs videos are resolved
through RedGifs' API and attached as an MP4, because Discord only gives
people's RedGifs links its click-to-play frame; the HD file is preferred, with
an SD fallback when it exceeds the attachment cap.

Commands that change server configuration need **Manage Server** or
**Administrator**. Commands meant for members are gated by the role chosen with
`/settings command-role`; until a role is chosen, only server admins can run
them. The welcome confirmation button stays open to every member.

## bdsmlr invites

`/bdsmlr invite email:<address>` sends an invitation from the bdsmlr account
site. The site uses a WordPress session, not the app's `v2_session` cookie, so
`BDSMLR_SESSION` must hold the full `Cookie:` header from
`account.bdsmlr.com`:

1. Sign in at `https://account.bdsmlr.com`.
2. Open DevTools, then Network, then reload any page on the account site.
3. Select the document request, copy its `Cookie` request header, and paste it
   into `BDSMLR_SESSION`. Both `wordpress_logged_in_*` and `wordpress_sec_*`
   are needed; `wordpress_test_cookie` is harmless.
4. Run `/settings command-role role:@Acolyte` once, so members with that role
   can use the command.

The bot resolves the account username from the session, reads a fresh
`_wpnonce` from the invitation form, and posts the invitation with the same
browser identity the bbdsmlr app uses. Every invite is a normal BuddyPress
submission, so pending invitations can still be reviewed or canceled on the
account site. A WordPress error notice is reported back instead of being shown
as success.

`/bdsmlr invite` never sends on the first command. It replies with the email
address and a **Send invite** button, so the member can double-check the
address and see what happens next. The invitation is sent only after the
button is pressed, and the button expires after 15 minutes. A member can send
one invitation every 24 hours; the limit is consumed only by a successful
send, so a failed attempt can be retried. Server admins are exempt from the
daily limit. A **Cancel** button discards the pending confirmation.

After a successful send, the bot posts
`An invite has been sent to your email @member` in the channel where the
command was used, so the server can see that the bot is being used. The
invited email address is never posted publicly.

`BDSMLR_SESSION` is stored in the environment like `REDDIT_SESSION`. It is
never logged, and it needs to be refreshed when the account-site session
expires.

## Stats reports

`/settings stats channel:#stats` makes the bot keep a dashboard of bbdsmlr's
anonymous Umami analytics in that channel. The message is edited in place, so
the channel holds one current report instead of a feed of stale ones. A deleted
dashboard is reposted on the next run.

The report covers today so far, yesterday with its change against the day
before, the last seven days, yesterday's average visit and bounce rate, and
the five most visited blogs of the last 7 days as links to
`bdsmlr.zastraea.xyz/<blog>`. It deliberately leaves out app internals and
visitor breakdowns, so the channel stays useful to people who do not work on
the site.

Blog names come from the page paths bbdsmlr records. Its tracker only sends
canonical paths, so `/rectrix` and `/rectrix/archive` both count as the blog
`rectrix`, while app routes like `/home`, `/login`, and `/search` are ignored.
The reserved route list mirrors bbdsmlr's `src/lib/analytics.ts`.

Reports run at 09:00 and 21:00 UTC by default. `/stats post` refreshes the
dashboard immediately, `/stats preview` shows the same report only to the
caller, and `/stats status` shows the configured channel, next run, and last
error. All three need **Manage Server**.

Umami access needs an API key from the instance:

1. Sign in to your Umami instance.
2. Open your profile menu, then Settings, then API keys.
3. Create a key and set `UMAMI_URL`, `UMAMI_WEBSITE_ID`, and `UMAMI_API_KEY`.

API keys need Umami 3.4.0 or newer. The bot reads only summary and ranking
endpoints, never raw sessions or individual visitors. If any of the three
variables is missing, the feature stays off and `/stats` explains why. Umami
errors leave the existing dashboard untouched and appear in `/stats status`.

## Bot metadata

`/settings bot-meta channel:<channel>` points the bot at a channel where it
reports which build is running. The bot posts an embed with the value of
`BUILD_VERSION` and its start time on the first gateway connection of every
process, so a deploy is visible in the channel but reconnects do not repeat
the post. Setting the channel posts once immediately, which proves the bot has
View Channel, Send Messages, and Embed Links there. Omitting `channel` turns
the posts off.

`mise deploy` sets `BUILD_VERSION` to the short commit hash and commit subject
before uploading, so each deploy reports the commit it was built from. The
value is a plain string, so it can hold a release version instead once the
project starts tagging releases.

## Roleplay bot

The repository can run a second Discord bot next to Grimoire. Both bots share
one process, one database, and one deploy. The persona has its own token,
commands, system prompt, intents, and rate limits, and it stays offline when
`DISCORD_ROLEPLAY_TOKEN` is empty. It can live in its own server; with no
`ROLEPLAY_GUILD_ID` it serves every guild it is invited to.

- It answers only mentions and replies to its own messages.
- Any member can talk to it by default. Set `ROLEPLAY_LOCKEE_ROLE_ID` to limit
  it to one role.
- It reads ChastiSafe status for the member who is talking to it.
- It has no memory and no ability to change a lock yet.
- An incomplete roleplay configuration logs an error and leaves Grimoire running.

Setup and behavior are covered in [docs/roleplay.md](docs/roleplay.md).

## Configuration

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `DISCORD_TOKEN` | yes | | Bot token |
| `GUILD_ID` | yes | | Single server the bot serves |
| `DATABASE_PATH` | no | `./data/grimoire.db` | SQLite file (use `/data/grimoire.db` on Railway) |
| `PORT` | no | `8080` | Health endpoint port |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `BUILD_VERSION` | no | `unknown` | Build label posted at startup; set by `mise deploy` |
| `WELCOME_CHANNEL_ID` | no | | Seeds settings on first boot |
| `BASE_ROLE_ID` | no | | Seeds settings on first boot |
| `RSS_POLL_INTERVAL` | no | `1h` | Feed poll interval (`30m`, `2h`) |
| `RSS_MAX_ATTACHMENT_BYTES` | no | `10485760` | Largest media file re-uploaded to Discord; accepts `10MiB` |
| `REDDIT_SESSION` | for Reddit feeds | | `reddit_session` cookie from a logged-in browser |
| `REDDIT_USER_AGENT` | no | Firefox desktop | User-Agent for Reddit requests; Reddit rejects generic agents |
| `REDDIT_INCLUDE_NSFW` | no | `true` | Allow NSFW subreddits and profiles (`over18`, `include_over_18=on`) |
| `BDSMLR_SESSION` | for bdsmlr invites | | Full `Cookie` header from a signed-in `account.bdsmlr.com` session |
| `UMAMI_URL` | for stats reports | | Umami base URL, for example `https://analytics.example.com` |
| `UMAMI_WEBSITE_ID` | for stats reports | | Website ID shown in Umami's website settings |
| `UMAMI_API_KEY` | for stats reports | | API key from Umami's Settings > API keys |
| `STATS_SITE_URL` | no | `https://bdsmlr.zastraea.xyz` | Base URL for blog links in the report |
| `STATS_TIMEZONE` | no | `UTC` | Time zone for day boundaries and report times |
| `STATS_TIMES` | no | `09:00,21:00` | Comma-separated report times, or `off` for manual only |
| `STATS_CHANNEL_ID` | no | | Seeds the stats channel on first boot; `/settings stats` wins |
| `DISCORD_ROLEPLAY_TOKEN` | no | | Second bot token; enables the roleplay persona |
| `ROLEPLAY_GUILD_ID` | no | all guilds the bot is in | Optional server lock |
| `ROLEPLAY_BOT_NAME` | no | `Keyholder` | Name the persona uses in its prompt |
| `ROLEPLAY_LOCKEE_ROLE_ID` | no | | Optional role gate; empty lets any member talk to the persona |
| `ROLEPLAY_SYSTEM_PROMPT_FILE` | no | `./prompts/keyholder.md` | Persona prompt file |
| `ROLEPLAY_SYSTEM_PROMPT` | no | | Inline persona override |
| `ROLEPLAY_CONTEXT_MESSAGES` | no | `15` | Channel messages sent as context |
| `ROLEPLAY_MAX_CHUNKS` | no | `3` | Largest reply, in Discord messages |
| `ROLEPLAY_TIMEOUT_SECONDS` | no | `60` | Model request timeout |
| `ROLEPLAY_MAX_CONCURRENT` | no | `2` | Concurrent model calls |
| `ROLEPLAY_RATE_LIMIT_CAPACITY` | no | `2` | Requests per member per window |
| `ROLEPLAY_RATE_LIMIT_SECONDS` | no | `10` | Rate limit window |
| `OPENROUTER_API_KEY` | with roleplay | | OpenRouter key |
| `OPENROUTER_MODEL` | no | `deepseek/deepseek-v4.1-flash-1017` | Model slug |
| `OPENROUTER_BASE_URL` | no | `https://openrouter.ai/api/v1` | API root |
| `ROLEPLAY_CHASTITY_ENABLED` | no | `true` | Read-only ChastiSafe status |
| `CHASTISAFE_KEYHOLDER_CODE` | for chastity | | Persona account's developer code |
| `CHASTISAFE_BASE_URL` | no | `https://chastisafe.com/api-v1` | ChastiSafe API root |

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

No privileged gateway intents are required for Grimoire. The optional
roleplay bot needs the **Message Content** intent enabled in its own Discord
application, covered in [docs/roleplay.md](docs/roleplay.md).

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
