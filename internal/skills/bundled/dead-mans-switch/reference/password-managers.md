# Emergency access, by password manager

Ask which password manager the user has, and its plan if they know it. Read that vendor's page below before giving the steps, since vendors move their menus, and say so if what the user sees differs. Every step here is the user's: no agent may add, invite or confirm anyone, and no CLI or public API can do it anyway.

For each person, the user picks how much they get. A partner might get full access after a short wait; others might only see the vault, after a longer one. Write each choice in `switch.md`.

## Bitwarden

https://bitwarden.com/help/emergency-access/ and https://bitwarden.com/help/add-and-manage-trusted-emergency-contacts/

- **Plan:** emergency access needs Premium, or membership of a paid organization such as Families. It isn't there on the free plan, and not in an organization with the "Automatic confirmation" policy.
- **The people:** each needs a Bitwarden account, free is enough, on the same server (bitwarden.com, or the EU one).
- **Levels:** *View* reads every item of the user's own vault. *Takeover* sets a new master password, which replaces theirs and removes their two-step login.
- **Wait:** 1, 2, 7, 14, 30 or 90 days. During the wait the user gets an email and can approve or reject the request.

Steps, for each person:
1. In the web vault (https://vault.bitwarden.com), **Settings › Emergency access › Add emergency contact**: their email, the level and the wait.
2. The person gets an invitation by email, valid for five days, and accepts it by signing in or making an account.
3. The user goes back to Emergency access and **confirms** them, after checking the fingerprint phrase the page shows against the one the person sees.

**The letter:** a secure note called READ ME FIRST: **New › Note**, paste the letter, save.

**Families:** to share a vault with up to six people, the Admin Console's **Members › Invite member**. Invitations expire after five days.

## 1Password

https://support.1password.com/emergency-kit/ and https://support.1password.com/recovery/

1Password has no emergency access. Instead:
- The **Emergency Kit**: a PDF with the sign-in address, the email address and the Secret Key, and a space to write the account password. Print it, write the password in, and keep it where the letter says, such as with the will.
- On a **Families** plan, a family organizer can recover a member's account; the member then sets a new password through their email.

**The letter:** a Secure Note called READ ME FIRST.

## Proton Pass

https://proton.me/support/emergency-access

Emergency access to the whole Proton Account, Pass included: up to five contacts, on any paid Proton plan, and both people need a Proton Mail address. Wait times: 1, 2, 3, 7, 14 or 30 days. Steps: https://mail.proton.me › **Settings › Recovery › Emergency access**.

## KeePassXC and KeePass

https://keepass.info/help/kb/faq.html#emsheet

No delegated access. The people need the database file, the key file if there is one, and the master password. KeePass 2 prints an Emergency Sheet (**File › Print › Print Emergency Sheet**) with the password left blank to fill in by hand. Write in the letter where the file and the sheet are.

## Apple Passwords (iCloud Keychain)

A Legacy Contact can't reach it: Apple says it can't access "Information stored in the account holder's iCloud Keychain" (https://support.apple.com/en-us/103128). For what the people need, a **shared password group** with them (iOS 17, macOS 14; https://support.apple.com/guide/iphone/share-passwords-iphe6b2b7043/ios).

## Google Password Manager

Not confirmed that Google's Inactive Account Manager hands over saved passwords. The user can share the passwords that matter with members of their family group (https://support.google.com/chrome/answer/95606).

## LastPass

https://www.lastpass.com/features/emergency-access — emergency access, at once or after a wait the user sets; Premium or Families.

## Dashlane

https://support.dashlane.com/hc/en-us/articles/4407179219986 — no emergency contacts any more. Dashlane suggests an encrypted export, with its password kept where the letter says.

## No password manager

Suggest one, without pushing. Bitwarden's free plan keeps the letter as a secure note; emergency access needs Premium.
