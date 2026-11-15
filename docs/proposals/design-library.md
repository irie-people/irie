# Proposal: a design library for Irie's clients

Date: 2026-11-15. Status: step 1 is built (below); the rest is proposed. This file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote it and built step 1. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Irie looks and behaves as one product in all six of its clients: the Mac app, the iPhone app, the phone's web app, the Windows app, the terminal sidebar and the site. What Irie learns about design (about colour, about how a part should be used, about which parts are built from which) is written down once, checked where it can be, and reaches every client from one place.

The request, 2026-11-15: start a front-end design and component library from what has been learned about design and colours, and about how components are used and built from one another.

## Why now

On 2026-11-15 the colours of the states were changed for colour-blind viewers. The change had to be made by hand in eight files in four languages (Go, Swift, JavaScript, CSS and HTML), and the colours of the agents live in four copies of one set of steps. Only the Go and Swift copies were checked against each other; a web or iPhone copy could drift unseen. The rules behind the colours (contrast floors, calm chroma, red for one thing, the button as the mark) were spread over DESIGN.md, docs/MAC.md, code comments and the history of requests. Each client drew its own card, count and group head.

## Step 1: the library and its checks (built)

The library is `design/`, read from [design/README.md](../../design/README.md):

- **The values.** The four palettes as files of their own in `design/palettes/`, in the shape of a theme file of one's own, and `design/tokens.json` for the rest every client must agree on: the palettes' order and default, the contrast floors, the terminal's colours and the agents' colours' numbers.
- **Go reads them.** Package `design` embeds the files; the terminal sidebar, the status line and the config take their palettes, the agents' numbers and the list of theme names from it, so Go keeps no copy.
- **The copies are checked and written.** Each copy that cannot read the library (the Swift apps, the web apps' CSS and JavaScript, the setup page, the site's themes page) marks its blocks of the library's colours with a comment. `tools/design.sh` says where a copy differs and `tools/design.sh sync` writes the library's values in; `TestCopiesMatchTheLibrary` fails `tools/check.sh` when a copy was changed by hand.
- **The rules.** [design/principles.md](../../design/principles.md): each rule once, with its reason, its source and its check, and how rules that met were settled.
- **The parts.** [design/components.md](../../design/components.md): every part the clients share, its states, when to use it, what it is built from, and where each client draws it; and the patterns parts follow together.
- **The gallery.** [design/gallery.html](../../design/gallery.html) draws every role, state and part in each palette and mode, from the library's own files, with the library's web parts (`design/web/components.css`).

## Steps after

In order; each is its own change.

1. **One set of web parts.** The phone app and the Windows app load `design/web/tokens.css` and `components.css`, served by their Go servers from package `design`, and drop their own card, count, group head, buttons and banners. Each change is checked with headless screenshots of both apps before and after.
2. **The site's themes page from the library.** `site/themes.html`'s pictures and tables are made from the palettes, so a colour change needs no new drawing; until then the check finds every stale colour in it.
3. **Swift from the library.** The Mac and iPhone apps' palettes, terminal colours and agents' numbers are written into one Swift file each by `tools/design.sh`, rather than kept by hand between markers.
4. **Sizes.** A spacing scale, a type ramp and corner radii per density in tokens.json, taken from what the clients draw now, and then used by them.
5. **The gaps the catalogue found.** The iPhone app honours Increase Contrast and Reduce Motion; the parts marked "To converge" in components.md are made one.
6. **More checks.** Each "unchecked" rule in principles.md gets a check where one can be written: a lint for colours named outside the library in the web clients' CSS, and snapshot tests of the gallery.

## Decisions

- **Inside Irie, as `design/`.** The clients it serves are Irie's, and the colour copies it checks are in this repository. A part of it could become a package of its own later, if another project wants it.
- **Theme files, not a token standard, for the palettes.** The built-in palettes are written as a person's own theme file is, so making one's own starts from copying one. The [Design Tokens format](https://www.designtokens.org/tr/2025.10/) can be an export when a tool needs it.
- **Markers in the copies, not templates.** A comment around each block keeps every copy's own layout and lets one command check and write all of them, with no change to how any client is built.
- **Public sources only.** Every rule's source is public or a request recorded here; rules known only from private notes are left out.
