# Irie's design library

How Irie looks and behaves in every client, kept in one place: the palettes and numbers every client draws with, the rules Irie has learned, and the parts it draws them as. Irie has six clients: the Mac app, the iPhone app, the phone's web app, the Windows app, the terminal sidebar, and the site's pictures. They are written in Swift, HTML and CSS, JavaScript and Go. The library is what keeps them one product.

| File | What it holds |
|---|---|
| [principles.md](principles.md) | The rules: colour, order, disclosure, controls, motion, words, accessibility, each with its reason, source and check |
| [components.md](components.md) | The parts: what each is for, its states, when to use it, and where each client draws it |
| [palettes/](palettes) | Irie's four palettes, one file each, in the form of a theme file of your own |
| [tokens.json](tokens.json) | Everything else every client must agree on: the palettes' order, the contrast floors, the terminal's colours, the agents' colours' numbers |
| [gallery.html](gallery.html) | Every palette, role, state and part, drawn in each palette and mode; open it in a browser |
| design.go, copies.go, site.go | The library in Go, and the checks that keep the copies in step |
| web/tokens.css, web/tokens.js | The palettes for a web page, made from the files above |

## 1. The layers

1. **Principles** say what must hold, and why.
2. **Tokens** are the values: a palette's 21 roles in light and dark, and the numbers in tokens.json.
3. **Components** are the parts, built only from roles, never from colours.
4. **Patterns** are how parts work together (components §4).

The core owns every rule and the clients only draw (components §1). The library is the same idea for the look: a value lives here once, and each client takes it from here.

## 2. Roles

A palette has 21 colours, each for one job. Parts name roles, never colours, so every palette, both modes and a person's own theme work everywhere.

| Group | Roles | Drawn as |
|---|---|---|
| Surfaces | `sidebar`, `ground` (the window), `card`, `panel`, `field`, `hover`, `selection`, `needs_tint` | grounds that text sits on |
| Hairline | `edge` | a line between surfaces |
| Text | `ink`, `muted`, `accent_ink`, `needs`, `done`, `paused`, `frozen` | words and glyphs: 4.5:1 on every surface |
| Accent | `accent` | a mark: 3:1 on every surface |
| Start button | `accent_fill`, with `on_accent` on it | the one prominent button |
| Icon | `sun`, `waves` | Irie's sun and sea |

`design.Roles()` lists them with their names in Make a Theme and how each is drawn.

## 3. Changing a colour or a number

1. Change it in [palettes/](palettes) or [tokens.json](tokens.json), never in a client.
2. Run `tools/design.sh sync`. It writes the new value into every copy (§4) and makes web/tokens.css and web/tokens.js again.
3. Run `tools/check.sh ./design ./internal/ui`. The tests check every text colour at 4.5:1 and the accent at 3:1 on every surface, the states apart in colour-blind vision, and every copy against the library.
4. If the site's themes page's pictures have the old colour, `tools/design.sh` says where: draw those again by hand (§5).

A new palette is a new file in palettes/ and its ID in tokens.json's `palettes`, then a copy in every client that lists the palettes (the Mac app's Theme.swift).

## 4. The copies

Go reads the library itself: the terminal sidebar, the status line and the config take their palettes and numbers from package `design`. The other clients cannot read a file from here where they run, so they keep copies, and the checks keep the copies honest:

| Copy | Holds | Why it keeps a copy |
|---|---|---|
| mac/Sources/Irie/Theme.swift | all four palettes, the terminal's colours, the agents' numbers | the app builds its palettes in, to draw its first frame without a file |
| ios/Irie.swiftpm/Sources/Look.swift | Evergreen, the terminal's colours, the agents' numbers | it draws in Evergreen until a core sends the Mac app's palette |
| internal/remote/web/js/look.js | Evergreen, the agents' numbers | the phone web app draws in Evergreen until the header brings a palette |
| internal/remote/web/app.css | Evergreen, the terminal's colours | the first paint, before look.js runs |
| internal/winapp/web/app.css | Evergreen, the terminal's colours | the same, on Windows |
| internal/remote/web/setup.html | some of Evergreen | the setup page stands alone |
| site/themes.html | every palette, in pictures and tables | the site is static pages |

In a copy, each block of the library's colours sits between two comments, `design: evergreen light` and `design: end`, in the file's own comment syntax. Inside a block every name with a colour must be one of the palette's roles, in any spelling (`--needs-tint`, `needsTint`, `"needs_tint"`), or a name the first comment gives a role (`design: evergreen light page=sidebar`). A colour of the file's own goes outside the blocks. The agents' numbers are found by the patterns in copies.go.

`tools/design.sh` checks every copy and says where one differs; `tools/design.sh sync` writes the library's values into them. `TestCopiesMatchTheLibrary` runs the same check with the other tests, so a copy changed by hand fails `tools/check.sh`. A new copy is a new entry in `Copies` (copies.go).

## 5. Where the library stands

Built: the palettes and tokens in one place, Go reading them, every copy checked and written by one command, the principles, the components, and the gallery.

Not yet, in the order [docs/proposals/design-library.md](../docs/proposals/design-library.md) plans them:

- The web clients still draw their own parts: the phone app and the Windows app each have a card, a count and a group head in their own CSS. They should share one set of parts.
- The site's themes page is drawn by hand; it should be made from the library, so a colour change needs no new drawing.
- The Swift apps keep their palettes as code; they could be written from the library at build time instead.
- There is no spacing scale or type ramp: each client sets its own sizes.
- The iPhone app does not honour Increase Contrast or Reduce Motion yet (principles §5.3, §7.1).
- Several parts differ between clients where they should not: they are marked **To converge** in components.md.

## 6. Adding what we learn

The library is meant to grow with every lesson about design and colour, about how parts are used, and about how they are built from smaller ones:

- **A rule** goes in principles.md with its reason, source and check (principles §9).
- **A part,** or a new state of one, goes in components.md, and in the gallery (components §5).
- **A colour or a number** goes in palettes/ or tokens.json (§3).
- **A check** goes in this package or with the code it checks. A rule with a check stays true.

Record each public source that shapes the library in [PROVENANCE.md](../PROVENANCE.md), as for code (`tools/provenance.sh add`).
