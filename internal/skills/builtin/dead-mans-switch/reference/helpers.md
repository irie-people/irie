# Irie's helpers: `irie dms`

Run them as `"$IRIE_BIN" dms …`. Every Irie session has `IRIE_BIN`. They keep the Healthchecks API key and the checks' ping URLs in Irie's state folder, readable only by the user, and they never print either. Never `cat`, copy or print those files, and never put a ping URL in a file, a message or the calendar: whoever has one can check in for the user, or set the alarm off at once.

| Command | What it does | Who runs it |
|---|---|---|
| `dms key --paste` | Keeps the API key the user just copied, then empties the clipboard. Prints only its last four characters, and whether it works | The user, as one line with an exclamation mark in front, after copying the key |
| `dms checks --alarm-name "…" --alarm-desc-file FILE [--every 14] [--remind 2] [--alarm 16]` | Makes the reminder and the alarm checks, or brings them up to date. Assigns no integrations | You |
| `dms status` | Each check's state, the integrations it emails, and what is left to do | You, after every step the user takes in Healthchecks |
| `dms ping` | Checks in from this computer | You, to test; the user, any time |
| `dms pause`, `dms resume` | Pauses both checks so nobody is emailed, or starts them again. A check-in also starts them again | The user decides; you may run it when they ask |
| `dms calendar --out FILE [--first YYYY-MM-DD] [--at HH:MM] [--review YYYY-MM-DD]` | Writes the calendar file: a check-in reminder every period, and a yearly review | You |
| `dms shortcut --out FILE` | Writes the iPhone shortcut that checks in to both checks (`phone.md`) | You |

## The order

1. The user makes a Healthchecks account and a project, and an API key in the project's settings (`healthchecks.md`).
2. They copy the key, and type: `! "$IRIE_BIN" dms key --paste`
3. With the user, write the alarm's description into `alarm.txt` in their folder: what has happened, the first steps, where the letter is, and who to call. It is what the people will read in the alarm email. Keep it short and kind. No secrets.
4. Agree the alarm's name with them. It is the subject of the people's email, so it says what to do, such as: `Alice has not checked in for 30 days: please read this`. At most 100 characters.
5. Run `"$IRIE_BIN" dms checks --alarm-name "…" --alarm-desc-file "<folder>/alarm.txt"`, with their rhythm if it isn't 14, 2 and 16 days.
6. The user adds the email integrations in Healthchecks and ticks them for the right check (`healthchecks.md`). Run `"$IRIE_BIN" dms status` after, and go on only when it says what you expect.
7. The tap (`phone.md`), then one check-in, then `dms status` again: both checks should be up.
