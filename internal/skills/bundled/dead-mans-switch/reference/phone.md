# The tap, and the calendar

Ask the user how they want to check in. Offer what fits their phone, and let them take one or more:

| Their phone | The check-in | Helper |
|---|---|---|
| iPhone | **A shortcut, "I'm OK", on the Home Screen**: one tap pings both checks | `dms shortcut` |
| Any phone | **A contact, "I'm OK"**: open its link, send the email it starts, and both checks are told | `dms contact` |
| Any computer | `irie dms ping` | — |

Then make the calendar file whatever they chose: a reminder to check in every period, and a yearly review.

Every file goes in their folder, readable only by them. The shortcut holds the checks' ping URLs and the contact their email addresses, so once the phone has them, offer to delete the files.

## The iPhone shortcut

1. `"$IRIE_BIN" dms shortcut --out "<folder>/I'm OK.shortcut"` writes it and signs it on this Mac with Apple's own `shortcuts sign`, for "people who know me": only phones that have the user in their contacts can run it, which their own iPhone does. If signing fails, say what it printed; signing needs the Mac to be signed in to iCloud. Never use `--mode anyone`.
2. On the iPhone: **Settings › Shortcuts › Private Sharing** on (https://support.apple.com/guide/shortcuts/adjust-privacy-settings-apd961a4fc65/ios).
3. Send the file to the phone with AirDrop, or in Messages to themselves. iCloud Drive can take a long while on a busy Mac, so prefer AirDrop.
4. Open it on the phone, check that it shows two Get Contents of URL actions, and tap **Add Shortcut**.
5. In Shortcuts, touch and hold "I'm OK" › Share › **Add to Home Screen**.
6. Tap it once. Then `"$IRIE_BIN" dms status`: both checks should be up.

If the phone won't import it, the contact below works on an iPhone too.

## The contact, for any phone

1. `"$IRIE_BIN" dms contact --out "<folder>/I'm OK.vcf"` writes a contact card called "I'm OK", with both checks' email addresses and a link that starts one email to both.
2. They send the file to the phone (AirDrop, Nearby Share, or a cable) and open it to add the contact.
3. To check in: open the contact, tap its link, and send the email. Its words don't matter, but keep them short: the alarm attaches the last check-in, so the people would read it.
4. After the first one, `"$IRIE_BIN" dms status`. Healthchecks' own code checks in every address one email goes to, but whether the hosted service does is not confirmed: if only one check is up, they write to both addresses next time, or send one email to each.

## The calendar

1. `"$IRIE_BIN" dms calendar --out "<folder>/Dead man's switch.ics"` makes two events: "Check in: tap I'm OK", every period from the next Sunday at 10:00, and "Review your dead man's switch", once a year, about two months from now. `--first YYYY-MM-DD`, `--at HH:MM` and `--review YYYY-MM-DD` change them. The file holds no ping URL.
2. They open it on the phone or the Mac, and add both events to the calendar they look at.
