# Proposal: a launch kit for every release

Date: 2026-11-15. Status: proposed, with step 1 built (`docs/demo/kit.sh`). This file stands in for the issue that AGENTS.md asks for. An agent (Claude Code) wrote this proposal and step 1, from the public sources listed at the end.

## Why

Irie puts out a release for every change that lands, often more than one a day. Few people see them unless someone films one and posts it.

Higgsfield, a company that makes AI video, reached a $300 million yearly run rate in eleven months, according to a public teardown of its growth. Part of that came from a habit worth copying:

- every feature launch came with a campaign, with a brief of what to show;
- the videos were made with Higgsfield's own tools, so every post showed the product;
- creators posted them, and were paid for each post Higgsfield accepted.

This proposal takes that habit and leaves out what went wrong with it.

## What Higgsfield did, and what Irie takes

| Higgsfield | Irie |
|---|---|
| A campaign for each feature launch, with a brief of what to show | A launch kit for each release: a brief of what to show, and drafts of what to say (step 1) |
| Every video made with its own tools | Every clip filmed by Irie itself, in the demo sandbox, with `record.sh` and `cut.sh`. It's a real capture, so a clip shows what a user gets |
| An automated review of each submission | `kit.sh check`, run before anything is posted |
| Pay for each accepted post, bonuses for views, and every creator's rate kept secret | Not taken. Steak sizes merged work, not reach, and its MODEL.md rules out open bounties and rewards per activity. A reel that is merged is a demo, which Steak sizes like code. Every rule is public |
| Templates: pick a style and click, rather than write a prompt | Not yet. Presets for common jobs, ranked by how often their work passes the dog's checks (DESIGN §4.8), would need a proposal of their own |
| A new model added within a day of its release | Already here: profiles, and routing by cost per success (DESIGN §6.4) |
| Four to seven releases a week | Already here: a release for every change that lands |

## What went wrong, and the rules that keep it out

The same teardown records what this growth cost Higgsfield:

- demos made from templates that looked better than what users could make;
- stock footage passed off as its own;
- offensive clips, which its pay for engagement had rewarded;
- creators' disputes over payment;
- its X account, suspended for inauthentic behaviour.

So every post made from a kit keeps these rules, and the brief repeats them:

1. Film only the sandbox, never a real desktop, so nothing names a person or a machine.
2. Show what a user gets: real captures of Irie, never a mock-up, a polished fake or stock footage.
3. Say only what the picture shows. Make no claims of speed, money or results that the clip doesn't prove.
4. Nothing goes out until the maintainer has read it, and the maintainer posts it by hand.
5. Never buy views, likes or follows, and never pay for reach.
6. Anyone who earns Steak and posts about Irie says so.

## What a kit holds

`docs/demo/kit.sh VERSION` writes `dist/kits/VERSION/` from CHANGELOG.md, the commits and the reels:

- **`brief.md`** lists each feature of the release with the first paragraph of its commit's message, and the reel that films it, or says it has none yet. A reel names the commit it films on a `# feature: HASH` line. Then come the clips any post can use, with their captions, and the rules above.
- **`post.md`** has drafts for Threads and Instagram, made from the release's first three features, with a link to the release notes. Once the file is there, `kit.sh` doesn't write over it, so the maintainer's edits stay.
- **The phone cuts** that `record.sh` and `cut.sh` have made, copied in so they're ready to attach.

`kit.sh check` fails a kit in any of these cases:

- a draft runs past its platform's limits: 500 characters, five links and one topic tag on Threads; 2,200 characters and five hashtags on Instagram;
- a draft links anywhere but the project's own pages;
- the brief or a draft names the user, their machine or their home, or has an email address outside example.com;
- a clip isn't 1080×1920, runs under 3 seconds, or names anyone in its metadata.

The check makes no judgement of taste. The maintainer still reads every post before it goes out.

## Steps

1. **Built:** `docs/demo/kit.sh` and `kit.sh check` (docs/demo/README.md, "Launch kits").
2. A reel for each feature that shows on screen, written from the brief and marked with its `# feature:` line. Many features need reel steps the recorder lacks, such as opening a menu, dragging a card or a group, or opening a plugin's window.
3. A kit made whenever a release lands, with word to the maintainer that it's ready.
4. Reels from outside contributors, merged as demos and sized in Steak like code. Saying so on the contribute page is a public promise, so it waits for the maintainer's word.

## Open decisions

1. Where a kit goes once it's made: only `dist/kits/`, or also somewhere the maintainer reads on a phone.
2. Which platforms: Threads and Instagram for now; X, Bluesky or Mastodon once the project has accounts there.
3. Whether the drafts carry hashtags, and which ones.

## Sources

- Rishikesh Ranjan, "$300M in 11 Months: The Rise and Reckoning of Higgsfield AI", Product Growth, 5 March 2026, updated 11 August 2026: https://www.productgrowth.blog/p/higgsfield-growth-teardown
- Higgsfield Earn Supplemental Terms and Conditions, which cover the automated review, rejections and payouts: https://creators.higgsfield.ai/terms
- Higgsfield's help center, on joining Earn: https://higgsfield.ai/creator-hub/help-center/business/how-do-i-join-a-partnership-or-creator-program
- Meta, Threads posts, for the 500 characters, five links and one topic tag: https://developers.facebook.com/documentation/threads/posts
- Meta, the Instagram media reference, for the 2,200-character caption and a reel's 3 seconds: https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media
- Instagram's limit of five hashtags, from 18 December 2025, as reported by Social Media Today: https://tech.yahoo.com/social-media/articles/instagram-implementing-limits-hashtag-141143530.html
