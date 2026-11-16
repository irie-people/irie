# Changelog

## [1.9.0](https://github.com/irie-people/irie/compare/v1.8.0...v1.9.0) (2026-11-16)


### Features

* **mac:** set the sidebar's sections apart with space and hairlines, and join what belongs together ([8722606](https://github.com/irie-people/irie/commit/872260666d7abdc7ad8905de888f8e37eedaccdc))
* **mac:** zen keeps the session's own card and panes ([14f08bd](https://github.com/irie-people/irie/commit/14f08bd726030fdc9ec1fa8c3d1a86b8aa9ac516))
* **mac:** zen mode on Fn+Z, the session alone in a layout of its own ([a323437](https://github.com/irie-people/irie/commit/a323437d9ec5f3c72a9532475de0364905cba19b))
* **mac:** zen's card becomes a header bubble over the terminal ([61daaa3](https://github.com/irie-people/irie/commit/61daaa37bb805088b657477d4d57e703557dd098))
* **terms:** Terms in short, a plugin of its own with a Terms tab in Irie ([9a11932](https://github.com/irie-people/irie/commit/9a11932bfca14a38f522584f60a51a52154e6247))
* **undo:** ask before ⌘Z or ⇧⌘Z takes a step, naming it ([4b7db63](https://github.com/irie-people/irie/commit/4b7db63b8d2a5bc6d102ea6b645688df80756305))

## [1.8.0](https://github.com/irie-people/irie/compare/v1.7.0...v1.8.0) (2026-11-16)


### Features

* **core:** keep each agent's window at its own session's terminal size ([e96cbfc](https://github.com/irie-people/irie/commit/e96cbfc3281db4026e944b9aa10c1507bcab5728))
* **core:** say where a terminal's link opens, by ui.terminal_links ([0e112c6](https://github.com/irie-people/irie/commit/0e112c67877150133f7a190865a0687639803d7c))
* **extension:** Irie in your browser, with Terms in short as its first tool ([372019f](https://github.com/irie-people/irie/commit/372019fbeffa2a8ebf42f38df42dad2a1788742d))
* **extension:** irie terms serve, the panel as a page for Irie's own panes ([5c5959e](https://github.com/irie-people/irie/commit/5c5959e3f6be69039615e61ea1e18db7c6eb8c98))
* **mac:** open a terminal's links in the session's panes ([6003581](https://github.com/irie-people/irie/commit/600358169826e898cecae5629273981458d7b8e6))
* **mac:** show or hide each session's panes as it last had them ([1d92d43](https://github.com/irie-people/irie/commit/1d92d431e0129658e7bc6f9e85387c483242bdd7))
* **plugins:** Bitwarden fills the sign-ins of your panes ([2f5a223](https://github.com/irie-people/irie/commit/2f5a22389121fb87c169f879ed74bc40cb96a725))


### Bug Fixes

* **core:** mirror the helpers op in the core package, and name irie-agents by brand ([20163ce](https://github.com/irie-people/irie/commit/20163ce305217ab3a418944381fae5ece7091fbf))
* **core:** pick out an app or a script a link names, never open it ([e926487](https://github.com/irie-people/irie/commit/e9264876a117d6b049d9b7e5c01520b762e9249b))
* **extension:** harden the terms reader and the host after review ([a859ff9](https://github.com/irie-people/irie/commit/a859ff91f6835ac9db506cf44612701318dfe4fe))
* **mac:** ask tmux for links in the app's own terminals only ([6385cc0](https://github.com/irie-people/irie/commit/6385cc0f201fad111a8b3de96c0b13e2bfbecfdb))
* **mac:** make every ⌘-click on a link open it, and leave the wheel be ([508fc5e](https://github.com/irie-people/irie/commit/508fc5e8b3038b5e0edb96481dd16aa05e31bdd5))
* **mac:** wait for the terminal before telling the core its sizes ([0f49cfe](https://github.com/irie-people/irie/commit/0f49cfe52d3d13979e694ed1d04970bf3af7b98c))
* stop macOS's firewall asking about irie at every install and test ([0764881](https://github.com/irie-people/irie/commit/076488168e7b6586cb60acf75ab76178feaa741d))


### Performance Improvements

* **tests:** run every test in 83 s instead of 595 s, spread over the CPUs, and rerun only what changed ([0f52e2b](https://github.com/irie-people/irie/commit/0f52e2bf4884d29c0049fdff04c52894a5be5513))

## [1.7.0](https://github.com/irie-people/irie/compare/v1.6.0...v1.7.0) (2026-11-15)


### Features

* **design:** start a design library for every client ([9245b1f](https://github.com/irie-people/irie/commit/9245b1fa52ce1231ef089cd7d198dffd2dd2b2f3))
* **groups:** move a group to another space or a new one, by menu or drag ([3b2d76a](https://github.com/irie-people/irie/commit/3b2d76aba83dd054c1558333a5946edce4ff503c))
* **mac:** fn+tab steps through the spaces ([4c7601b](https://github.com/irie-people/irie/commit/4c7601ba07201d7a9ec316de3d9ca25ca797cf22))
* **mac:** land a drag anywhere in the session list, groups into groups too ([34ae482](https://github.com/irie-people/irie/commit/34ae482acdaf6da02e5eef030e744e3a189ad938))
* **mac:** panes go over the terminal past its narrowest, and focus picks the one on top ([b36025b](https://github.com/irie-people/irie/commit/b36025becf2d21c3cd928215e73cfdf2aa28d224))
* **mac:** View › Panes Over the Terminal, and the overlap in the design library ([94232f8](https://github.com/irie-people/irie/commit/94232f80c93ef57499ba5b722879da4a7d1fd2e5))
* **plugins:** give the Variorum tab a switch of its own, so a bundle can turn it off ([5eff329](https://github.com/irie-people/irie/commit/5eff329a7b96cad9d5c98860d639f1cb4cb860ae))
* **remote:** open plugin panes on the phone ([f417507](https://github.com/irie-people/irie/commit/f417507df320854e51d29d2c09d3bef65be00676))


### Bug Fixes

* **mac:** redraw a card pinned or parked while the window is open ([dc81f78](https://github.com/irie-people/irie/commit/dc81f78612a4b9a579a18cbba7b0af0d51f4f94a))
* **theme:** keep every state colour apart for colour-blind viewers ([0545b5b](https://github.com/irie-people/irie/commit/0545b5bbef1ea2bfe5db50fafba172eba3e86366))

## [1.6.0](https://github.com/irie-people/irie/compare/v1.5.0...v1.6.0) (2026-11-15)


### Features

* **bench:** close a judged bench's runs and judge ([49656a0](https://github.com/irie-people/irie/commit/49656a006aa9fd82e80db0db4d27287a5f11d8ab))
* **core:** drop a closed session's worktree once its work has landed ([39d0c43](https://github.com/irie-people/irie/commit/39d0c435e21cadf161b55a4322bfcb9958cc8d8a))
* **core:** lock each session's setup and check it against OSV.dev ([51cd323](https://github.com/irie-people/irie/commit/51cd323da144b653f00c625691250689186e5ff2))
* **core:** make freezing or thawing them all one step of the history ([5076366](https://github.com/irie-people/irie/commit/507636660ee663c3dbaf474510b3b8ec6b03b18a))
* **core:** mark each op inside or outside, and ask only where its effect leaves Irie ([2dafac4](https://github.com/irie-people/irie/commit/2dafac4c7e6b0ed47b545588e547ce65527ddb00))
* **core:** open a file beside the selected session with :open ([67e7a89](https://github.com/irie-people/irie/commit/67e7a89173ae077f05a139471f5702bf1f81e58d))
* **core:** open a file in a code card with irie ctl open ([03c8132](https://github.com/irie-people/irie/commit/03c81329c2742893e5bc8e6d7c8d9b4688cf7232))
* **core:** open a shell card beside a peer's session over ssh ([c72861f](https://github.com/irie-people/irie/commit/c72861fca6ba4764e6f52e2a77b8648743b2ba05))
* **mac:** calm the panes into one tab row and add a code card ([3680100](https://github.com/irie-people/irie/commit/3680100d111ade33e2fb19aa4df9bbbd53187325))
* **mac:** show the session's folder as a tree in a code card ([f8a959a](https://github.com/irie-people/irie/commit/f8a959ada543cedde916906c8b24f05010358d5c))
* **route:** sort tasks by labelled ones and hand slow delegations on ([78768d3](https://github.com/irie-people/irie/commit/78768d320a8ec525e54f7a1ac480b9a52aac5c3b))


### Bug Fixes

* **bench:** keep the profile in a peer run's playbook ([a4cbde3](https://github.com/irie-people/irie/commit/a4cbde37314f4d6502278d7ef634189bb95107b1))
* **bench:** refuse prompts no one will answer in a bench's sessions ([e0bb5f7](https://github.com/irie-people/irie/commit/e0bb5f71fa4f7fc571cbe786fbc8dc9e55f77abe))
* **bench:** show a bench given its id rather than bench the id ([2808cb8](https://github.com/irie-people/irie/commit/2808cb8751895584ce7b8ab22da02ee58e8f0ef1))
* **core:** keep a worktree that holds a checkout or a local file of its own ([f40bdea](https://github.com/irie-people/irie/commit/f40bdea0877152673e69b849ff66eddc167fb4de))
* **core:** name the product through internal/brand in a drop's word ([410e43d](https://github.com/irie-people/irie/commit/410e43db1fe2d79be1f8cd1c735fd4bd447db0b6))
* **core:** start a question that names no session as a task, and keep why a task was refused in view ([7a0508e](https://github.com/irie-people/irie/commit/7a0508e6cc392c5debeca7aab88e7d1750caa915))
* **jot:** start a jot that opens with “dont think” as a task of its own ([2eeac16](https://github.com/irie-people/irie/commit/2eeac160423773f27e9351cd299ba077737add4d))
* **live:** rebuild irie when the core package changed ([bb29203](https://github.com/irie-people/irie/commit/bb292036c8a1a1706e01e3d2b7802f63e67c4ecc))
* **mac:** keep a code card's typing safe and its editor on its file ([3e5ac7e](https://github.com/irie-people/irie/commit/3e5ac7e687a10053220371ab2bc104d606e6b10d))
* **mac:** keep the selected card, its lines and the terminal's frame on one session ([daae8f7](https://github.com/irie-people/irie/commit/daae8f780001668e5ad8b9e4de0c23f1f7a5de71))
* **route:** keep a handed-on subtask's name and show a wait run past as a least ([d57829a](https://github.com/irie-people/irie/commit/d57829a8d006a0cffc14749283b99a5bcb20958a))
* **route:** let the words' hard stand over the nearest labelled task ([11d86dd](https://github.com/irie-people/irie/commit/11d86dd08aa93917fbc56f2bcb45516cfc04fd81))
* **route:** regenerate the SDK's wire types and name the product through brand ([462ae08](https://github.com/irie-people/irie/commit/462ae080137b54ece8298f67ac3fb2351b36b25b))
* **theme:** tell needs from paused and done from frozen for colour-blind viewers ([378f197](https://github.com/irie-people/irie/commit/378f197ae9847c4a059fa6214ab6c9216a6ee8c7))

## [1.5.0](https://github.com/irie-people/irie/compare/v1.4.0...v1.5.0) (2026-11-15)


### Features

* **mac:** fold the panes down to a strip of their tabs ([5d09cfa](https://github.com/irie-people/irie/commit/5d09cfa934705fd5f566f5335ddecb7c3b0eeac0))
* **windows:** carry the core and the slot's terminal over one wsl.exe ([c9ff76b](https://github.com/irie-people/irie/commit/c9ff76b7641664b8eb98f79177cdc547e937f06f))
* **windows:** draw Irie in a window of its own on Windows ([34b05b2](https://github.com/irie-people/irie/commit/34b05b26e78722850ddeb56c55cb12e2a2de1aca))
* **windows:** install Irie for Windows and ship it in every release ([71959e7](https://github.com/irie-people/irie/commit/71959e7d577dff01bfc7204a221de2e8f3762b52))


### Bug Fixes

* **windows:** call WebView2 without go-webview2's generated binding ([5dfe0d0](https://github.com/irie-people/irie/commit/5dfe0d0b80d1cf2d1010dbc0443d7d2e67032a2e))
* **windows:** keep the page's first messages and COM's pointers safe ([aaf784e](https://github.com/irie-people/irie/commit/aaf784e99328b04cce3ca9244990b6db79c07edd))
* **windows:** name the product through internal/brand ([63361fc](https://github.com/irie-people/irie/commit/63361fcf4a14331ae1a41f58ea13426c55bc1ed2))

## [1.4.0](https://github.com/irie-people/irie/compare/v1.3.0...v1.4.0) (2026-11-15)


### Features

* **demo:** make a launch kit for each release ([532707a](https://github.com/irie-people/irie/commit/532707aac5ac1bd37b6d05c6b8c4ac5ce5aea2ad))


### Bug Fixes

* **plugins:** keep a broken manifest from hiding the plugins, and tighten panes ([deac4c4](https://github.com/irie-people/irie/commit/deac4c47aa1f404f417feea43e0d7b1cd7b031c9))

## [1.3.0](https://github.com/irie-people/irie/compare/v1.2.0...v1.3.0) (2026-11-15)


### Features

* **browser:** return a script's table as TOON to save the agent tokens ([e04152f](https://github.com/irie-people/irie/commit/e04152fcbad022bcdd601636c5d1668dd4af4b7b))
* **core:** keep held-back agents at full speed, and boost one after two API errors ([b5ff4bc](https://github.com/irie-people/irie/commit/b5ff4bc7eb54d6504d8baa228f12f38d4a7e0be6))
* **core:** meter prompt-cache rebuilds and weigh a cold wake's price ([8cbb64c](https://github.com/irie-people/irie/commit/8cbb64c49190e4e6ea4ff8f4b9ef133e01cdb4cc))
* **core:** run a shell of your own beside each session ([886626e](https://github.com/irie-people/irie/commit/886626ed67a6fd56c9c9eed748d15c316f7e50be))
* **core:** switch a session to its profile's fallback model at its model's own usage limit ([3ba71e5](https://github.com/irie-people/irie/commit/3ba71e550c0b0fbe696bd73922f6b103ddb48b24))
* let a boost change only the priority, never a card's place ([28b9620](https://github.com/irie-people/irie/commit/28b9620fb95bef7078c4c0fe5439f3862e24a672))
* **lid:** put the display to sleep when the lid closes during a hold ([0291d42](https://github.com/irie-people/irie/commit/0291d42d76b325402b88a9c9d447ebf12c3ec193))
* **mac:** offer lockdown and account time only for a VPN that has them ([8c14600](https://github.com/irie-people/irie/commit/8c1460039451612a2192e857ee64f9523930a475))
* **mac:** show Mail in the right sidebar and a window of its own ([67c31d1](https://github.com/irie-people/irie/commit/67c31d1d682ef9e3a2ccf4e5b2fec1ba0ceefa60))
* **mac:** show the note a writing session works on in a Variorum tab ([6f4934f](https://github.com/irie-people/irie/commit/6f4934f8cb81ace4de72d1dc9edba6986523b181))
* **mac:** show your own panes beside the terminal as a rolodex ([22827f7](https://github.com/irie-people/irie/commit/22827f793f21b20c8fb0702f26f08703443848f0))
* **plugins:** give :write sessions Variorum's door and add the variorum op ([b1d4db5](https://github.com/irie-people/irie/commit/b1d4db57ce2e71c60a9fa0149fcdda8064b3da40))
* **skills:** add the chaos monkey, /irie:chaos and +chaos ([3e47b40](https://github.com/irie-people/irie/commit/3e47b4016748d9c0f1fb852f943c4c407c9bf1ae))
* **vpn:** follow any VPN, and pin a company's copy to the one it approves ([bcf1573](https://github.com/irie-people/irie/commit/bcf15736141bde08ee8530cb92f4100d0869f707))


### Bug Fixes

* **core:** guard the boost's carry-on and the hook events a restart missed ([e583931](https://github.com/irie-people/irie/commit/e583931b7341924cea1107adc85134473a0a4d6a))
* **core:** leave a session without the model-switch hooks to wait for the reset ([c510fc7](https://github.com/irie-people/irie/commit/c510fc7c38e2aa44f5d4d3fa3499102616d3293d))
* **core:** name the dog's unproven claim in the generated wire ([90b6936](https://github.com/irie-people/irie/commit/90b6936d8e53a90c28726461f0d5e0837527112c))
* **core:** name the product through brand in the meter's cause words ([c13e1bc](https://github.com/irie-people/irie/commit/c13e1bc44eb568835b47733464218d2e04a95b05))
* **core:** price the conversation a session runs now, and tell long tool calls apart ([8ad4cba](https://github.com/irie-people/irie/commit/8ad4cbadd7ff8c97838fdfa05c26a52f9a80ffc8))
* **core:** start a pane's shell once and never leave it unowned ([34c0820](https://github.com/irie-people/irie/commit/34c08207b3d722cb3b81f3768ccd0985b4089098))
* **local:** cap the prompt cache and checkpoints of local model runners ([c46bbfa](https://github.com/irie-people/irie/commit/c46bbfa0d28d604ed235eae712b9a2a1ecf9f595))
* **local:** refuse a model on Ollama's MLX engine, whose memory Irie cannot cap ([d3ea809](https://github.com/irie-people/irie/commit/d3ea80961a2fe8fd2e926d6cbf697bd24c0ea54c))
* **mac:** bring the selected card into sight when the list is drawn afresh ([36cb99c](https://github.com/irie-people/irie/commit/36cb99c479b8b61fabcb875ff293932fd3ecd31f))
* **mac:** call the Mail parts' menu item Show Mail ([c999897](https://github.com/irie-people/irie/commit/c99989714e9274d1d09034996db9265afb104504))
* **mac:** keep the keyboard where you send it as the panes turn ([4b772c0](https://github.com/irie-people/irie/commit/4b772c0017c72fc0e9d088696426a8c76a7dd32b))
* **mac:** leave the live pane's marker out of drawn turns ([2af64b2](https://github.com/irie-people/irie/commit/2af64b25271f26dee79b282578078b5157ca7c1d))
* **mac:** name the right sidebar's tabs in its button's tooltip ([e7dd078](https://github.com/irie-people/irie/commit/e7dd078a0fe4573679484685cdefcbe8106fcb75))
* **mac:** remember the app's desktop when its window comes before launch ends ([01d2481](https://github.com/irie-people/irie/commit/01d2481a964ad14e20027093e3d178cc446a4cab))
* **mail:** sort by arrival and list folders on any server ([089a4e6](https://github.com/irie-people/irie/commit/089a4e69216eeb0251cd2b4f08bfc1b9e79ffff2))
* **provenance:** drop a row the union merge listed twice ([7a705b4](https://github.com/irie-people/irie/commit/7a705b4ed8e5d634acb856c7e16e5eff03ebee58))
* **skills:** match the variorum skill to Variorum's door ([2ff1ee4](https://github.com/irie-people/irie/commit/2ff1ee46ae3eb3dda7ea0e6034f88de634391ff4))
* **vpn:** keep addresses and prompts out of errors, and order reads ([d05525c](https://github.com/irie-people/irie/commit/d05525cf80dd598c506368cff504bff842967d5f))
* **vpn:** put --json before listen, so Mullvad's state comes through ([a0bc868](https://github.com/irie-people/irie/commit/a0bc8680da463406e1ad3073b48287843e2146b9))
* **vpn:** send the person to Cisco's app rather than open it ([b39927c](https://github.com/irie-people/irie/commit/b39927c1f138d762c725c35e56ef1a638c1de963))

## [1.2.0](https://github.com/irie-people/irie/compare/v1.1.0...v1.2.0) (2026-11-14)


### Features

* **arch:** let Gabriel boost your focus always ([0709aed](https://github.com/irie-people/irie/commit/0709aed5f3952e5b96e55ffcc2a92db0caed2cbd))
* **arch:** put the tab you are on first in Gabriel's focus ([46b473f](https://github.com/irie-people/irie/commit/46b473f232fb5435f8d4bc68e2c37405bee4ee86))
* **groups:** make a new group from a session's tab ([7625872](https://github.com/irie-people/irie/commit/76258726105be9b38cb4273757ec79b2f4bb7906))
* **groups:** put groups inside groups, and drag a group into another ([6816ee9](https://github.com/irie-people/irie/commit/6816ee9422aedf1d7727368e9331297b2ceafb51))
* **mac:** show the always boost in the menu, from the core's header ([62232b5](https://github.com/irie-people/irie/commit/62232b566260f1049feabc34da06a4e522f36107))
* **plugins:** add a markets plugin that shows your market map in a window ([08f733e](https://github.com/irie-people/irie/commit/08f733e3ebc9b6efad8ea55c76b67820494de043))
* **ui:** draw groups inside groups in the terminal sidebar ([4bf4d1d](https://github.com/irie-people/irie/commit/4bf4d1dc9599a7683d96188bd7feba8cd1265d33))


### Bug Fixes

* **arch:** keep the tab on top known whenever you are at the computer ([9d9e28b](https://github.com/irie-people/irie/commit/9d9e28b1c8bee7f7eaee718703daf3d348f7be42))
* **groups:** a drop in its own group moves a card there, and a renamed group's moves say its name ([bea1f1c](https://github.com/irie-people/irie/commit/bea1f1c65f40648440ee3ae7ef6b4f5357e9dd0d))
* **groups:** find a move's group in its space, and describe nesting ([c7e9f71](https://github.com/irie-people/irie/commit/c7e9f71fbf35b8c9286471349c7e76130a936411))
* **groups:** keep each group together, in place and in its space ([e5621cb](https://github.com/irie-people/irie/commit/e5621cbd5368c15e6c74ef4fa77a49417443be94))
* **groups:** keep the product's name out of the group examples ([0136b22](https://github.com/irie-people/irie/commit/0136b2280c1390d59e5dcc6870c25887f81498a8))
* **groups:** what review found in groups inside groups ([bac934c](https://github.com/irie-people/irie/commit/bac934c1ea10ef9d89b3ed4383307f261809cea9))
* **mac:** keep the Markets window's state on the main actor ([70610e5](https://github.com/irie-people/irie/commit/70610e5af587839b9e2797a38835c9d0a9e5cb07))
* **mac:** number each installed build so macOS reopens it, not an old copy ([2a03acb](https://github.com/irie-people/irie/commit/2a03acbfc565a3c9fdf52ecced35b85793e00fe7))
* **mac:** open a Markets link only at the person's click ([40b78ea](https://github.com/irie-people/irie/commit/40b78eace49e2bfc09f901d249876395cb52164f))

## [1.1.0](https://github.com/irie-people/irie/compare/v1.0.0...v1.1.0) (2026-11-14)


### Features

* **brand:** keep the product's names in one place ([8698483](https://github.com/irie-people/irie/commit/8698483ee742a77e8b66f4ab36238904dcc90f0a))
* **brand:** name the build tags a copy builds with ([698d654](https://github.com/irie-people/irie/commit/698d654318f229c1f8aa4e755fa8b08dfaaa8c41))
* **core:** bring back the sessions a restart lost ([24cf8ab](https://github.com/irie-people/irie/commit/24cf8ab0f29fc0a837c60b43f4eebdf1cef7752a))
* **plugins:** build Irie without a plugin's code ([a813f8c](https://github.com/irie-people/irie/commit/a813f8c74402ce44ce8dd438d49f1d7d6f5fbe2e))


### Bug Fixes

* **anon:** close three leaks found testing anonymous research ([60adf55](https://github.com/irie-people/irie/commit/60adf55b6c1654241ccfeff8a14bff4cf8d58bb1))
* **anon:** keep a quoted +anon task in its compartment ([726c120](https://github.com/irie-people/irie/commit/726c1201d593184e5fc0a929be41c089590280eb))
* **anon:** shut an anonymous session's shell in a sandbox ([c26dbed](https://github.com/irie-people/irie/commit/c26dbed438f4a8ca0bec305d9741d0ccbb70eeea))
* **mac:** give each build its own Brand.swift draft ([30b86d7](https://github.com/irie-people/irie/commit/30b86d72073cdfe45bd802b4cb045373556ae6ce))

## [1.0.0](https://github.com/irie-people/irie/compare/v0.2.0...v1.0.0) (2026-11-09)


### Features

* **anon:** research anonymously in compartments with exits of their own ([244f5b7](https://github.com/irie-people/irie/commit/244f5b750c42961903989f8fdf39d2b0e73b1f39))
* **browser:** agents drive Mullvad Browser through Irie's own MCP server ([49d1b30](https://github.com/irie-people/irie/commit/49d1b30510b8938791868915f24d6b16d1a49f02))
* **claude:** run sessions on the Agent SDK for credits that do not cover Claude Code ([c489b2e](https://github.com/irie-people/irie/commit/c489b2e7f8e5796917e7b91eb3a3fe2ce7c51f63))
* **context:** let old contexts go after context.keep_days, 30 by default ([5826794](https://github.com/irie-people/irie/commit/58267944c6fe5a5c8c379747a4e559dfe34348d6))
* **core:** a show brings its session into Gabriel's focus at once ([7375aaf](https://github.com/irie-people/irie/commit/7375aaf7006c50fb8c2d77304eb917ebbcfa25df))
* **core:** Claude Code's own panels in Irie's palette ([7e02412](https://github.com/irie-people/irie/commit/7e0241230b616935a92202330b7ecf7a65586444))
* **core:** every client draws in the Mac app's palette ([d0be490](https://github.com/irie-people/irie/commit/d0be4901ff25d450d2dfccbf8964532650b4b34a))
* **core:** make rename, reorder, freeze and promote undoable ([d03b8d1](https://github.com/irie-people/irie/commit/d03b8d1086f9a7ce35c381b6bd0d050ca569d1dd))
* **core:** pin sessions, close several as one undo step, and place new tabs ([806540c](https://github.com/irie-people/irie/commit/806540c316a51994da6dfe8e103d79d00157d301))
* **core:** spaces, named sets of sessions a sidebar shows one at a time ([f4ddaca](https://github.com/irie-people/irie/commit/f4ddacaccf0205a733e4343f19a7fd7a80918f1d))
* **core:** the theme op takes a palette of the user's own ([3e096c1](https://github.com/irie-people/irie/commit/3e096c1f940370e9f7a3b264e921352624877d15))
* **demo:** reels of a card that needs you, a closed window and a local model ([4b2feb9](https://github.com/irie-people/irie/commit/4b2feb9bf84d506d148059a625d687c987f1c1ca))
* hide a card's helper or teammate, or stop it ([2b7efb0](https://github.com/irie-people/irie/commit/2b7efb0ef0187f11b39445a650913861f27b6be3))
* **localmodel:** estimate sliding-window and looped models' cache ([def1b6f](https://github.com/irie-people/irie/commit/def1b6f079da8b352825878f16c01db14bfe71a3))
* **mac:** a Plugins menu in the menu bar ([ff1eedf](https://github.com/irie-people/irie/commit/ff1eedfa6bd9d87d9942969f7cca5cfa0c3e5a8e))
* **mac:** film answers, a closed window and steady frames in recordings ([6cd1b98](https://github.com/irie-people/irie/commit/6cd1b98c9e235742f5d5e9f4a4ca2de134dddce7))
* **mac:** make a theme, and save the window's layouts ([445c1c2](https://github.com/irie-people/irie/commit/445c1c24e807a451395e08a079968136d9abc162))
* **mac:** pick how long conversations are kept, in Settings ([1aa522f](https://github.com/irie-people/irie/commit/1aa522f43562d2ca5c4454c39c1ae59fa2f28a90))
* **mac:** pin, close several, pick cards, move keys and a Tabs settings pane ([f9f5611](https://github.com/irie-people/irie/commit/f9f5611ef72cb3c39f1e2197295dd3b3a3a1ff94))
* **mac:** show Irie's plugins in Settings, with each part's state and switch ([3f037f0](https://github.com/irie-people/irie/commit/3f037f0ea8773792b2d76cbbce97d29aac91a3eb))
* **mac:** show notices clear of the task bar, and keep them in a Notices tab ([6898153](https://github.com/irie-people/irie/commit/6898153b7a647f6bacb9129492b11be728120a9a))
* **mac:** slide the right sidebar in from the window's edge, as a drawer ([d8e08b7](https://github.com/irie-people/irie/commit/d8e08b70400de7ee46c6d6083528f0d36cceb6c0))
* **mac:** spaces as chips under the list, with keys and Move to Space ([670d66f](https://github.com/irie-people/irie/commit/670d66fe51b1c639d9e284a9b00086cbd7986a98))
* **mac:** the right sidebar sits inline after the terminal ([da5e3d4](https://github.com/irie-people/irie/commit/da5e3d487ab8fa4c63163464f60645442326562d))
* **park:** park moves a card into Parked at once, and unpark puts it last in the list ([2aadc37](https://github.com/irie-people/irie/commit/2aadc374fde55a6398b7abadfe18172308ad20c6))
* **peers:** put another machine's sessions in a group of their own ([cf8462c](https://github.com/irie-people/irie/commit/cf8462c48c93048dfbf39fddc62cc5f3b90c6e6d))
* **plugins:** a writing plugin, for writing in Variorum ([0caab9c](https://github.com/irie-people/irie/commit/0caab9cc0ea3aa06e8ffc33a3a2ff71cf79d47d5))
* **plugins:** plugins for Irie, starting with privacy ([4f7ab2a](https://github.com/irie-people/irie/commit/4f7ab2aa9100e75074a34ece74a886761a96cb50))
* **remote:** a Term view on the phone, with the terminal live ([c2ab7f2](https://github.com/irie-people/irie/commit/c2ab7f2323579f685c913a49f73be619f1e88b24))
* **site:** link-preview tags and a preview picture on every page ([1d12ffa](https://github.com/irie-people/irie/commit/1d12ffa999a7dd764ce479161523359137c96480))
* **trod:** keep the week to a day line with trod.pace ([df45141](https://github.com/irie-people/irie/commit/df451416e15deafacae9e7ddaed4446c79ed48d5))
* **write:** write with Variorum, Lillian Duarte's writing app, from any task bar ([5a0ec8f](https://github.com/irie-people/irie/commit/5a0ec8ff0d14f5659e45135ddd424f625de98256))


### Bug Fixes

* **anon:** turn anonymous research on before the optional installs ([088057e](https://github.com/irie-people/irie/commit/088057eab6459896489e85e5e704c5e9990b0297))
* **anon:** wait for Tor's ports to open before using them ([2267d50](https://github.com/irie-people/irie/commit/2267d5050a720b549d7a3337c7385dc44c0c8541))
* **arch:** Raphael gives a sidebar a minute before he restarts it ([370ea3c](https://github.com/irie-people/irie/commit/370ea3c03dbfadf5a0044f5aa3afbd63964b0357))
* **browser:** say 1 link listed, not 1 links ([671ac9d](https://github.com/irie-people/irie/commit/671ac9d8e4b35778f4662dc32b2bfead7efd6dd9))
* **claude:** run local models in accept-edits mode with build and test commands allowed ([f0a5dee](https://github.com/irie-people/irie/commit/f0a5dee0ca9a420e7b9913c37d097ed1f63eada1))
* **context:** a turn is one commit, its bookkeeping riding with the next ([83efc92](https://github.com/irie-people/irie/commit/83efc92f9f70f7891251d42d79ec84cba8b0ed58))
* **context:** pack the store and give its gc time to finish ([54f59cc](https://github.com/irie-people/irie/commit/54f59cc1db5b89467984e6fcb5a7be465afac655))
* **context:** store a turn as one commit, once its reply is written ([0f1096b](https://github.com/irie-people/irie/commit/0f1096bbcf885306ef1c19195689dde7432e5558))
* **core:** fit the waiting state round live's words, trod bot and lid ([f2d5637](https://github.com/irie-people/irie/commit/f2d5637da1b4bb613f97e86e9afce79caa11360e))
* **core:** round the agents' memory in the header up, not down ([42eef29](https://github.com/irie-people/irie/commit/42eef294598ad0d716821cb6ff535ef929c59971))
* **core:** stop the Iries that test runs and checks leave running ([3a4e220](https://github.com/irie-people/irie/commit/3a4e22060d109f97db7a6e2d04eae84d96eaa8c7))
* **core:** wait a minute for a new core to answer, not five seconds ([e34c0cf](https://github.com/irie-people/irie/commit/e34c0cf832e6861db9787f48c580ff1880b7515f))
* **demo:** delete a sandbox once its agents have finished writing ([e1f5e09](https://github.com/irie-people/irie/commit/e1f5e0960ed8a2555540f81c6863022d7f43bc43))
* **mac:** drag a program's panel edge in the terminal ([a3b53c9](https://github.com/irie-people/irie/commit/a3b53c937768fd9d968feb22031888691a91399c))
* **mac:** fill the join between the selected card and its terminal, over the divider ([7d6ea06](https://github.com/irie-people/irie/commit/7d6ea06e1e3bc5e6330d55d7e9f35a14a09782dd))
* **mac:** give the recording's window the store as the views now take it ([95d5245](https://github.com/irie-people/irie/commit/95d5245f42f1817bcb2c22e063f8885d87a916f4))
* **mac:** keep an arch item's buttons on the main actor ([f61b87e](https://github.com/irie-people/irie/commit/f61b87e2b00e57a6bda9cfcea64a5c0b5134a063))
* **mac:** resize the terminal once a drag of the sidebar ends ([7114385](https://github.com/irie-people/irie/commit/7114385718b2e180c516d2e17d7bdfd150f2ea21))
* **mac:** show a card in another space with one swap of the terminal ([1372b47](https://github.com/irie-people/irie/commit/1372b47ca163762b4e4329e8656490ae96ec623e))
* **raphael:** name each waiting update once, and an empty plist fairly ([f2606f4](https://github.com/irie-people/irie/commit/f2606f44c295f93482c1974a610d05863f22985f))
* **raphael:** take no sleep for a stuck core, and open the app again ([3e9779b](https://github.com/irie-people/irie/commit/3e9779b8edad5b54661dfa9b88a0ee5a2c254c03))
* **sdk:** keep escape sequences, controls and tabs in shown text out of the terminal ([4864b00](https://github.com/irie-people/irie/commit/4864b00bf3170c30f31b9994aef49a7842a6d794))
* **trod:** leave a peer's rows to its own core ([49c7483](https://github.com/irie-people/irie/commit/49c7483ccd9d5df096c2125e3a1681ec4568d8c2))
* **trod:** thaw one session at a time, and none into a signed-out agent ([fde80f3](https://github.com/irie-people/irie/commit/fde80f3995d42b578de16f3e25c7f0c9a5fdb880))


### Performance Improvements

* **mac:** a card's close items read the rows only once chosen ([722335c](https://github.com/irie-people/irie/commit/722335c1f6660ec8cd553339a9a4fc2973cbd4d8))


### Miscellaneous Chores

* release 1.0.0 ([cb1ecdb](https://github.com/irie-people/irie/commit/cb1ecdbe56979a02c28e0242fe41afab824e62cf))

## [0.2.0](https://github.com/irie-people/irie/compare/v0.1.0...v0.2.0) (2026-11-02)


### Features

* **arch:** every arch agent reports through the core, with a nickname and an icon ([71759a3](https://github.com/irie-people/irie/commit/71759a3637be814a9dd973100cb4d7dba929711e))
* **arch:** Gabriel gives the user's focus the machine first while it is busy ([a6cba59](https://github.com/irie-people/irie/commit/a6cba59e658980303cf8c0496d8c9ccd1f818bb5))
* **arch:** Gabriel keeps agents from taking your focus, and installs wait until you look away ([ca11ba9](https://github.com/irie-people/irie/commit/ca11ba909982ddd22a447510d17b34aedf2aa2aa))
* **arch:** Gabriel's boost puts your focus first at once, the safe overclock ([b6ca0b5](https://github.com/irie-people/irie/commit/b6ca0b5c87194e6db5a6a908caa05812dd5e161d))
* **arch:** Mikael, the first arch agent, stands with every session against evil ([819788e](https://github.com/irie-people/irie/commit/819788e257e1a03abc374b8d5b4fc19d4c77bcd1))
* **core:** a jot that reads as more for the one just sent goes to its agent ([0800284](https://github.com/irie-people/irie/commit/08002847c118d34591f844375e7842c5f1c84945))
* **core:** a pair op for clients that show a pairing code ([d26a49e](https://github.com/irie-people/irie/commit/d26a49ed9fbd82741eaff765f0a83a2a8c74ffcb))
* **core:** a peer's finished work comes back, and no peer's frame can stop the core ([e5262b0](https://github.com/irie-people/irie/commit/e5262b0231d3c1f50f94ff89dd0d1ef3c67362d3))
* **core:** a peer's row says where it runs, in every client ([aa603e0](https://github.com/irie-people/irie/commit/aa603e0ce7d1efa6b3ac027b1e0ec141a75ac5dc))
* **core:** a task typed to input is a jot, and input's apart takes one back ([2f14c08](https://github.com/irie-people/irie/commit/2f14c084759db0988c194d00ce011c893b2d081a))
* **core:** irie-core, one core that reads what a person types for every client ([126f2e3](https://github.com/irie-people/irie/commit/126f2e31d99d45afcd14edafc3fed144af194752))
* **core:** peers, another machine's sessions in the list and ^name to start one there ([10e4299](https://github.com/irie-people/irie/commit/10e42997fc0120902124e325260da6020f8f9b28))
* **core:** screen and keys ops for clients without a terminal ([111ef03](https://github.com/irie-people/irie/commit/111ef0347eefa2fbc071a934f9ce5e507046fd21))
* **core:** the balancer, and no task where the hooks differ ([87bce2b](https://github.com/irie-people/irie/commit/87bce2bcaa4fa59134e56284ab5a5adbc9664ca5))
* **core:** the header says how busy the machine is ([f25c4ef](https://github.com/irie-people/irie/commit/f25c4ef41da32cedc57e394d39fe0248a721b1b5))
* **filetx:** writes take the file's write lock and land in one rename ([b071a38](https://github.com/irie-people/irie/commit/b071a384821b7ddbb8f06180e37090bd24078399))
* **install:** one line installs Irie from each release's downloads ([ad52ee6](https://github.com/irie-people/irie/commit/ad52ee6f08d7a44e46a473ac92c3500516680e6e))
* **ios:** a native iPhone app ([6e4d5ec](https://github.com/irie-people/irie/commit/6e4d5ecb3dfa109bb801d5066080f4ddb8613eeb))
* **ios:** the VPN on each machine's card, with Connect and Disconnect ([2e85559](https://github.com/irie-people/irie/commit/2e85559431015bdaccdbcf4a8fcf23b804e863f7))
* **learn:** tutor mode, pictures and Irie tips for Claude Code sessions ([84b5116](https://github.com/irie-people/irie/commit/84b51167f6e762c6a46efb86a71b6598d6c63182))
* **live:** an install can also fast-forward a remote main, with irie.push ([611cdd1](https://github.com/irie-people/irie/commit/611cdd1fb9bb4e65a015175ab674a12b3fd73eda))
* **mac:** a drag selects in the terminal and copies when you let go ([6102bce](https://github.com/irie-people/irie/commit/6102bce7233a2fdc684bcc9eb390778bbea7c620))
* **mac:** a Learning tab, a Help menu with resources and a glossary, and diagrams ([812f854](https://github.com/irie-people/irie/commit/812f8548fb2d4046a31c8fa200befe38cd80a566))
* **mac:** Irie.app for Intel Macs too, in a release's universal app ([1310b0a](https://github.com/irie-people/irie/commit/1310b0a1cea0eb1c0c482dfe2a943d1cbd668df9))
* **mac:** Irie.app starts Irie itself and welcomes a first run ([1bd163c](https://github.com/irie-people/irie/commit/1bd163cbef88ad1f2da8a0f0affda5c8f950197a))
* **mac:** sign Irie.app with your own identity ([02ba684](https://github.com/irie-people/irie/commit/02ba684725df0cd29c9b591173fb887e9d38922d))
* **mac:** the task bar sends what is typed to the core, so every : command works ([e514df3](https://github.com/irie-people/irie/commit/e514df3517d1f5282f55ccb21854b410ef7719ae))
* **mac:** the task bar takes the next jot at once and offers New task ([155ed49](https://github.com/irie-people/irie/commit/155ed492581cd77fca24d513394cdc8b7b931c49))
* **peer:** irie peer list says whether each machine takes tasks, and why not ([ce9878b](https://github.com/irie-people/irie/commit/ce9878b0215d52707432f1c0f60c60dced639428))
* **remote:** a pairing link for a native iPhone app ([c7d93be](https://github.com/irie-people/irie/commit/c7d93bed944c7ca45bc0ba3e205fe838303f8031))
* **remote:** irie remote on and off ([d0ce282](https://github.com/irie-people/irie/commit/d0ce282398e7eae4f31b0db70ab4a315fb09e447))
* **remote:** log each setup step and each launch of the app ([64ef56c](https://github.com/irie-people/irie/commit/64ef56c2b03bf1ab3a79fd332f349a7dfa716a35))
* **remote:** notifications in the phone app, and polish ([fdcea11](https://github.com/irie-people/irie/commit/fdcea117e6ad8f129069ab8d2bfe066e34788f74))
* **remote:** notifications when a session needs you ([c66dd4a](https://github.com/irie-people/irie/commit/c66dd4a769e499fa9b224c3a9f7d3d1fcfef404d))
* **remote:** the phone app ([5a944c2](https://github.com/irie-people/irie/commit/5a944c2ed27190995b4e29b6089aa0ef053827b7))
* **remote:** the phone app's gateway, pairing and certificate authority ([a0779ff](https://github.com/irie-people/irie/commit/a0779ffe98dd04dbfa776e5e0e8fbf2dac599963))
* **setup:** irie setup and a first run that get a computer ready for Irie ([ef33a7b](https://github.com/irie-people/irie/commit/ef33a7b87393c7b2f197aa03af06c8cc11b80bf2))
* **skills:** a dead man's switch guide, the first of Irie's own skills ([0272b94](https://github.com/irie-people/irie/commit/0272b9453d44d6c72771b6e1f40a1b26403ed1e7))
* **skills:** hand each Claude Code session Irie's own skills as a plugin ([aa041dd](https://github.com/irie-people/irie/commit/aa041ddd109ad95f844dea28a633a1d5a4512584))
* **spawn:** your preferences, told to every session as it starts ([e66020e](https://github.com/irie-people/irie/commit/e66020eb17b208f2f82a43eec3dddb3bdd6d7f75))
* the freeze dog restarts Irie.app when it stops answering ([865d507](https://github.com/irie-people/irie/commit/865d507b00206abda887032b36fa8d4c8912ac49))
* **trod:** keep sessions going at night while you are away ([82a7cf8](https://github.com/irie-people/irie/commit/82a7cf828cb65e4fa319174f6c230d188dca5991))
* **trod:** start the trod bot by hand and see its boot print ([0a4c23d](https://github.com/irie-people/irie/commit/0a4c23df6691b8d2291935ce0a38329ea11f1851))
* **ui:** the terminal sidebar welcomes a first run with a task to try ([18ba5d8](https://github.com/irie-people/irie/commit/18ba5d84e2fb623f951cde027c13c42cb4442d72))
* **vpn:** Mullvad VPN in every task bar, the sidebars and the phone app ([2fb303b](https://github.com/irie-people/irie/commit/2fb303b93cec3be1053696ddbad34e8166747903))


### Bug Fixes

* calm agent colours, near opposite for cards side by side ([f9e03cc](https://github.com/irie-people/irie/commit/f9e03cc7954dfce8e0a47adf983c073dd76ce357))
* **core:** the public wire carries Gabriel's report ([7a776e2](https://github.com/irie-people/irie/commit/7a776e29ad646b49d7e4366bd94e4e2eb727b99b))
* **install:** no curl error before building from source ([cb82e71](https://github.com/irie-people/irie/commit/cb82e71af774c78b35f0b8a8ea24be8aee64e51d))
* **install:** sign a release's Irie.app with the identity build.sh uses ([3d11d93](https://github.com/irie-people/irie/commit/3d11d934e6ec2d83f58460c392061778ab9dc349))
* **ios:** the calm agent colours, and the colour the core gives ([b8e2327](https://github.com/irie-people/irie/commit/b8e23273810558eae5b0c5d1bebfcd293b677193))
* **live:** say when the install holding the lock is stopped ([412f15b](https://github.com/irie-people/irie/commit/412f15b5c7ba8f92ab29ace3a3ec29f8aa18d8aa))
* **mac:** read the setup session's reply with a type of its own ([c941874](https://github.com/irie-people/irie/commit/c9418744e626fbe567e4a60bf8e9a2e5d55b2668))
* **mac:** show a long preference or rule whole in Settings ([4aedb46](https://github.com/irie-people/irie/commit/4aedb46abd332ce5bb0cac9a538dad2be6cdd436))
* **mac:** the task bar's count of tasks starting no longer clashes with starting ([2aa2357](https://github.com/irie-people/irie/commit/2aa235727075d8c87634a87e5de558ea2caef2bd))
* **remote:** close what a review of the gateway found ([95ef8c7](https://github.com/irie-people/irie/commit/95ef8c770b6fff8a3617aa775441354609b92ffb))
* **remote:** code in link text, escaped backticks, and the app's cache ([9235848](https://github.com/irie-people/irie/commit/92358486477d14984a97ac5e9f9e607a011dada9))
* **remote:** no second first notification for a subscription the phone sent before ([95ef15f](https://github.com/irie-people/irie/commit/95ef15f24b1b56d409ccc7473c2f116027fd4118))
* **remote:** no Undo after a freeze, which the core can't undo ([ad7a063](https://github.com/irie-people/irie/commit/ad7a063ba7f0990690972ed990b6091f4282fe17))
* **remote:** the rest of the gateway review ([b66a865](https://github.com/irie-people/irie/commit/b66a865e27af7390382c84c487c5b922dc8ef0b6))
* **skills:** a guide runs in a folder of its own, trusted once ([7f59304](https://github.com/irie-people/irie/commit/7f593040c7f257db441d0f64c24d18c668aefa21))
* **skills:** the dead man's switch guide lists its contact card helper ([b23c999](https://github.com/irie-people/irie/commit/b23c99946cd30d5e386400d10df4a562216cdbcf))
* **test:** a killed or timed-out test run leaves no tmux servers behind ([90d30fc](https://github.com/irie-people/irie/commit/90d30fc718dede62d9f092eaad8c216d9fc03dd9))
* **vpn:** read the relay's name as mullvad-cli 2026.3 prints it ([13b838e](https://github.com/irie-people/irie/commit/13b838ecff194199647de7640038f6900905abe1))


### Performance Improvements

* **core:** the machine advert costs less while nothing changes ([235a62e](https://github.com/irie-people/irie/commit/235a62e929d6b7f704a1b9b99aa2bb4596e284b9))

## 0.1.0 (2026-10-31)


### Features

* a finished turn reads "to verify", and irie ctl close lets an agent close its own session ([ba4f211](https://github.com/irie-people/irie/commit/ba4f211c39c9fc545072ac961dea6644c07a01bb))
* a helper's work beside its session, as cards or as a terminal ([f56d838](https://github.com/irie-people/irie/commit/f56d838bbc329f3dcc6d33a802b4e6a6603bc865))
* a Settings window with every preference, saved to config.toml in place ([3d79c27](https://github.com/irie-people/irie/commit/3d79c2717b6d7eb70891e1dfb0f4e4c2714e3f46))
* a working session's state and its details take its agent's colour ([e5509c3](https://github.com/irie-people/irie/commit/e5509c3dc79467d41ffd0672299cc3a0f8bd6fd1))
* **core:** a jot dog sees each jot through until its work lands ([7feed9d](https://github.com/irie-people/irie/commit/7feed9dfd410c46a3c9bc6fefe7d189f95b66f00))
* **core:** close and reopen sessions ([8baafe7](https://github.com/irie-people/irie/commit/8baafe7708ada0872046968ab8d3cffa3926033d))
* **core:** freeze a whole tree at once, promote a session, keep one order ([7a9c18d](https://github.com/irie-people/irie/commit/7a9c18d9467e0feecb0a9a1a5a4d13a4eb3c92a6))
* **core:** send a message to an agent as a person typing would ([c909ae9](https://github.com/irie-people/irie/commit/c909ae94583f941d705130e821980da830280cd5))
* **core:** sessions get readable titles that follow Claude Code's own ([c874d47](https://github.com/irie-people/irie/commit/c874d47902f851aa3c89b27808a0bb283c12305d))
* **core:** spawn.close_when_done closes a finished session after close_delay ([7f8a25e](https://github.com/irie-people/irie/commit/7f8a25e0e9538536cbab78a428d3169a3c27ff58))
* **core:** undo and redo, asking when it is not sure ([9abaf32](https://github.com/irie-people/irie/commit/9abaf322c982ad2ba997bb02ec866c41cbee4ed1))
* **core:** undo takes back a park, and irie ctl parks and unparks ([d95cea0](https://github.com/irie-people/irie/commit/d95cea04f6681c949eacfe47c288225af02b108d))
* each row says in plain words what its agent is doing ([8b258c3](https://github.com/irie-people/irie/commit/8b258c34cb57dd03f5f90c32b118e864e5739cc0))
* **live:** an install must leave every agent's process as it was ([d45ab16](https://github.com/irie-people/irie/commit/d45ab167c54de58008c25ed38241f0a3c37c653c))
* **local:** +llama:&lt;model&gt; for one task, and name a model that fits now ([556244e](https://github.com/irie-people/irie/commit/556244e2c5ec189501390d45677eecfa70fef542))
* **local:** run big models on Irie's own llama-server, any llama.cpp build ([b50615d](https://github.com/irie-people/irie/commit/b50615de3a52c3b1640c65904f9ee7d628e62aa0))
* **local:** run the best quantized model that fits, and match names as Ollama does ([57ffba9](https://github.com/irie-people/irie/commit/57ffba99b2e6a26c6a70a2d5bcf99a4df4975650))
* **mac:** a file dropped on the conversation or the terminal goes into the agent's prompt ([ba5f13a](https://github.com/irie-people/irie/commit/ba5f13a5ae642bae3c1e672a0a0f1474d423e7f3))
* **mac:** a line ties the selected card to the terminal's frame ([6febb7b](https://github.com/irie-people/irie/commit/6febb7b290644209b854756be97e05b4ee5b8463))
* **mac:** a muted phrase bar under the terminal, cycling every 30 s ([deb35d7](https://github.com/irie-people/irie/commit/deb35d7a0c080da45e0e1d2a9f6fd92e54b17d66))
* **mac:** a new task starts behind the session on show, and unsent text survives a relaunch ([a6eb548](https://github.com/irie-people/irie/commit/a6eb548648a1c9d4270d8c090b4c2706603c7fe4))
* **mac:** a repo menu beside the task bar, and a new-task sheet on Cmd+N ([0a0dfde](https://github.com/irie-people/irie/commit/0a0dfdeff28b0216aa3da21f6966ec92553f8ba9))
* **mac:** a State | Repo switch, and the repo tree with helpers ([1facccf](https://github.com/irie-people/irie/commit/1facccf1f393cc7cf83a57eb2363b9ebfffea169))
* **mac:** a working agent takes its own colour, almost opposite the accent ([2ce59cf](https://github.com/irie-people/irie/commit/2ce59cf3875fd6441366fbb5e8d7bb484459ff40))
* **mac:** add Evergreen palette, soften the terminal focus edge ([aa88523](https://github.com/irie-people/irie/commit/aa8852301137296405cc5b428f147a98fbb9d6ac))
* **mac:** close sessions like tabs, and reopen them ([2b7e84a](https://github.com/irie-people/irie/commit/2b7e84ab0d736ca25b9b74954cb71cca63dc061b))
* **mac:** Ghostty draws the terminals, and its split keys work in the split ([2a030ea](https://github.com/irie-people/irie/commit/2a030ea5f2d00a03497d37f3db95a4194e4217c9))
* **mac:** give Irie.app its icon, drawn by tools/mkicon ([60d0cd0](https://github.com/irie-people/irie/commit/60d0cd075f2731c12a0a03a8f8cddf8c0e3c1749))
* **mac:** one row above the sessions instead of two ([0ad3bec](https://github.com/irie-people/irie/commit/0ad3beca1e8949af2d2a15ba28895f2dc9009cb1))
* **mac:** pause, resume and promote a session, and drag to reorder ([8826a2c](https://github.com/irie-people/irie/commit/8826a2ca2978ac4995f941a686600c3bcbc2be63))
* **mac:** review dock lists auto-closed sessions ready to review ([821f874](https://github.com/irie-people/irie/commit/821f8744de0cd42cbb2a7871856dfcdb3b49d878))
* **mac:** show the core version, muted, in the bottom right corner ([b9a47c5](https://github.com/irie-people/irie/commit/b9a47c5ed7648087dd5bce7167dee8253dbfd125))
* **mac:** the conversation view ([286dd3f](https://github.com/irie-people/irie/commit/286dd3fd35d53b47272911127aa79e32f79c5590))
* **mac:** the conversation view follows the palette and shows without a terminal ([aef6282](https://github.com/irie-people/irie/commit/aef62821a580ac48b73118f85e99a22b848e1fb6))
* **mac:** the details panel opens small, drags taller and minimizes to a line ([85802f1](https://github.com/irie-people/irie/commit/85802f167a2b1cf1dd39b6e1a84dd2dbffa36207))
* **mac:** the phrase bar cycles the owner's own phrases ([83d6dde](https://github.com/irie-people/irie/commit/83d6dde04d39d1a7813be688dd13f544f302b77f))
* **mac:** the sun and the name back in the title bar, beside the counts ([c38a8ec](https://github.com/irie-people/irie/commit/c38a8ecdcf36520612f73a23a22ea1ff02852c74))
* **mac:** three palettes, Rosewater by default, and DESIGN §5.3's marks ([4776cd5](https://github.com/irie-people/irie/commit/4776cd564d9f94c07543f23a97cb9492836ca29e))
* **mac:** up arrow in the task bar recalls earlier tasks ([1ca80e1](https://github.com/irie-people/irie/commit/1ca80e18205a4831c297cec0a32bb627ed113ce8))
* **mac:** up arrow in the task bar recalls earlier tasks ([f76dc40](https://github.com/irie-people/irie/commit/f76dc4050676c51266d56f75676a29a829e5d49a))
* **mac:** wash a working card in its agent's colour ([6fcd88b](https://github.com/irie-people/irie/commit/6fcd88b3dd50760bbef974e3b813c51381a0f78e))
* no Done group or count under close_when_done; ctl history and ctl reopen ([ea51eda](https://github.com/irie-people/irie/commit/ea51edad42bf23f8b7ca33ee2c2194e1dbbf77da))
* park sessions, in a list where only you move a card ([d7a2406](https://github.com/irie-people/irie/commit/d7a240661350998595fcb4559cf19fa3364c2e5a))
* parking puts a session out of sight and out of To verify ([daac737](https://github.com/irie-people/irie/commit/daac7374d7efea3afc02a9f7a0c34c1da35b5354))
* rename a session, and keep its name through restarts ([44c8973](https://github.com/irie-people/irie/commit/44c8973dca0968504ec050f34b4f8e8f6adb6b63))
* **statusline:** lead with the session's state and its helpers ([d7cba6c](https://github.com/irie-people/irie/commit/d7cba6c7bd9d8616c064ba40755822a5a11d55bc))
* teammates in terminals beside their session, as cmux splits them ([d30fbe0](https://github.com/irie-people/irie/commit/d30fbe0a22269c273642979d29b2ff919bad97eb))
* the conversation in the terminal slot, and a key to flip to the raw terminal ([0fc5a06](https://github.com/irie-people/irie/commit/0fc5a063e705c8f3f4436187ee66599b5b2e6f95))
* the Mac app draws done in red when ui.red_on_done is set ([c9544a4](https://github.com/irie-people/irie/commit/c9544a44aa482cd5a566898b815b47c6aaeb5db0))
* **ui:** cards, a repo tree and the mouse in the sidebar ([0a1b618](https://github.com/irie-people/irie/commit/0a1b61892e41a679c594f6315b34208cc922fbac))
* **ui:** hows X in the task bar shows that agent's status ([7e8195a](https://github.com/irie-people/irie/commit/7e8195a1740ba24568cb5c74763526bd28e6ab16))
* **ui:** Irie's palettes in the sidebar, and DESIGN §5.3 exactly ([88f9f07](https://github.com/irie-people/irie/commit/88f9f072e692c155112396982e3e6fed58821804))
* **ui:** u and U undo and redo, with the labels in the header ([694f71a](https://github.com/irie-people/irie/commit/694f71af5b8aa1a7414fa18dc03016a97d290876))
* **ui:** x x and :close close a session, and :reopen brings it back ([76613eb](https://github.com/irie-people/irie/commit/76613eb1e7058071c42fe500f5cae84ce3244e76))


### Bug Fixes

* **core:** a first core with the view starts on the session in the slot ([f32ddf6](https://github.com/irie-people/irie/commit/f32ddf6ecee6a9e6373c5f50fe962f7066641237))
* **core:** a session you close is done, and nothing brings it back ([187b72c](https://github.com/irie-people/irie/commit/187b72caa825bd908df3de355497123767749203))
* **core:** close_when_done leaves a session the user paused ([8fed164](https://github.com/irie-people/irie/commit/8fed1648261c4658820cf0a3d80c2b45c2ce4cbe))
* **core:** continue a frozen session before closing it ([5502fdd](https://github.com/irie-people/irie/commit/5502fdd3efbc00990f33330cf0af5123b068332b))
* **core:** keep a reopened session resumable before it shows life ([842048e](https://github.com/irie-people/irie/commit/842048ec2a73f009516e1ada10e78cf88f458216))
* **core:** never SIGKILL a pid that a closed host gave up ([0fd7e56](https://github.com/irie-people/irie/commit/0fd7e56efef77bdd54f6f053e01f8cf8fb070110))
* **live:** keep the core running when only Go tests changed ([6edab26](https://github.com/irie-people/irie/commit/6edab26ef5160e4a00a70b441eebc64759103ba8))
* **live:** the lock holds its install's pid, so a bare rmdir cannot take it ([3ce7455](https://github.com/irie-people/irie/commit/3ce7455df04522b7e91b031912d6e3b55fa854a6))
* **mac:** a card parked from under the pointer stays put, so a second click unparks it ([6ca5762](https://github.com/irie-people/irie/commit/6ca576271c5a206aa70fd56b54d2f0b4763811aa))
* **mac:** a selected card runs from the selection into its agent's colour ([852a56f](https://github.com/irie-people/irie/commit/852a56fae7afd10ddc5f0c2db93e832715afa2e8))
* **mac:** a selected working card keeps its agent's colour ([0f87e32](https://github.com/irie-people/irie/commit/0f87e321e0066de6f92ac563456125bc08e97b78))
* **mac:** bring back ⌘W, ⇧⌘W and ⇧⌘T ([d2e0b04](https://github.com/irie-people/irie/commit/d2e0b0424c303aec4e40accd71b626d93cdeb5ff))
* **mac:** Ctrl+Tab lands on the highlighted session ([3242a40](https://github.com/irie-people/irie/commit/3242a401e9d316879034e51d0396c44a6de7752e))
* **mac:** hold the hows notice open on the say() sleep ([aa64cf1](https://github.com/irie-people/irie/commit/aa64cf17effa935148299caccc2dc8bc9769055b))
* **mac:** Irie's sun and name back over the sidebar, above the find field ([9cf1e4f](https://github.com/irie-people/irie/commit/9cf1e4fb0c7b30649adc2c534e945c41051c9b96))
* **mac:** one inset for cards and fields, so their text lines up ([78479a0](https://github.com/irie-people/irie/commit/78479a022fdef63e8ff367fbf78f05f24997c42c))
* **mac:** show the session's name once in the right pane's header ([bb0b219](https://github.com/irie-people/irie/commit/bb0b219f197a09278b2c3dec38dad3a1d47926bc))
* **mac:** the details panel's buttons as icons, so they never get cut short ([d55fcca](https://github.com/irie-people/irie/commit/d55fccad06683b78f8a16c984b52103421e2caf3))
* **mac:** the right pane's title centres on the toolbar's midline ([0dc390a](https://github.com/irie-people/irie/commit/0dc390ad3e82de947948c292d74864c99c3e145f))
* **tools:** live.sh tells agents apart by pane and process, not by name ([7a76ddd](https://github.com/irie-people/irie/commit/7a76ddd949f342361c30dc8a2f310c94edd30ef4))


### Performance Improvements

* **mac:** SwiftTerm compiles once per release and compiler, shared by every worktree ([4c820ca](https://github.com/irie-people/irie/commit/4c820ca259ccf82959fe3139f952987e2c358fd2))
