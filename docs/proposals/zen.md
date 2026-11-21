# Zen, a calm place

Date: 2026-11-21. Status: step 1 built. An agent (Claude Code) wrote it with the owner.

The owner asked to pivot zen mode (Fn+Z, built 15 Nov) into a view for mental health:

1. **Very minimal**, a view "that teaches you".
2. **Shaped live.** You "interactively update your user experience live and have it persisted as your zen mode".
3. **Versions** of it, tracked the way Mycylic tracks graphs, and shown in a graph explorer.
4. **Slowly.** "Slowly usher them into this new world."
5. **Wellbeing first.** "The conscious effort and highest priority while in zen mode is one's user's mental health."

## 1. What stays

Zen is still Fn+Z: the session on show alone, full screen, its card as a bubble over the terminal, its own panes, and nothing else. The three layouts stay (Centred, Edge to Edge, Keep Watch), and so do zen's own text size and Fill the Screen. Zen still moves and closes nothing, so leaving it gives the window back as it was (MAC.md §1).

What changes is what zen is for. Until now it was a way to focus. From now on it is a calm place, where the person comes before the work.

## 2. Principles

1. **You before the work.** Nothing in zen hurries you, keeps you longer than you meant to stay, or makes you feel behind. There are no streaks, points, scores or counts of what you haven't done (Roots §8).
2. **What needs you comes softly.** Another session that needs you shows as one quiet line, with its red mark (red still means one thing, Roots §1.5) and plain words: "Another session would like you". Keep Watch's list keeps its marks, and its words go calm. Calm technology moves news to the edge of attention and lets the person bring it to the centre (Weiser and Brown). Interruptions cost stress as well as time (Mark et al.), and news that's gathered rather than pushed leaves people calmer (Fitz et al.).
3. **One thing at a time.** One session, one offer, one lesson.
4. **Rest is part of the work.** Zen can offer a short rest after a long stretch at the computer, with a breath to follow. Short breaks raise vigour and lower fatigue (Albulescu et al.), and slow breathing, under ten breaths a minute, goes with calm (Zaccaro et al.). Late at night it says once that the agents can keep going without you.
5. **You shape it, live, and can always go back.** Every change shows at once. What you keep becomes your zen, as a new version, and any earlier zen can come back.
6. **Nothing leaves your computer.** Your zen, its versions and what the path has shown stay on this Mac. Zen records no moods, feelings or health, and Irie collects no data on anyone (local-chain.md, "What it never is").
7. **Not a therapist.** Zen is no treatment and makes no claims. Once, on its path, it says where people can find help: findahelpline.com lists free, confidential helplines by country.

## 3. Your zen

Your zen is a small document, kept by the core and drawn by every client (the core owns the rules, CORE.md):

| Key | Values | First | What it does |
|---|---|---|---|
| `layout` | `centred`, `edge`, `watch` | `centred` | Which of zen's layouts |
| `columns` | 80, 100, 120 | 100 | Centred's reading column, in columns of text |
| `text_size` | 0, or 9 to 28 | 0 | Zen's own text size in points; 0 keeps the window's |
| `full_screen` | true, false | true | Zen fills the screen |
| `gentle` | true, false | true | Zen asks the agent to be gentle (§8) |
| `breaks` | 0, or 15 to 180 | 0 | Minutes at the computer before zen offers a rest; 0 is off until you say yes |
| `night` | true, false | true | Late at night, zen says once that you can rest |

Rest starts off: zen asks before it ever interrupts you. Its path offers breaks early (§6).

You change it in four ways, and each shows at once:

- Settings › Appearance › Zen, and View › Zen Layout.
- ⌘+ and ⌘- in zen (text size).
- The path's offers: Try It shows the change live, then Keep or Go Back.
- Your agent, at your word. Zen tells it how (§8), so "make my text bigger" works: it runs `irie ctl zen set text_size=16`.

## 4. Versions

Each change you keep makes a version of your zen. It's built like a git commit, from public standards only:

- **The document:** `{"created", "format": "irie-zen/1", "parent", "why", "zen"}`. `created` is RFC 3339 in UTC, to the second. `parent` names the version before; the first has none. `why` says what changed, in plain words ("A rest every 50 minutes"). `zen` holds the settings of §3.
- **Canonical bytes:** the JSON Canonicalization Scheme (RFC 8785): keys sorted, no whitespace, minimal string escapes. Only integers are allowed, within ±2^53, so every JCS implementation writes the same bytes.
- **The name:** SHA-512 of those bytes (FIPS 180-4), written as an OCI-style digest, `sha512:` and 128 hex digits. Any tool with a JCS library and SHA-512 can check a version without Irie.
- **The state:** SHA-512 of the canonical `zen` object alone. Two versions with the same settings share a state, which is what the graph explorer draws (§5).
- **On disk:** `$XDG_CONFIG_HOME/irie/zen/`, holding `objects/<hex>.sealed` (each checked against its name when read), `HEAD` and `key`. It sits with your themes and layouts, since it's yours.
- **The vault.** Every version is sealed at rest with AES-256-GCM (local-chain.md, "The vault, at rest"):
  - Each record has a fresh random 96-bit nonce and is bound to its name, so a file put in another's place doesn't open.
  - The data key in `key` is sealed the same way under a key the system keeps: the login Keychain's item "Irie vault: zen" on macOS (made by `security`, so it reads without asking), else `$XDG_STATE_HOME/irie/keys/zen.key`, readable by you alone and apart from the vault, so a copy of your settings carries no key.
  - Nothing in the folder reads without that key. A version's name is the hash of its plain bytes, so names don't change in the vault.
  - The vault hides what each version says, not how many there are or when their files were written.
  - Versions kept before the vault move in when it first opens, and their plain files go.
  - The state file (`$XDG_STATE_HOME/irie/zen.json`) keeps your zen as it is now, with how far along the path you are, so zen looks as you left it while the keychain is locked. A change then keeps nothing, rather than a version in plain, and the vault is tried again at the next change, a minute apart at most.
  - A vault its key doesn't open, as when the keychain lost the key, is set aside whole as `zen-unreadable-<time>`, where that key could still open it, and your zen as it was starts a new one.
- **History is never rewritten.** Going back to an earlier zen makes a new version with that zen's settings ("Back to a2f9c4…"), so you can come back from going back.
- **The package:** `internal/chain` holds the canonical bytes, the names and the object store. Zen is its first user; the sidebar's local chain (local-chain.md) is the next.

`irie ctl zen` shows your zen; `irie ctl zen log` lists the versions; `set` and `restore` change it. `--json` gives each version's hash, parent and state for other tools. `irie ctl zen export` prints every version, oldest first, one a line: its exact canonical bytes, so `shasum -a 512` of a line, without its line break, is the version's name.

## 5. The graph explorer

View › Your Zen… (and the path's own step) opens your zen's history as a graph. Each zen you've had is a node, and each change is an arc from the zen before to the one after.

- **Arcs, not a hairball.** The nodes sit in a row in the order you first had them, and the changes arc above it. This is an arc diagram, as Wattenberg drew them for sequences that come back to earlier states. Going back to an earlier zen is one long arc home, and a zen you keep returning to gathers arcs.
- **Size is time.** A node's size is how long you kept that zen. Your zen now is in the accent colour, and the one you're looking at is ringed.
- **Details one step away.** Clicking a node shows its settings, when you had it and for how long, with **Go Back to This Zen**. The keys move along the row too (Roots §4.3).

It's drawn by the app itself, with no web view and no network, in calm colours, and holds still while you look (Roots §5.1).

## 6. The path

Zen teaches one small thing at a time, only when the moment is calm, so a newcomer is led in slowly. The steps live in `internal/zen/path.toml`, in order.

**When a step may come:**

- Zen has been on for 2 minutes.
- It has been 15 minutes since the last offer was answered.
- No more than 3 steps a day, so the path takes a few days.
- Nothing needs you in the session on show.
- Its agent is working, or you've rested your hands for 20 seconds, so you're waiting rather than typing.
- Not late at night (23:00 to 05:00).

**How it goes:**

- A step is one sentence in a bubble at the foot of the terminal, and it stays until you answer, never timed out (Roots §7.3).
- A lesson has Got It. A change has Try It, which shows the change live, then Keep or Go Back.
- Not Now puts a step at the back of the path; a second Not Now drops it.
- Nothing counts how far along the path you are.

**The steps:**

1. **Welcome:** what zen is, and that it tells you softly when another session would like you.
2. **Eyes:** while the agent works, look at something about six metres away for twenty seconds. That's the American Academy of Ophthalmology's 20-20-20 habit, offered as a habit, not a cure: its own doctors say it has hardly been studied.
3. **Breaks:** a change. "Would you like a gentle reminder to rest every 50 minutes?"
4. **Ask your agent:** zen asks it to be gentle, and you can ask it to change your zen.
5. **Text:** a change. "Larger text is easier on the eyes. Try it?" adds one point.
6. **History:** every change you keep is a version, with Show Me to open the graph explorer. This is the step into the new world of versions and graphs.
7. **Night:** late at night zen will say once that you can rest.
8. **Help:** you matter more than the work; findahelpline.com.
9. **Proof:** each version is named by its SHA-512 hash and linked to the one before, like git's commits, so its history can be checked (`irie ctl zen log`), and anchored one day if you want (§9).

## 7. Rest and night

- **At the computer.** The core reads how long the keyboard and mouse have been still, as Gabriel does (DESIGN §4.11). Five minutes away counts as a rest, so the count starts again when you come back.
- **A rest.** Once you've been at the computer for `breaks` minutes, or half that late at night (never under 15), the rest bubble offers a breath at the next pause: while the agent works, or once your hands have rested 20 seconds, never mid-sentence. A question waiting on you doesn't hold it back: the bubble takes no keys, and a breath before a decision helps it. A dot rises as you breathe in for 4 seconds and falls as you breathe out for 6, about six breaths a minute. It moves only in position and opacity, and holds still under Reduce Motion (Roots §5.1, §5.3). Back to It starts the count again; In 10 Minutes waits.
- **Night.** Between 23:00 and 05:00, once a night, zen says the agents can carry on without you and that rest is part of the work.
- **Nothing is recorded.** Whether you rested isn't recorded.

## 8. The agent in zen

When zen starts with `gentle` on, the core queues a note for the agent of the session on show, and for each session you show while in zen. It goes through the notes channel, at the agent's next tool call or prompt (DESIGN §4.3):

> [Irie] The user is in zen, a calm, minimal view where their wellbeing comes first. Until a note says zen ended: keep replies short, plain and kind; ask one question at a time; no urgency, alarm or pressure; say plainly when something is done or stuck. If they seem tired or stressed, it is fine to suggest a pause: the work can wait. At their word only, you can change their zen: `"$IRIE_BIN" ctl zen` shows it and `"$IRIE_BIN" ctl zen set KEY=VALUE` changes it.

When zen ends, each session told gets "[Irie] The user left zen; carry on as usual." A note that hasn't gone out yet is taken back instead. The note says only that zen is on, never how the person seems: it goes to the model's provider with the conversation.

## 9. Mycylic

Mycylic, the owner's graph product, keeps graph versions as SHA-512-named canonical JSON linked to their parents. That's the same shape as zen's versions, so Mycylic can track them. The direction matters:

- **Irie defines its format here, in public,** from public standards (RFC 8785, FIPS 180-4, OCI digests), and never copies Mycylic's code. Mycylic is the owner's own and unpublished, and Irie builds from public sources only (AGENTS.md).
- **Mycylic reads Irie's format.** An importer (planned there) reads `irie ctl zen export`, checks each line's SHA-512 against the parent the next names, turns the history into a Mycylic graph, and shows it in Mycylic's explorer. It never reads the vault: the export is the way out, and only at your command.
- **Anchoring stays opt-in and hash-only,** as in local-chain.md: `irie ctl zen log --json` gives the head's hash, and `mycylic anchor` (planned there) can put that hash, and nothing else, on a chain. Irie never signs a transaction.

## 10. Steps

1. **Built (this change):**
   - `internal/chain` and `internal/zen`, the `zen` op and `irie ctl zen`.
   - The header's `zen`, the notes to the agent, rest, night and the path.
   - In the Mac app: zen read from the core and changed through it (the app's old zen settings become the first version), the offer and rest bubbles, the soft line, calm words in Keep Watch, Settings, and View › Your Zen… with the arc diagram.
2. **Built: the vault.** Zen's versions sealed at rest (§4): `internal/chain`'s vault and `internal/keyring`, the first part of local-chain.md's step 2, and `irie ctl zen export` for Mycylic.
3. **Mycylic:** its importer and `mycylic anchor`, in Mycylic's own repo.
4. **Other clients, each with a zen of its own** (the terminal sidebar's and the phone's web app's built):
   - **Who holds zen.** The core keeps zen on while any client holds it: the Mac app (`client` empty, or `app`), the terminal sidebar (`sidebar`) or the phone (`phone`). A client's "off" ends only its own hold, so the Mac app, which says whether it is in zen at each new selection, never ends the sidebar's.
   - **The header's `zen.clients`** lists the holders, so a client that restarts, as each does on an install, comes back into zen by itself.
   - **When a hold ends.** The app's hold ends 2 minutes after its last window goes, as before. Another client renews its hold by saying `on` again at least every minute, and the hold lapses 2 minutes after the last. Holds are kept in `zen.json`, so a core that restarts keeps them until they lapse.
   - **Every session zen shows,** in any client, is told once to be gentle.
   - **The terminal sidebar,** with `~`: the card of the session on show alone, a soft line when another session would like you, and what zen offers, with `y` and `n` for its two answers. A rest comes with a dot that rises for 4 seconds and falls for 6. There are no counts, no status strip and no tagline. `j` and `k` go on to the next session or the one before, which the slot shows. `~` leaves.
   - **The phone's web app,** first among a session's actions: the conversation alone, Leave Zen in place of the actions, the soft line (a tap shows that session, in zen still), and what zen offers over the message box, with the rest's dot under CSS that keeps still under Reduce Motion. It says `on` each minute while the session shows; leaving the session ends its zen. `tools/phonepreview`'s fake core offers a rest, for pictures.
   - **The native phone app** comes next.

## Open questions

- **More of the path,** and in what order: each step should teach something a newcomer needs next.
- **Zen per space** (zen.md first asked for one zen); a space could pick a version of your zen.
- **Notices in zen:** whether the app's own notices (the results of what you did) should wait until zen ends.

## Sources

- Weiser and Brown, "Designing Calm Technology" (1995): https://calmtech.com/papers/designing-calm-technology.html
- Mark, Gudith and Klocke, "The Cost of Interrupted Work: More Speed and Stress", CHI 2008: https://doi.org/10.1145/1357054.1357072
- Fitz et al., "Batching smartphone notifications can improve well-being", Computers in Human Behavior 101 (2019): https://doi.org/10.1016/j.chb.2019.07.016
- Albulescu et al., "Give me a break!": micro-breaks for well-being and performance, PLOS ONE (2022): https://doi.org/10.1371/journal.pone.0272460
- Zaccaro et al., "How Breath-Control Can Change Your Life", Frontiers in Human Neuroscience (2018): https://doi.org/10.3389/fnhum.2018.00353
- American Academy of Ophthalmology, eye strain and the 20-20-20 rule: https://www.aao.org/eye-health/diseases/what-is-eye-strain
- Find a Helpline: https://findahelpline.com/
- Wattenberg, "Arc Diagrams: Visualizing Structure in Strings", IEEE InfoVis 2002: https://doi.org/10.1109/INFVIS.2002.1173155
- RFC 8785, JSON Canonicalization Scheme: https://www.rfc-editor.org/rfc/rfc8785
- FIPS 180-4, Secure Hash Standard: https://csrc.nist.gov/pubs/fips/180-4/upd1/final
- OCI image spec, digests: https://github.com/opencontainers/image-spec/blob/main/descriptor.md#digests
- Git's objects: https://git-scm.com/book/en/v2/Git-Internals-Git-Objects
