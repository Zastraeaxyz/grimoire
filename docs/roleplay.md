# Roleplay bot

Grimoire can run a second Discord bot from the same binary and the same deploy.
The second bot is the server's roleplay keyholder. It has its own token, its
own commands, its own persona prompt, and its own message intents.

It stays completely offline unless `DISCORD_ROLEPLAY_TOKEN` is set.

## What it does today

- Answers when a member mentions it or replies to one of its messages.
- Reads image attachments on the current message and on the message it replies
  to, so a member can show rather than describe.
- Any member can talk to it by default. Setting the lockee role limits it to
  members with that role.
- Can check the caller's ChastiSafe lock status with a tool, only when locks or
  chastity are already part of the conversation.
- Can add time as a punishment and freeze or thaw a timed lock.
- Exposes `/chastity status` so a member can check their own lock privately.
- Exposes `/prompt` to admins so the persona can be reviewed and tuned.
- Uses no memory yet.

## Setup

1. Create a second Discord application and bot.
2. In the Bot settings, enable the **Message Content** intent. Nothing else is
   required for the gateway.
3. Invite the bot with the `bot` and `applications.commands` scopes.
4. Set `DISCORD_ROLEPLAY_TOKEN` and `OPENROUTER_API_KEY` on the service.
5. Optionally set `ROLEPLAY_GUILD_ID` to restrict the persona to one server.
   Empty serves every guild it is invited to.
6. Optionally create a lockee role and set `ROLEPLAY_LOCKEE_ROLE_ID` to its ID.
   Without it, any member can talk to the persona.
7. Optionally edit `prompts/keyholder.md`. The file is copied into the image.

The persona needs its own Discord identity. The username and avatar live in the
Discord Developer Portal, not in this repository.

If the token is set but the OpenRouter key or the prompt is missing, the persona
logs an error and Grimoire keeps running.

## Images

Image attachments on the mentioning message and on the message being replied to
are passed to the model as multimodal content, with a short label that names
the file. That lets a member send a picture with no text at all.

Only images Discord hosts are used, so the model never fetches an arbitrary
link. Accepted types are PNG, JPEG, WebP, and GIF. At most four images go per
request, and each must be 8 MB or smaller. History stays text-only; images from
older messages are not re-sent.

The configured model must support vision. With a text-only slug the request
fails and Raven answers that she lost her train of thought, so point
`OPENROUTER_MODEL` at a vision-capable model. Turn the feature off with
`ROLEPLAY_VISION_ENABLED=false`.

## ChastiSafe

The persona acts as one ChastiSafe keyholder account. Put that account's
developer code in `CHASTISAFE_KEYHOLDER_CODE`.

The developer code is a password. It is sent in the `chastisafedev` header,
never logged, and never written to the database. Raven uses this one account and
never reads the per-member codes stored by the main bot.

Grimoire itself has separate member-scoped ChastiSafe commands under `/cs`. A
member runs `/cs login` on the main bot, and that member's encrypted code is
used only for their own `/cs status`, `/cs lockees`, `/cs templates`, and
`/cs locks`. See
[Member ChastiSafe access](../README.md#member-chastisafe-access). Raven is not
part of that path and keeps using only `CHASTISAFE_KEYHOLDER_CODE`.

Where to find the code: sign in to ChastiSafe, open your profile, and copy the
developer code shown there.

The model sees the lock status as a tool. It is not injected into every prompt,
so she only looks when the conversation makes it relevant. Most conversations
never touch ChastiSafe.

### Connecting a lockee

ChastiSafe does not let a keyholder attach a lockee through its API. The lockee
starts the relationship:

1. Create at least one timed lock template in the persona's ChastiSafe account
   so a lockee has something to load. Enable updates on it so punishments can
   add time. The bot does not create templates.
2. The member runs `/chastity status`.
3. With no active lock, the bot replies with its keyholder name, ChastiSafe ID,
   and the templates it offers.
4. The member adds that account as their keyholder in the ChastiSafe app and
   loads a timed lock.
5. The bot sees the lock the next time the member talks to it.

Recognition uses the Discord ID linked to the ChastiSafe account, so there is
no separate allowlist to maintain.

## Commands

| Command | Who | Result |
| --- | --- | --- |
| `/chastity status` | Any member | Ephemeral list of the caller's active locks, or connection instructions |
| `/prompt` | Admins | Ephemeral copy of the persona prompt and runtime tool notes |
| Mentions and replies | Any member allowed to talk to her | An in-character reply; lock status is available as a tool, not injected |

Admins can run `/chastity status` because slash command permissions always allow
server admins.

## Punishment

Raven has house rules in her prompt, starting with the rule that no sub may
change his mind without her permission. When a rule breaks, she can call
`chastity_punish`, which adds time to the sub's timed lock.

The amount is 5% of the remaining time, clamped to 30 minutes at the low end and
4 hours at the high end. A two hour lock takes a 30 minute hit. A two day lock
takes about 2 hours 24 minutes. Tune it with the variables below.

Raven can also freeze or thaw a timed lock. She cannot unlock a lock, create
one, or change a card lock.

## Limits

This version only touches timed locks, and only through three tools.

- It can read status, add time, and freeze or thaw a timed lock.
- It cannot unlock, remove cards, create a lock, assign a task, or start a
  pillory.
- The system prompt tells the persona this explicitly, so it does not pretend
  to have controls it lacks.
- Lock combinations and unlock codes are never placed in the prompt, the reply,
  or the logs.
- Status is a tool, not a prompt injection. The model sees it in its toolbelt
  and calls it only when chastity or locks are already in the conversation.
- Status responses are ephemeral. Channel mentions do not quote private lock
  details beyond what the persona chooses to say.
- Message content is never logged. Only channel, member, timing, and token
  counts are recorded.

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `DISCORD_ROLEPLAY_TOKEN` | | Turns the persona on |
| `ROLEPLAY_GUILD_ID` | all guilds | Optional server lock |
| `ROLEPLAY_BOT_NAME` | `Keyholder` | Fills `{{bot}}` in the prompt |
| `ROLEPLAY_LOCKEE_ROLE_ID` | | Optional role gate; empty allows any member |
| `ROLEPLAY_SYSTEM_PROMPT_FILE` | `./prompts/keyholder.md` | Persona prompt |
| `ROLEPLAY_SYSTEM_PROMPT` | | Inline override; wins over the file |
| `ROLEPLAY_CONTEXT_MESSAGES` | `15` | Channel history per request |
| `ROLEPLAY_MAX_CHUNKS` | `3` | Reply split limit |
| `ROLEPLAY_TIMEOUT_SECONDS` | `60` | Model request timeout |
| `ROLEPLAY_MAX_CONCURRENT` | `2` | Concurrent model calls |
| `ROLEPLAY_RATE_LIMIT_CAPACITY` | `2` | Requests per member per window |
| `ROLEPLAY_RATE_LIMIT_SECONDS` | `10` | Window length |
| `ROLEPLAY_VISION_ENABLED` | `true` | Read image attachments |
| `ROLEPLAY_VISION_MAX_IMAGES` | `4` | Images per request |
| `ROLEPLAY_VISION_MAX_IMAGE_MB` | `8` | Largest image read |
| `ROLEPLAY_VISION_DETAIL` | `auto` | `auto`, `low`, `high`, or `original` |
| `ROLEPLAY_PUNISH_PERCENT` | `5` | Percent of remaining time added per punishment |
| `ROLEPLAY_PUNISH_MIN_MINUTES` | `30` | Smallest punishment |
| `ROLEPLAY_PUNISH_MAX_MINUTES` | `240` | Largest punishment |
| `ROLEPLAY_CHASTITY_ENABLED` | `true` | ChastiSafe status and keyholder tools |
| `CHASTISAFE_KEYHOLDER_CODE` | | ChastiSafe keyholder developer code |
| `CHASTISAFE_BASE_URL` | `https://chastisafe.com/api-v1` | API root override |

A missing keyholder code disables only the ChastiSafe status tool. Grimoire and
the rest of the persona keep running. A bad code logs a warning at startup and
disables the tool for that process.

## Running locally

```sh
DISCORD_ROLEPLAY_TOKEN=... \
OPENROUTER_API_KEY=... \
CHASTISAFE_KEYHOLDER_CODE=... \
go run .
```

Both bots connect from the same process. The persona can be tested against a
test ChastiSafe keyholder account before the real one is connected.

## Safety

This is a consensual adults-only space. The persona is instructed to treat
limits and safewords as absolute and to answer plainly when a scene stops. Since
this version cannot touch a lock, a safeword needs no automated release path
yet. A lockee must always keep an independent way out, such as the ChastiSafe
app or a physical key. A Discord bot should never be the only route to release.
