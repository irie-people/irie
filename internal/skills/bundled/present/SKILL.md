---
name: present
description: How to show your work to a person who reads this session in Irie, Irie's Mac app or sidebar, rather than in a terminal. Use before you ask the person to decide something, and when you report results, a comparison, changes or progress.
license: Apache-2.0
---

# Present your work in Irie

This session runs in Irie. The person may follow it in Irie's Mac app or its sidebar, which draw your conversation themselves: your replies as Markdown, each tool call as one line in plain words, and a question you ask with a tool as a card of its own. Write so it reads well there and in a terminal alike.

## Ask a decision with AskUserQuestion

When you need the person to choose, ask with the `AskUserQuestion` tool, not in a sentence at the end of a reply. Irie draws the question and its choices as a card, which the person sees at a glance, even when they are looking at another session.

- One to four questions, each with a short `header` and two to four options.
- Put the option you recommend first, and end its label with "(Recommended)".
- Give each option a one-line `description` of what choosing it means.
- When the options are concrete alternatives, such as code, a layout or a config, give each a `preview` that shows it.
- Ask only what is the person's to decide. When there is a sensible default, or you can find the answer yourself, decide, go on, and say what you chose.

## Report results as data

- **Lead with the outcome** in a sentence or two: what changed, what you found, what is left. Details come after.
- **A comparison or a set of results:** a Markdown table with few columns and short cells. Irie draws tables natively.
- **Steps or findings:** a short list.
- **Changed lines:** a fenced `diff` block, with `+` and `-` lines.
- **How parts connect, or what happens in what order:** where a picture helps, a small fenced `mermaid` diagram, a dozen nodes at most, with the words beside it. Irie's Mac app draws it; a terminal shows its text.
- **Commands, paths and code:** a fenced block with its language.
- **Numbers:** their units, and what they compare with ("40 ms, from 120 ms").
- **A file you made for the person to open,** such as a page or a report: its full path on a line of its own.

## Keep it drawable

- Write Markdown only: no HTML, no terminal escape codes, no images from web addresses. Irie draws none of them, and a page that fetches an address can leak what it shows.
- Keep tables and code blocks narrow enough to read beside a sidebar: a hundred characters at most.
- Don't pad. A short reply that ends where the work ends reads best in a card.
