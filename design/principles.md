# Irie's design principles

What Irie has learned about how it should look and behave, each rule once. Every rule says why it holds, where it came from, and what checks it. "Asked for" means a request to Irie's maintainers on that date; the public sources are in [PROVENANCE.md](../PROVENANCE.md). A rule with no check yet says so: making one is the next thing to do for it.

Read one part with `tools/doc.sh design/principles.md 2` (or words from a heading). To add a rule, see §9.

## 1. Colour

### 1.1 Lightness carries the meaning

Text, glyphs and edges differ from their ground in lightness, not only in hue. Two colours of one lightness lose their edge, for everyone and most of all for colour-blind viewers. Design a view in grey first, then add hue.

- **Why:** the eye finds edges and shapes by lightness; hue alone gives them no form.
- **From:** M. Livingstone and D. Hubel, ["Segregation of form, color, movement, and depth"](https://www.science.org/doi/10.1126/science.3283936), *Science* 240, 1988.
- **Check:** the contrast tests below.

### 1.2 Text reads at 4.5:1 and marks at 3:1, on every surface

Every colour drawn as text (`ink`, `muted`, `accent_ink`, `needs`, `done`, `paused`, `frozen`) keeps 4.5:1, and the accent, drawn as a mark, 3:1, on all eight surfaces (`sidebar`, `ground`, `card`, `panel`, `field`, `hover`, `selection`, `needs_tint`), in every palette and both modes. Measure the colour that ships, after any transparency, on the ground it really sits on. A ratio just over its floor is nudged further: rounding to 8 bits can take away the margin.

- **Why:** what WCAG asks for text and for marks that carry meaning.
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/), 1.4.3 and 1.4.11.
- **Check:** `TestPalettesMeetWCAG` and `TestContrastMatchesWCAG` (internal/ui); `design.Contrast` for new code.

### 1.3 Work in OKLCH, mix in OKLab

Pick colours by OKLCH lightness, chroma and hue, mix and fade them in OKLab, and never reason in HSL, whose lightness is not what the eye sees: HSL's yellow and blue at "50%" are far apart in lightness.

- **Why:** in OKLCH, colours of equal lightness and chroma look equally light and equally vivid, so a palette can be spaced evenly.
- **From:** B. Ottosson, [A perceptual color space for image processing](https://bottosson.github.io/posts/oklab/); [CSS Color 4](https://drafts.csswg.org/css-color-4/).
- **Check:** the agent colours are worked out this way in every client (design/tokens.json, `agents`).

### 1.4 Calm: low chroma, never neon

Colour goes up only where meaning does. In OKLCH chroma, Irie's built-in palettes keep their surfaces between 0.003 and 0.041, the selection and the needs tint under 0.06, text under 0.06, the agents' marks at 0.085, accents at 0.04 to 0.14, and the states at 0.05 to 0.16, the most vivid being `needs`. Tell things apart by hue and lightness, never by turning saturation up.

- **Why:** a calm view, never neon. An earlier fan of colours at full brightness came out as electric violets and magentas on dark grounds.
- **From:** asked for on 2026-11-01; the ranges were measured from design/palettes on 2026-11-15.
- **Check:** `TestAgentColorsAreCalm` (internal/ui) for the agents; the rest is unchecked.

### 1.5 Red means one thing: it needs you

The `needs` colour marks only what waits on the person: a card, a count, a group's "· 1 needs you". Nothing else is red: unread mail is news, not a need, and a count of nothing is faint, not red.

- **Why:** if red means one thing, a glance down the list finds every session that needs you.
- **From:** asked for on 2026-10-26.
- **Check:** `TestRedMarksOnlyTheRowsThatNeedYou`, `TestZeroCountsAreNotRed` (internal/ui).

### 1.6 Agents have colours of their own, apart from their neighbours

A working session's card takes one of six colours: hues 50° apart round the whole wheel, leaving 30° clear either side of the palette's `needs` red, all at one low chroma. A card's colour sits at least 100° round the wheel from the card above it, as near opposite as it can, and a session keeps its colour wherever it moves and through a restart.

- **Why:** cards side by side stand apart at a glance, and none glares.
- **From:** asked for on 2026-11-01; Ottosson's OKLCH, as in 1.3.
- **Check:** `TestHuesApart`, `TestAgentColorsReadOnTheTerminal` (internal/ui); `TestCardsSideBySideTakeColoursNearOpposite`, `TestColoursHoldThroughARestart` (internal/core); the agents' numbers in every client are the library's (`TestCopiesMatchTheLibrary`, design).

### 1.7 Colour is never the only cue

Every state has a glyph of its own and words of its own, so a colour-blind viewer, or anyone on a grey screen, reads it as well as anyone. The states' colours also stay apart in colour-blind vision: every pair of `needs`, `paused`, `done` and `frozen` at least deltaE-OK 0.06 apart in normal, protan, deutan and tritan vision; each state at least 0.06 from `ink` and `muted` in normal vision and 0.03 for dichromats, since working rows take ink and dim rows muted; and `done` at least 0.06 from the accent. So the states differ in lightness as well as hue, each in its own hue family: in light mode the ones moved apart go deeper and in dark mode paler, and `needs` is deeper than `paused`.

- **Why:** about one man in twelve of Northern European descent has a red-green deficiency, and before this rule `needs` and `paused` met at deltaE-OK 0.014 to 0.017 for deutans. A sweep of every pair then found `done` and `frozen` alike to tritans (0.034 to 0.042) and Evergreen's `done` within 0.008 of its muted text for deutans. Two pairs stop short of 0.06 for dichromats as far as their hues allow: Evergreen's light `done` and `muted` (0.033), and Terracotta's dark `needs` and `muted` (0.055).
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 1.4.1; [MedlinePlus, colour vision deficiency](https://medlineplus.gov/genetics/condition/color-vision-deficiency/); [libDaltonLens](https://github.com/DaltonLens/libDaltonLens) (Brettel, Viénot and Mollon 1997); the floors are Irie's own, set on 2026-11-15.
- **Check:** `TestEveryStateHasItsGlyph`, `TestRedIsNeverTheOnlyCue`, `TestStatesStayApartForColourBlindViewers` (internal/ui).

### 1.8 A change to one colour checks every pair it meets

Changing a colour can move a clash rather than end it: a crimson `needs` tried on 2026-11-15 came within deltaE-OK 0.03 of one palette's accent text. So a change is checked against every pair it can meet, in all four visions, before it ships. Change it in the library, never in a copy (README §3).

- **Why:** each colour sits beside many others.
- **From:** the colour work of 2026-11-15.
- **Check:** the tests of 1.2 and 1.7 run over every palette; `TestCopiesMatchTheLibrary` (design) catches a copy changed by hand.

### 1.9 Dark mode is a set of its own

A palette's dark set is drawn for the dark, not inverted from the light one: dark grounds that are not black (OKLCH lightness 0.18 to 0.21 in Irie's), surfaces a little lighter the nearer they are, and accents lighter than in light mode and no more vivid.

- **Why:** inverted colours glare on dark grounds, and pure black makes light text seem to bleed.
- **From:** [Material Design 2, dark theme](https://m2.material.io/design/color/dark-theme.html).
- **Check:** the contrast tests of 1.2 cover both sets; the rest is unchecked.

### 1.10 A wash keeps to one hue

A gradient goes from a colour to a quieter step of the same hue, as a working card's wash and the selected card's fade toward its terminal do. Irie never draws a gradient across hues, gradient text, a glow, a coloured shadow or a neon edge.

- **Why:** one hue reads as one thing that is busy; many hues read as decoration.
- **From:** asked for (the selected card's fade); [unslop](https://github.com/heydamir/unslop) by Damir Sokolovsky, CC BY 4.0, for what not to draw.
- **Check:** unchecked.

## 2. Order and layout

### 2.1 Only the person moves a card

The list keeps one order, the person's: a card moves when they drag it, use the move keys, pin, park or close it. A state change or a boost changes a card's mark and colours, never its place, and nothing groups or sorts by state.

- **Why:** a list that reorders itself sends the eye hunting, and a click can land on the wrong card.
- **From:** asked for on 2026-10-27, and for boosts on 2026-11-14 (DESIGN §4.1, §4.3).
- **Check:** `TestTheListViewKeepsOneOrder`, `TestABoostLeavesTheRowInItsPlace`, `TestBoostingAParkedSessionLeavesItParked` (internal/ui); `TestParkAndUnparkPutASessionLastAndABoostMovesNone` (internal/core).

### 2.2 A card lands where the eye already is

Parking puts a card last in Parked, folded, at once; unparking puts it last above Parked; pinning puts it last in Pinned. Nothing lingers where it was.

- **Why:** the card goes somewhere the person can see, not back to a place far up a long list.
- **From:** asked for on 2026-11-09 (DESIGN §4.1).
- **Check:** `TestParkedComesLast` (internal/ui).

### 2.3 Nothing moves under the pointer

Hover swaps what sits in a fixed-width slot, as the age gives way to a card's buttons; a rename edits in place at the same size; a name never re-wraps as an age ticks; a drawer resizes the terminal once, not on every frame. Only the person scrolls, but for one thing: a task they just started scrolls into view.

- **Why:** a target that moves as you reach for it gets missed.
- **From:** asked for on 2026-10-27 (DESIGN §4.1).
- **Check:** `TestWheelMovesTheOffsetNotTheSelection` (internal/ui); the Mac app's side is unchecked.

### 2.4 Compact rows, details on demand

A card is two lines: a mark, the name and the age, then the detail in plain words, with up to three helpers under it. Where the terminal sidebar is short or narrow, each card becomes one line.

- **Why:** the list should show many sessions at once; "more compact", asked for on 2026-10-26.
- **From:** asked for on 2026-10-26 (DESIGN §4.1).
- **Check:** `TestLongNamesAreCut` (internal/ui).

### 2.5 Flat, but the selection stands up

Cards lie flat in the list, with no edge or shadow at rest. The selected card stands up on one gentle shadow and is tied by a line to its terminal. Panels sit lighter than the list; depth otherwise comes from the surfaces' lightness steps.

- **Why:** one raised thing says where you are; many raised things say nothing.
- **From:** the Mac app's sidebar (mac/Sources/Irie/SidebarView.swift).
- **Check:** unchecked.

### 2.6 Space and shape

Sizes step on a 4-point grid; there is less space inside a group than between groups; an inner corner's radius is the outer radius less the padding between them. Corners follow the platform: cards are 14 on the Mac, 16 on the phones, 8 on Windows.

- **Why:** rhythm makes a view easy to read; one-off sizes make it look assembled.
- **From:** [unslop](https://github.com/heydamir/unslop) (CC BY 4.0); the clients' own sizes.
- **Check:** unchecked. The clients have no shared spacing scale or type ramp yet (README §5).

## 3. Disclosure

### 3.1 Plain words first, the exact thing one step away

A card says what a session is doing in plain words ("Running the tests"); the exact command is in its details panel, which opens at one line, and the terminal is one more step. Each tool call is a line in plain words that opens to its exact command.

- **Why:** most of the time nobody needs the exact command, and when they do it is right there.
- **From:** asked for on 2026-10-26 (DESIGN §4.1, §4.7).
- **Check:** `TestRowsSayPlainWordsAndKeepTheCall`, `TestPhrase` (internal/core).

### 3.2 Controls show where they act

A card's buttons show only on the card under the pointer. In a narrow place a control is an icon, with its words on hover. Settings that only some people want stay out of the way, never out of reach.

- **Why:** a list full of buttons is noise; a hidden thing must still be easy to find.
- **From:** asked for on 2026-10-26 ("more progressive disclosure").
- **Check:** unchecked.

## 4. Controls and marks

### 4.1 The button is the mark

When a card's hover button toggles a state (boost, park, pin, pause), the state shows as that button kept on the card, filled, in its own place. Never add a second icon for the same state.

- **Why:** two icons for one state is clutter; the button already says it.
- **From:** asked for on 2026-11-14.
- **Check:** unchecked (the Mac app; the terminal sidebar has no buttons and keeps its `↑`).

### 4.2 Act at once, then offer undo

A step happens when asked, and Undo takes it back, even across a restart. Irie asks first only when a step cannot be undone, and then gives the recommended answer first.

- **Why:** asking before every step trains people to click through; undo loses nothing.
- **From:** DESIGN §4.5.
- **Check:** `TestHistoryUndoesAndRedoesInOrder` (internal/core).

### 4.3 Every drag has a click or a key

Whatever a drag does, a pointer's single click or a key can do too: move keys for the order, a menu to move a card to a group.

- **Why:** not everyone can drag.
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 2.5.7.
- **Check:** unchecked.

### 4.4 Menus name what they act on

Every command is in the menu bar; a menu item names its object ("Close “api-fix”", "Undo Park …"); a card's menu repeats the Session menu's words in its order; Show and Hide items say which they will do.

- **Why:** a person reads the item, not the context, before choosing it.
- **From:** asked for on 2026-11-14; [Apple's Human Interface Guidelines, menus](https://developer.apple.com/design/human-interface-guidelines/menus).
- **Check:** `TestTitleQuotesEachName` (core), `TestABatchNamesItsSessions` (internal/core).

### 4.5 Focus always shows

The selected card keeps its fill, and its edge goes bolder while the list has the keyboard, so the keyboard's place is always plain.

- **Why:** someone using the keyboard needs to see where it is.
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 2.4.7.
- **Check:** unchecked.

### 4.6 A drop shows where it lands, and only where it acts

While a drag holds something, the place it would land lights, and only a place where the drop would change something: a group held over its own space's chip lights nothing. A target that is plain until a drag, as the + after the space chips, says what a drop there does while one is held ("New Space").

- **Why:** a drag is a guess until the target answers; a target that lights for nothing teaches people to distrust the rest.
- **From:** asked for on 2026-11-15 ("drag groups to new workspaces"; the drags should be "super great and easy").
- **Check:** unchecked (the Mac app's `SpaceDrop` and `DropModel`).

### 4.7 A key with fn belongs to the key router

A key with fn in it, such as Fn+Tab for the next space, is caught by the Mac app's key router (KeyRouter), and its menu item names it in its help, never as its shortcut.

- **Why:** AppKit takes fn out of an app's own menu keys ("only supported for system-provided menu items"), so the item would show and take the key without fn: tried with fn⌃Tab, the item read and acted as ⌃Tab, Next Session.
- **From:** asked for on 2026-11-15; AppKit's message on macOS 14.1, and SwiftUI's [`EventModifiers.function`](https://developer.apple.com/documentation/swiftui/eventmodifiers/function), "reserved for system applications".
- **Check:** `TestNoMenuKeyTakesFn` (design).

## 5. Motion

### 5.1 Motion only explains

Motion shows where a thing came from or went: a drawer slides in from its edge, a fold turns its chevron. Animate colour, opacity and position only; no bounce, no growing on hover, no loop on something that is not live. A list's marks hold still: an endless animation in a list makes the whole window lay out again.

- **Why:** motion that says nothing tires the eye, and in Irie's lists it also costs every frame.
- **From:** [unslop](https://github.com/heydamir/unslop) (CC BY 4.0); the Mac app (mac/Sources/Irie/States.swift).
- **Check:** unchecked.

### 5.2 Drawers, not covers

Side panels are drawers that slide in (0.28 s on the Mac) beside the terminal, never sheets over it. The terminal resizes once when a drawer opens, closes or its edge is let go.

- **Why:** each resize redraws every agent's terminal, and a cover hides the work.
- **From:** asked for on 2026-11-02 and 2026-11-08.
- **Check:** unchecked.

### 5.3 Reduce Motion is honoured

With Reduce Motion on, every slide, turn and fold happens in one step.

- **Why:** some people feel ill at motion on screen.
- **From:** Apple's Reduce Motion setting; [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 2.3.3.
- **Check:** unchecked. The iPhone app does not honour it yet (README §5).

## 6. Words

### 6.1 Plain words, for people who never open a terminal

Every line a person reads says what is happening in everyday words, with no command's name where a word will do.

- **Why:** Irie is for people who do not know what a terminal is, as well as for those who do.
- **From:** asked for on 2026-10-26.
- **Check:** `TestPhrase` (internal/core), `TestPlainTextIsWhatAPersonReads` (internal/core).

### 6.2 Labels are short and calm

Sentence case; a button is a verb and its object; no exclamation marks, greetings or hype words (supercharge, seamless, magic); no claim that cannot be checked; numbers in one format, with figures that line up.

- **Why:** calm words fit a calm view, and hype reads as filler.
- **From:** [unslop](https://github.com/heydamir/unslop) (CC BY 4.0).
- **Check:** unchecked.

### 6.3 The product's name comes from one place

Every string that names the product takes it from `brand.Name`, so a copy under another name is one file's change.

- **Why:** DESIGN §2.10.
- **From:** DESIGN §2.10.
- **Check:** `TestNoNameOutsideBrand` (internal/brand).

## 7. Accessibility

### 7.1 Increase Contrast makes text stronger

Under Increase Contrast the Mac app moves dark text 35% toward black and light text 45% toward white, moves hairlines 40% toward the ink, draws cards without the agents' washes, and holds agents' marks at 4.5:1 and their text at 7:1.

- **Why:** some people need more than the minimum.
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 1.4.6; Apple's Increase Contrast setting.
- **Check:** unchecked. The iPhone app does not honour it yet (README §5).

### 7.2 A screen reader reads a card as one thing

VoiceOver reads each card as one element (its name, state, what it needs, when it started) and offers every hover and menu action.

- **Why:** a card is one thing to a sighted person too.
- **From:** docs/MAC.md.
- **Check:** unchecked.

### 7.3 Notices wait for the person

A notice never covers the task bar; an error stays at least 8 seconds and as long as the pointer is on it; every notice is kept in the Notices tab.

- **Why:** a message that vanishes before it is read was never sent.
- **From:** [WCAG 2.2](https://www.w3.org/TR/WCAG22/) 2.2.1.
- **Check:** unchecked.

## 8. What Irie never draws

- Gradients across hues, gradient text, glows, coloured shadows, neon edges (1.10).
- Emoji as icons; "New" and "Pro" badges; points, streaks or badges that nag.
- A list that sorts itself by state, or a card that lingers after it is parked (2.1, 2.2).
- A cover over the terminal where a drawer would do (5.2).
- A second icon for a state its button already shows (4.1).
- Colour fans at full brightness and saturation (1.4).
- One-off sizes, uppercase tracked labels on body text, pulsing on what is not live.

From [unslop](https://github.com/heydamir/unslop) (CC BY 4.0) and Irie's own history. A quick test for a new view: put another product's name on it. If it still fits as well, it has no point of view yet.

## 9. Adding a rule

When a session learns something that should hold from now on, about colour, layout, motion, words or how parts behave:

1. Add it to the part of this page it belongs to, as a rule in one sentence, then **Why**, **From** and **Check**. Keep the numbers exact.
2. Give the source a row in [PROVENANCE.md](../PROVENANCE.md) (`tools/provenance.sh add`), as for code. A rule from a request says when it was asked for, never who asked.
3. Write the check if you can: a Go test, or a check in `design/` (README §4). Say "unchecked" if not.
4. If it changes a part, change its entry in [components.md](components.md) too.

### 9.1 Where rules met and how they were settled

- **Gradients.** "No gradients" met the selected card's fade, which was asked for: a gradient within one hue is allowed, across hues never (1.10).
- **Shadows.** "Depth from surfaces" met the selected card's shadow: one shadow, for the selection (2.5).
- **Harmony or distinction.** Neighbouring hues look calmer, but cards side by side must stand apart: distinction wins for neighbours (1.6).
- **Agents' colours for colour-blind viewers.** The six share one lightness, so some pairs come close in colour-blind vision. They mark which session is which, never a state, and each card has its name: no meaning rides on them alone (1.7).
- **Scrolling.** "Only the person scrolls" has one exception: a task they just started scrolls into view (2.3).
- **Contrast in dark mode.** Some advice asks dark-mode text for 7:1; Irie's floor is 4.5:1 in both modes, with 7:1 for agents' text under Increase Contrast (1.2, 7.1).
