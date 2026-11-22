# chastisafe

A small, typed Go client for the ChastiSafe lock service at
`https://chastisafe.com`.

ChastiKey is gone. Its old API host `api.chastikey.com` no longer resolves.
ChastiSafe is the replacement, but it has no published API. The endpoints,
payloads and quirks in this package were recovered from the ChastiSafe Flutter
web client and checked against a real account.

This package is intentionally independent of Discord and SQLite. A future bot
feature can depend on it without pulling in bot code.

## Authentication

Send the account's developer code in the `chastisafedev` header and the
`chastisafe` cookie:

```
chastisafedev: <developer code>
Cookie: chastisafe=<developer code>
```

The live service currently accepts the cookie form and can return a server
error for header-only requests, so the client sends both. Copy the code from the
signed-in web app.

Treat the code as a password:

- never log it
- never commit it
- store it only for the account that owns it
- validate it with `Who` before trusting it

`Who` returns ChastiSafe's linked Discord snowflake. Compare it with the
Discord user who supplied the code before storing anything:

```go
identity, err := client.Who(ctx)
if err != nil {
    return err
}
if !identity.OwnedBy(discordUserID) {
    return errors.New("this ChastiSafe code belongs to another Discord user")
}
```

## Usage

```go
client, err := chastisafe.New(chastisafe.Options{
    Token:  code,
    Logger: log,
})
if err != nil {
    return err
}

locks, err := client.Loaded(ctx)
if err != nil {
    return err
}
for _, lock := range locks {
    fmt.Println(chastisafe.LockSummary(&lock))
}
```

`Client` is bound to one account. `WithToken` shares the HTTP client with a
different account, which is useful for a multi-member Discord bot.

## Primitive and service layers

`Client` is the primitive layer. It maps one method to one endpoint and keeps
the API's raw shapes.

`Service` is the convenience layer for a bot. It adds short-lived caches,
reference resolution, typed drafts, and higher-level operations:

```go
service := chastisafe.NewService(client)

template, err := service.ResolveTemplate(ctx, "Steel")
description := "Updated description"
updated, err := service.UpdateTemplate(ctx, template.ID.String(), chastisafe.TemplatePatch{
    Description: &description,
})

view, err := service.Lock(ctx, "Steel")
detail := chastisafe.LockDetailText(view)
```

`Service` resolves templates by ID, exact name, or unique prefix. Locks resolve
by ID, lock name, lockee name, keyholder name, or unique prefix. Ambiguous
references return `ErrRefAmbiguous` with the candidate labels.

Typed helpers:

- `TemplateDraft` builds the large create payload ChastiSafe expects.
- `TemplatePatch` sends only the fields a caller sets.
- `LockSettingsPatch` converts typed durations into the units `KeyholderSettings` uses.
- `LockFilter` and `LockView` select running locks and attach the caller's role.
- `ParseDuration`, `ParseCards`, and `ParsePurposes` handle Discord option text.
- `LocksText`, `TemplateDetailText`, `LockListText`, `LockDetailText`, and
  `ProfileText` render text without combinations or passcodes.

Caches last 20 seconds by default. Every write invalidates the relevant cache.
Call `Service.Invalidate` after an account change such as a new login.

## Stats leaderboard

`Leaderboard` reads the public `#stats` page. The criteria are `Chastity`,
`Task`, `Bondage`, `Loyalty` and `Total`:

```go
rows, err := client.Leaderboard(ctx, chastisafe.StatsChastity)
for _, row := range rows {
    fmt.Println(row.Name, row.ChastityLocks, row.TotalLocks, row.Visible)
}
```

`Visible` is the account's public-profile flag. The endpoint includes hidden
profiles, so filter on `Visible` when the bot should only show public accounts.

## Public profiles

`ProfileFor` reads `/profile/{user}`. It accepts a numeric ChastiSafe user ID
or a bare username, and strips the `#0` discriminator from usernames.

If the requested account has hidden its profile, ChastiSafe returns the
caller's own profile instead of an error. `ProfileFor` detects that and returns
`ErrProfileNotPublic`, so the bot does not accidentally present the caller's
stats as someone else's.

## Units

The API mixes seconds and minutes.

Seconds:

- `timeremaining`
- `timerMin`, `timerMax`
- `addtime`, `remainingtime`
- `wait` (next card pick)
- `timetonextcheckin`
- `timetohygieneunlock`
- `freezeDuration`
- `nextTaskPick`, `nextTaskComplete`
- `remainingguaranteed`
- `trusttime`
- `lockduration`

Minutes:

- `regularity` (card pick interval)
- `checkinperiod`, `checkintolerance`
- `hygieneunlockperiod`, `hygieneunlockduration`
- `autoresetperiod`
- `taskMinRegularity`

The client exposes `FormatDuration`, `FormatMinutes` and
`FormatShortDuration` so callers do not have to remember this.

## Endpoints used

Read:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/who` | Current account and linked Discord ID |
| GET | `/profile` | Profile, stats, developer code |
| GET | `/profile/{user}` | Another public profile (numeric ID or bare username) |
| GET | `/locks` | Managed lock templates |
| GET | `/loadable/{keyholder}` | Templates a keyholder offers |
| GET | `/getlock/{id}` | One template |
| GET | `/getlockforedit/{id}` | One template with edit fields |
| GET | `/loaded` | Active locks for this account |
| GET | `/loaded/{id}` | One active lock, plus `isLockee` |
| GET | `/lockees` | Active locks of this account's lockees |
| GET | `/lockee/{id}` | One lockee's active lock |
| GET | `/recent` | Completed lock history |
| GET | `/stats/{criterion}` | Public stats leaderboard |
| POST | `/log` | Lock event log |
| GET | `/tasks`, `/task/{id}` | Task rollers |
| GET | `/taskstatus/{lock}` | Task state for a lock |
| GET | `/pillory`, `/pillory/{lock}` | Pillory sessions |
| GET | `/scripts/{template}` | Saved scripts |
| GET | `/bots/card`, `/bots/timer` | Built-in keyholder bots |

Lockee actions:

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/load` | Start a lock from a template |
| GET | `/open/{id}` | Open a ready lock and reveal its code |
| GET | `/abandon/{id}` | End a lock without a code |
| GET | `/reset/{id}` | Restart a lock |
| GET | `/random/{id}` | Random unlock attempt |
| GET | `/pick/{id}` | Draw a card |
| POST | `/update` | Add or remove timer time |
| GET | `/checkin/{id}` | Record a check-in |
| GET | `/keyholderdecide/{id}` | Ask the keyholder to decide |
| POST | `/hygiene` | Start a hygiene unlock with a code |
| GET | `/hygieneskip/{id}` | Skip a hygiene unlock |
| GET | `/putgreenback/{id}` | Return the last green card |
| POST | `/respond/{id}` | Answer a keyholder prompt |
| POST | `/trust` | Extend the lockee trust period |
| GET | `/taskpick/{lock}` | Draw a task |
| GET | `/taskcomplete/{lock}` | Complete a task |
| GET | `/taskabandon/{lock}` | Abandon a task |
| POST | `/vote` | Vote in a pillory |
| POST | `/pillorytaskdone` | Finish a pillory task |

Keyholder actions:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/unlock/{id}` | Unlock a lockee's ready lock |
| POST | `/update` | Freeze, hide, purpose, remaining time, settings |
| POST | `/update` | Add or remove cards |
| POST | `/clear` | Clear warning flags |
| POST | `/split` | Create fake locks |
| POST | `/taskassign` | Assign a task roller item |
| POST | `/startpillory` | Start a pillory |
| GET | `/cancelpillory/{id}` | Cancel a pillory |
| POST | `/script` | Run a script |
| POST | `/button` | Press a script-backed button |

Template management:

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/create` | Create a template, or replace when `id` is set |
| POST | `/updatetemplate` | Partial update using `templateId` |
| GET | `/delete/{id}` | Delete a template |
| POST | `/setbackground` | Change a template background |
| POST | `/updatecardrange` | Change one card range on a template |
| POST | `/simulate` | Preview card duration and card counts |

## Verified behaviour

Checked against a live account:

- header and cookie authentication with `chastisafedev` and `chastisafe`
- `/who`, `/profile`, `/locks`, `/loaded`, `/recent`
- `/stats/{criterion}` leaderboard
- `/profile/{id}` public profiles, including hidden-profile detection
- `POST /load` returns `{"locks":[...]}`
- `GET /open/{id}` opens a ready lock and returns its code
- `GET /abandon/{id}` ends a lock
- `GET /pick/{id}` returns the card and the updated lock
- `POST /update` with `addtime` adds timer seconds for a lockee when the
  template allows updates
- `POST /update` with `cards` works for a lockee when the template allows
  updates and the card is a yellow variant
- a lockee is denied when trying to modify a green card
- template create, partial update, card range edit, background, simulate and
  delete
- with a second account: `/lockees`, keyholder freeze, hide, remaining time,
  purpose change, card edits, split and unlock
- lockee trust extension
- a self-lock returns `locknotfound` for `frozenbykh`, `hidden`,
  `remainingtime` and similar keyholder fields

Not yet checked against a real keyholder account:

- keyholder task, pillory and script actions

The request shapes for those actions come from the web client. A self-lock
cannot exercise them because its keyholder is `<self>` and ChastiSafe denies
keyholder writes with `locknotfound`.

## Quirks

- `keyholderId` is a JSON string in loaded locks and a JSON number in
  templates. `ID` accepts both.
- Loaded timestamps are RFC3339 strings. Log and history timestamps are epoch
  seconds. `Time` accepts both.
- `status` is sometimes `0`/`1` and sometimes `false`/`true`. `Status` accepts
  both.
- Some responses put the updated lock at the top level. Others return
  `{"status":0}`. The client handles both.
- `POST /update` succeeds for a self-lock with `addtime` but not with
  keyholder fields.
- Loaded-lock `frozenbykh`, `hidden` and `cumulative` require JSON booleans.
  Template `hidden`, `startfrozen` and `cumulative` are the strings `yes`,
  `no` or `randomize`.
- `POST /trust` is a lockee action. It works with a duration in seconds.
  Calling it as the keyholder returns an internal error.
- `SetPurpose` works for an allowed purpose. In testing, changing a lock whose
  purpose was already `TEST` back to `CHASTITY` returned `locknotfound`.
- `GET /open/{id}` is the lockee path. `GET /unlock/{id}` is the keyholder
  path.
- `deck` is a map of card name to remaining count. The app also accepts the
  lowercase alias `yellow`, and a loaded deck can report a specific yellow
  variant as `YELLOW`. Use `Deck.Yellows()` when the variant does not matter.
- `requiredgreens` values above 1000 mean "all greens". Values of 1000 or less
  mean a fixed number of greens.
- `GET /profile/{user}` accepts a numeric user ID or a bare username. Strip the
  `#0` discriminator before building the path. A hidden profile returns the
  caller's profile with a different ID, which `ProfileFor` rejects.

## Integration tests

Read-only, skipped unless `CHASTISAFE_DEV_CODE` is set:

```sh
CHASTISAFE_DEV_CODE=... go test -run TestIntegrationRead -v ./internal/chastisafe
```

Destructive, skipped unless `CHASTISAFE_WRITE_TEST=1` is also set. These create
and delete a template, load a lock, and clean up:

```sh
CHASTISAFE_DEV_CODE=... CHASTISAFE_WRITE_TEST=1 \
  go test -run 'TestIntegration(Template|Timer)Lifecycle|TestIntegrationLockeeCardUpdate' -v ./internal/chastisafe
```

Two-account keyholder test:

```sh
CHASTISAFE_WRITE_TEST=1 \
  CHASTISAFE_KEYHOLDER_DEV_CODE=... \
  CHASTISAFE_LOCKEE_DEV_CODE=... \
  go test -run TestIntegrationKeyholderActions -v ./internal/chastisafe
```

Do not run the write tests against an account that matters.
