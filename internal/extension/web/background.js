// Irie's browser extension (docs/proposals/browser-extension.md). Its
// button opens the side panel and lets the panel read the page it was
// pressed on: the browser grants that page, and only that page, for the
// press (activeTab). Nothing is read before a press, and nothing leaves
// this computer: the panel talks only to Irie, over native messaging.
"use strict";

const ext = globalThis.browser ?? globalThis.chrome;

// Chrome opens the panel itself on a press when told to, but then no
// press reaches us; we open it ourselves, so we also know which tab.
if (ext.sidePanel && ext.sidePanel.setPanelBehavior) {
  ext.sidePanel.setPanelBehavior({ openPanelOnActionClick: false }).catch(() => {});
}

ext.action.onClicked.addListener((tab) => {
  // Open first: a browser opens a panel only in answer to the press itself.
  if (ext.sidePanel && ext.sidePanel.open) {
    ext.sidePanel.open({ windowId: tab.windowId }).catch(() => {});
  } else if (ext.sidebarAction && ext.sidebarAction.open) {
    ext.sidebarAction.open().catch(() => {});
  }
  const pressed = { tabId: tab.id, windowId: tab.windowId, at: Date.now() };
  // The panel may be opening still: it finds the press here when it loads,
  // and hears it at once when it is already open.
  ext.storage.session.set({ pressed }).catch(() => {});
  ext.runtime.sendMessage({ type: "pressed", pressed }).catch(() => {});
});
