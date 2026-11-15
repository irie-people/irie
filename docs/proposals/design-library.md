# Proposal: a design library for Irie's clients

Date: 2026-11-15. Status: step 1 is built (below); the rest is proposed. This file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote it and built step 1. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Irie looks and behaves as one product in all six of its clients: the Mac app, the iPhone app, the phone's web app, the Windows app, the terminal sidebar and the site. What Irie learns about design (about colour, about how a part should be used, about which parts are built from which) is written down once, checked where it can be, and reaches every client from one place.

The request, 2026-11-15: start a front-end design and component library from what has been learned about design and colours, and about how components are used and built from one another; then, the same day, put the library under the irie-people organisation on GitHub, and name it: Roots, for roots reggae.

## Why now

On 2026-11-15 the colours of the states were changed for colour-blind viewers. The change had to be made by hand in eight files in four languages (Go, Swift, JavaScript, CSS and HTML), and the colours of the agents live in four copies of one set of steps. Only the Go and Swift copies were checked against each other; a web or iPhone copy could drift unseen. The rules behind the colours (contrast floors, calm chroma, red for one thing, the button as the mark) were spread over DESIGN.md, docs/MAC.md, code comments and the history of requests. Each client drew its own card, count and group head.

## Step 1: the library and its checks (built)

The library is Roots, its own repository, [irie-people/roots](https://github.com/irie-people/roots), a Go module (`github.com/irie-people/roots`) with no dependencies; it was irie-people/design for its first hours. It began as `design/` in Irie (9245b1f) and moved out the same day.

- **The values.** The four palettes as files of their own in `palettes/`, in the shape of a theme file of one's own, and `tokens.json` for the rest every client must agree on: the palettes' order and default, the contrast floors, the terminal's colours and the agents' colours' numbers. The library's own test holds every palette to its contrast floors.
- **Go reads them.** Irie's terminal sidebar, status line and config take their palettes, the agents' numbers and the list of theme names from the module, so Irie's Go keeps no copy.
- **The copies are checked and written.** Each of Irie's copies that cannot read the library (the Swift apps, the web apps' CSS and JavaScript, the setup page) marks its blocks of the library's colours with a comment. The library gives `Check` and `Sync` for a project's list of copies; Irie's list, and its check of the site's themes page, are in `internal/designcheck`. `tools/design.sh` says where a copy differs and `tools/design.sh sync` writes the library's values in; `TestCopiesMatchTheLibrary` fails `tools/check.sh` when a copy was changed by hand.
- **The rules.** The library's `principles.md`: each rule once, with its reason, its source and its check, and how rules that met were settled.
- **The parts.** Its `components.md`: every part the clients share, its states, when to use it, what it is built from, and where each client draws it; and the patterns parts follow together.
- **The gallery.** Its `gallery.html` draws every role, state and part in each palette and mode, with its web parts (`web/components.css`).

## Steps after

In order; each is its own change.

1. **One set of web parts.** The phone app and the Windows app load the library's `web/tokens.css` and `web/components.css`, served by their Go servers from the module (which would then embed them), and drop their own card, count, group head, buttons and banners. Each change is checked with headless screenshots of both apps before and after.
2. **The site's themes page from the library.** `site/themes.html`'s pictures and tables are made from the palettes, so a colour change needs no new drawing; until then the check finds every stale colour in it.
3. **Swift from the library.** The Mac and iPhone apps' palettes, terminal colours and agents' numbers are written into one Swift file each by `tools/design.sh`, rather than kept by hand between markers.
4. **Sizes.** A spacing scale, a type ramp and corner radii per density in tokens.json, taken from what the clients draw now, and then used by them.
5. **The gaps the catalogue found.** The iPhone app honours Increase Contrast and Reduce Motion; the parts marked "To converge" in components.md are made one.
6. **More checks.** Each "unchecked" rule in principles.md gets a check where one can be written: a lint for colours named outside the library in the web clients' CSS, and snapshot tests of the gallery.

## Decisions

- **Its own repository, under irie-people, named Roots.** Asked for on 2026-11-15, and the name chosen the same day from names that refer to roots reggae, leaving out Rastafari's religious words, artists' and labels' names, and names already a company's, so the library is a home of its own for every Irie People project that draws, and lessons learned outside Irie's repository can go straight into it. Irie depends on it as a Go module; a change to the palettes is a change there, then a `go get` and `tools/design.sh sync` here, in one commit with the copies it changes.
- **Theme files, not a token standard, for the palettes.** The built-in palettes are written as a person's own theme file is, so making one's own starts from copying one. The [Design Tokens format](https://www.designtokens.org/tr/2025.10/) can be an export when a tool needs it.
- **Markers in the copies, not templates.** A comment around each block keeps every copy's own layout and lets one command check and write all of them, with no change to how any client is built.
- **Public sources only.** Every rule's source is public or a request recorded here; rules known only from private notes are left out.
