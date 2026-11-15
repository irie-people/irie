# Plugin panes, plugins of your own and bundles

Date: 2026-11-14. Status: built, with what is left at the end. An agent (Claude Code) wrote it from `live` at `56e3e74`. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

The owner asked on 2026-11-14 for plugins "as like pane views - like variorum and a browser pane": panes for two local web apps of theirs, and "a small business bundle of pluggins" holding those two and the Variorum tab, "and then we will add to this list". The two apps are named only in the owner's own manifests, never here.

**In short.** Three small pieces on top of DESIGN §4.17:

1. **Panes.** A plugin's part can bring a pane: a tab in the Mac app's right sidebar ([right-dock.md](right-dock.md)) that shows while the part is on.
2. **Plugins of your own.** A manifest, a TOML file in the `plugins` folder of Irie's config folder, makes a plugin out of parts that need no code of Irie's. For now that part is a web pane: a local web app beside the sessions.
3. **Bundles.** A manifest can also name plugins, one line each, that go on and off together under one switch.

So adding a pane, or a plugin to a bundle, needs no change to Irie's code.

## 1. Panes

A part brings a pane by saying so: `ipc.Pane` on the part, with an `id`, a `title`, an SF Symbols `icon` and a `kind`. The core says which panes there are and how each part stands. The Mac app draws a tab for each pane whose part is on, ready or needing something, after Irie's own tabs. A part that is off, or whose plugin is off, has no tab.

- **`web`** panes show a page: `address`, the one `origin` the pane may load, the `start` words that start the app behind it, and `store`, the name of the website data it keeps.
- **Any other kind** names a view of the app's own. The Writing plugin's Variorum tab ([writers.md](writers.md), being built in another session) can be one, kind `variorum`, rather than a fixed tab.

The tab keeps its page while you go to another tab and back, with its scroll and a half-filled form. Its web view goes when its plugin goes off. Like every tab, a pane never opens or comes forward by itself.

## 2. Manifests

`~/.config/irie/plugins/NAME.toml`, where NAME is lower-case words joined by hyphens, is a plugin named NAME:

```toml
title = "Invoices"
summary = "The invoicing app on this Mac, beside your sessions."

[panes.invoices]
title = "Invoices"                    # the tab's name; the plugin's title when left out
address = "http://127.0.0.1:8080/"    # the page, on this Mac unless remote = true
icon = "doc.text"                     # an SF Symbols name; a globe when left out
start = "cd ~/code/invoices && make serve"   # shown when nothing answers; never run
```

A manifest is data: Irie reads it and runs nothing from it. `[schedules.<id>]` tables are kept for the schedules another session is adding: until a build reads them, each shows as a part that is held, so a manifest written today keeps working. A manifest that doesn't read shows as its plugin with a part that says what is wrong.

## 3. Bundles

A manifest with a `plugins` list is a bundle:

```toml
title = "Small business"
summary = "The apps that run the business, with one switch for all of them."
plugins = [
  "invoices",
  "writing.tab",
]
```

Each line is a plugin's name or a part of one, `PLUGIN.PART`. The bundle's switch turns each of them on or off: a part goes on with its plugin. The bundle reads on when all of them are on, off when none is, and *some* in between.

## 4. Security

- **One origin.** A web pane loads only from its address's origin: a content rule list blocks every other load, and the navigation delegate refuses every other page. The origin is on this Mac (127.0.0.1, localhost or ::1) unless the manifest also says `remote = true`, and then it must be https.
- **Links elsewhere** open in the browser, and only from a link the person clicked, as in the Markets window: never a page's script, even one that runs as they type. A script can open no window.
- **Its own data.** Each plugin's panes keep their logins in a website data store of that plugin's own (`WKWebsiteDataStore(forIdentifier:)`, macOS 14), so a login lasts across launches and no other pane, Dock control or window sees it.
- **Signing in from Bitwarden** (2026-11-15). With the Bitwarden plugin on, a pane's sign-in page fills itself from the login the person's vault has for the pane's host and port, once they unlock Bitwarden in the app (DESIGN §4.17). The fill runs in a script world of its own, in the main frame only, and only on the pane's own origin.
- **No bridge.** The page has no way to talk to Irie.
- **No WebRTC, camera or microphone.** WebRTC is outside what a content rule list governs, so a page could reach any host with it: every frame starts without its constructors, a blank frame the page makes too, and the page can't put them back. A pane denies the camera and the microphone, which the app tells macOS it never uses.
- **No dialog flood.** A page that asks more than three times in ten seconds gets no more sheets until it loads again; the pane answers for it, as Cancel. A page whose web process stops twice in a minute waits for Reload.
- **When the app isn't running**, the pane says so and shows what starts it, and tries again every few seconds while it shows. The core knocks once on a pane's address when that is on this Mac, a TCP connect that goes no further than the Mac, so Settings and the Plugins menu say so too. It never knocks on a remote address.

## 5. Where it shows

- **Settings › Plugins:** each bundle first, with its one switch and a row for each of its plugins, then Irie's plugins, then yours, each with its manifest's path.
- **The Plugins menu:** each bundle's switch, then each plugin's, as now. A pane's part has Show in Right Sidebar.
- **View › Right Sidebar** lists the panes after Irie's own tabs.
- **`irie plugins`** and `:plugins` print the bundles first. `irie plugins on|off NAME [PART]` takes a bundle's name too.
- **The `plugins` op** answers with `bundles`, and takes `bundle` with `on`.

## 6. Decisions

1. **A bundle sets switches and holds none.** Each plugin in it keeps its own switch, and the bundle's state comes from theirs, so it never says on while one of them is off. A bundle that held a switch of its own, gating its plugins as a plugin gates its parts, was the other choice. It would have needed every one of Irie's own switches (`VPNOn` and the rest, in `config.Config`) to know about bundles, and gives no clear answer for a plugin in two bundles.
2. **A part in a bundle needs a switch of its own** to go off without its plugin. A part with none goes on with its plugin and stays on while the plugin is, and the bundle says so.
3. **A plugin of your own is on until you turn it off,** as the Dock runs the controls in your own `dock.json` without asking: the folder is yours. A plugin with a pane off this computer is the exception: it is off until you turn it on, so a manifest written into the folder, by an agent say, can't bring a remote page by itself. Its switch is `plugins.manifests.NAME` in config.toml, never in the manifest, so a manifest can be shared, or later come from a repository, without its state.
4. **Web panes come from manifests; Irie's own panes are its own views.** No page of Irie's own is a web page.
5. **One data store per plugin, not per pane,** so a plugin's panes share their logins.
6. **Markets stays a window.** Its map wants room (the window is at least 720 by 480 points; the sidebar is 260 to 520 wide), reads a folder rather than a server, and landed today (PR #19). With panes in place it could also show as one, of kind `markets`, if the owner wants it beside the sessions.
7. **No shortcut keys for panes yet.** ⌥⌘1 to ⌥⌘6 go by place among Irie's own tabs; a pane's place moves as plugins come and go.

## 7. Not yet

- A pane's own window, or a zoom over the terminal, for an app that wants more room than the sidebar has.
- Forget a pane's logins from Settings (`WKWebsiteDataStore.remove(forIdentifier:)`).
- Plugins from a repository of their own, pinned to a commit (DESIGN §4.17, Later).
- The terminal sidebar and the phone list plugins and bundles but draw no panes. The phone reaches the web panes that `remote.panes` names instead, each at an address of its own, in Safari or on its Home Screen ([PHONE.md](../PHONE.md) §2.2), not inside Irie's app.
