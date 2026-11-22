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

Send the account's developer code in the `chastisafedev` header:

```
chastisafedev: <developer code>
```

The same value is stored in the browser cookie `chastisafe`. The header works
without the cookie. `GET /profile` returns the code as `developercode`, so a
member can copy it from their own profile.

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
| POST | `/trust` | Set a trust period |
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

- header authentication with `chastisafedev`
- `/who`, `/profile`, `/locks`, `/loaded`, `/recent`
- `POST /load` returns `{"locks":[...]}`
- `GET /open/{id}` opens a ready lock and returns its code
- `GET /abandon/{id}` ends a lock
- `GET /pick/{id}` returns the card and the updated lock
- `POST /update` with `addtime` adds timer seconds for a lockee when the
  template allows updates
- template create, partial update, simulate and delete
- a self-lock returns `locknotfound` for `frozenbykh`, `hidden`,
  `remainingtime` and similar keyholder fields

Not yet checked against a real keyholder account:

- `Unlock`
- `Freeze`, `Hide`, `SetRemainingTime`, `SetPurpose`
- `UpdateCards`, `AddCards`, `RemoveCards`
- `Split`
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
- `GET /open/{id}` is the lockee path. `GET /unlock/{id}` is the keyholder
  path.
- `deck` is a map of card name to remaining count. The app also accepts the
  lowercase alias `yellow`.
- `requiredgreens` values above 1000 mean "all greens". Values of 1000 or less
  mean a fixed number of greens.
- `GET /profile/{user}` accepts a numeric user ID or a bare username. Strip the
  `#0` discriminator before building the path.

## Integration tests

Read-only, skipped unless `CHASTISAFE_DEV_CODE` is set:

```sh
CHASTISAFE_DEV_CODE=... go test -run TestIntegrationRead -v ./internal/chastisafe
```

Destructive, skipped unless `CHASTISAFE_WRITE_TEST=1` is also set. These create
and delete a template, load a lock, and clean up:

```sh
CHASTISAFE_DEV_CODE=... CHASTISAFE_WRITE_TEST=1 \
  go test -run 'TestIntegration(Template|Timer)Lifecycle' -v ./internal/chastisafe
```

Do not run the write tests against an account that matters.
