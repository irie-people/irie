# The check-in: Healthchecks.io

Healthchecks.io watches for a ping. A check expects one every *period*; when the period and then its *grace time* pass without one, the check goes down and emails the integrations ticked for it, once. The switch has two checks, because a check emails all its integrations at the same moment and the user must hear first:

| Check | Period | Grace | Emails |
|---|---|---|---|
| The reminder | the rhythm's period (14 days) | its reminder days (2) | only the user |
| The alarm | the same | its alarm days (16) | each person the user chose, and the user |

Its free plan (Hobbyist) has 20 checks and email alerts. Docs: https://healthchecks.io/docs/ and https://healthchecks.io/docs/api/. The code is open source (BSD-3-Clause), and a user may run their own: https://healthchecks.io/docs/self_hosted/.

## 1. Account and project (the user)

1. Ask them to sign up at https://healthchecks.io/ with their own email address. Sign-in is a link by email. Asking for a second link voids the first, so ask them to request one and wait for it; links last an hour.
2. A project for the switch: from the projects list, a new project called `Dead man's switch`. A project of its own keeps its key and its integrations apart.
3. In that project's **Settings** page, under API keys, create the read-write key (not the read-only one). There is no API for keys.

## 2. The key (the user, then you)

They copy the key, then type into the prompt:

    ! "$IRIE_BIN" dms key --paste

It keeps the key where only they can read it, empties the clipboard, and prints the key's last four characters and whether it works. For their own server they add `--api https://their-server/api/v3/`.

Never ask them to paste the key into the conversation. If they do, tell them to delete that key in Healthchecks, make a new one, and use `--paste`.

## 3. The checks (you)

Agree the alarm's name and description with the user first (`helpers.md`, steps 3 and 4). Then:

    "$IRIE_BIN" dms checks --alarm-name "…" --alarm-desc-file "<folder>/alarm.txt"

with `--every`, `--remind` and `--alarm` if their rhythm isn't 14, 2 and 16 days. It makes both checks with no integrations. Run it again after any change: it updates the checks by name.

## 4. The integrations (the user)

You can't do this part, and must not try: the API can't make integrations, and an integration is a standing rule that emails other people. Give them these steps, one person at a time:

1. In the project, open **Integrations**, and add an **Email** integration for each person. Name each one after the person, such as `Bob`, so `dms status` can show who is told.
2. If the list has no integration with their own email address, add one and name it `Me`.
3. Each new address gets an email from Healthchecks, "Verify email address on Healthchecks.io", and receives nothing until its owner clicks the link in it. The user's own sign-in address needs none. Suggest they tell each person first, so the email isn't a surprise and doesn't go to spam.
4. Healthchecks ticks a new integration for every check that already exists. So, on the **reminder** check's page, they untick everyone but `Me`; otherwise the people hear at 16 days instead of 30. On the **alarm**, everyone stays ticked, `Me` too.

Then run `"$IRIE_BIN" dms status`. Go on only when the reminder emails only `Me`, and the alarm emails everyone. If an address is still unconfirmed, the Integrations page says so.

## 5. Arm it (the user)

A new check waits for its first ping, and never goes down before it. Once the tap is set up (`phone.md`), they check in once; `dms status` then shows both checks up, with the next check-in's date.

## 6. Test it

- **Their own email:** each integration in the Integrations page has a test button. Ask them to send a test to `Me` and confirm it arrived. Testing the people's integrations sends them an email; suggest it only if they have warned them.
- **The check-in:** after the first tap, `dms status`.
- Never test by letting the alarm go off, and never use the ping URL's `/fail`: that emails everyone at once.

## What the people's email says

Its subject is `DOWN | ` and the alarm's name. Its body holds the alarm's description word for word, and the time and IP address of the last check-in. When check-ins start again, they get an `UP` email too. So the description says what to do, and also: "If Alice checks in again, you'll get an email saying UP: it was a false alarm."

## Turning it off

`"$IRIE_BIN" dms pause` pauses both checks: nobody is emailed until the next check-in starts them again. Deleting the checks, or the project, in Healthchecks ends it.
