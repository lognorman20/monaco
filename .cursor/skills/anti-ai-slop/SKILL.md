---
name: anti-ai-slop
description: >-
  Hard banlist + agent workflow against vibe-coded AI slop in websites, apps,
  landing pages, marketing copy, and UI microcopy. Merges community design
  skills (Hallmark, Taste-Skill, Anthropic frontend-design, SwiftUI anti-slop,
  ui-ux-pro-max). Use whenever generating or reviewing UI, CSS, SwiftUI, React,
  landing pages, brand sites, empty states, onboarding, or product marketing.
  Triggers on: design, UI, landing page, website, hero, SaaS page, marketing
  site, Tailwind, Framer, vibe code, make it pretty, polish the UI, brand
  refresh, anti-slop, design taste.
---

# Anti-AI-Slop Design Doctrine

You are not here to ship the median of Dribbble + Tailwind UI + 2023 SaaS Twitter.
You are here to make work that looks like a human with taste made decisions.

This file is the result of designer Twitter, indie-hacker dunk threads, HN pile-ons,
article after article about purple-gradient monoculture, and the best public agent
skills on GitHub (what agents should do + what they must never do). Follow it like law.

**If a choice is the statistical average of GitHub tutorials 2019–2024, refuse it.**

---

## 0. Instant fail (abort and redesign)

Ship any of these → rewrite before user sees it:

| Fail | Why it screams slop |
| --- | --- |
| Purple / indigo / violet / blue-purple gradients | Tailwind `indigo-500` + AI product branding saturated training data (Adam Wathan apology era) |
| Soft box shadows on every card (`0 4px 24px rgba(0,0,0,.08)` energy) | Default "depth" with no hierarchy |
| Emoji as icons, nav, section markers, or CTA decoration | Fountain Institute tell #3; lazy hierarchy |
| `🟢 LIVE` / pulsing green dots / vanity status pills | Status theater; Fountain tell #7 |
| Inter / Roboto / Open Sans / system-ui as the whole brand | Default font of every AI builder |
| Glassmorphism + glow + aurora blobs behind hero | Lazy "premium dark mode" |
| 3 identical icon cards in a row ("Feature / Feature / Feature") | The SaaS template |
| Centered hero + vague H1 + dual CTA ("Get started" / "Learn more") | Unprompted median landing page |
| Fake logo wall ("Trusted by…") with grayscale FAANG marks | Borrowed credibility |
| Fake testimonials with stock faces | Instant distrust |
| Neon-on-dark with 5+ competing accents | Fountain tell #1 |
| Rainbow left-border accent on every block | Fountain tell #6 ("em-dash of AI UI") |
| Nested cards in cards ("cardocalypse") | Paul Bakaus |
| Lucide/Heroicons at 48px over every H2 | Icon cosplay |
| Mesh / spline / floating 3D abstract blob as the product | No product, only vibes |
| Bento grid used as personality | Layout costume |
| Dark mode + cyan/purple glow as default brand | Crypto/AI aesthetic wash |
| Side-stripe cards (thick left border accent) | Hallmark / 2018 SaaS-AI tell |
| Full-viewport centered hero (`100vh` + one sentence + one CTA) | Default LLM landing page |
| Pure `#000` / pure `#FFF` as whole surfaces | Flat synthetic (taste-skill / Cuuper22) |
| Italic display headers / italic emphasis word in H1 | Hallmark typography purity tell |
| Fake browser chrome (traffic lights + URL pill) | Hallmark re-drawn chrome ban |
| Cream + serif + terracotta "editorial" as unchosen default | Anthropic 2026 cluster #1 |
| Near-black + acid green/vermilion as unchosen default | Anthropic 2026 cluster #2 |
| Broadsheet hairline / zero-radius newspaper cosplay as unchosen default | Anthropic 2026 cluster #3 |

**Brand test:** Strip the nav. Could this first viewport belong to any other startup?
If yes → fail.

---

## 1. Color (religious)

- **Ban:** blue→purple, purple→pink, indigo→violet gradients on heroes, buttons, text, borders.
- **Ban:** `#6366F1`, `#8B5CF6`, `#A855F7`, `#7C3AED`, and Tailwind `indigo-*` / `violet-*` / `purple-*` as primary brand.
- **Ban:** neon cyan/magenta accents on charcoal as the whole system.
- **Ban:** pure `#000` text on pure `#FFF` with no temperature (flat AI sterile).
- **Do:** pick ONE decisive accent with a reason (industry, product, place, era). Write hex tokens.
- **Do:** prefer odd, specific palettes (oxidized green, newsprint cream, kiln orange, hospital teal, night-rail navy) over "tech purple."
- **Do:** flat or near-flat surfaces. If you need separation, use border, spacing, or type — not glow.

Gradients allowed only when: brand already owns a specific non-purple gradient AND it is not the hero's only idea.

---

## 2. Shadows, depth, effects

- **No box shadows** as decoration. Period for marketing surfaces.
- App UI: shadow only for true elevation (modals, popovers) — single soft shadow, not stacked glow.
- **No** `backdrop-filter: blur` glass stacks.
- **No** text-shadow glow, neon outlines, outer glow rings on buttons.
- **No** skeuomorphic "premium" metallic gradients on CTAs.
- Borders: 1px hairline or none. Not 2–3px colored "AI card frames."

---

## 3. Typography

- **Do not default to Inter, Roboto, Arial, system-ui, Space Grotesk, Plus Jakarta Sans, DM Sans** as the personality.
- Pair with intent: one display (serif / grotesque / mono with character) + one readable body.
- Real hierarchy: size, weight, tracking, measure — not "make H1 huge and add a gradient fill."
- **Ban** gradient-filled headlines.
- **Ban** title-case marketing H2s that sound like LinkedIn (`Unlock Your Potential Today`).
- Line length ~45–75ch. Stop the ultra-wide hero paragraphs.
- Avoid "Japanese-style UX writing" redundancy (every label restated three ways).

---

## 4. Layout & composition

### First viewport budget (marketing)
Only: brand mark, one headline, one short supporting line, one CTA group, one dominant real visual.
**Do not** stuff first viewport with: stats strips, logo walls, pill clusters, "as seen in", schedule widgets, floating badges, social proof chips.

### Hard layout bans
- No dashboard cosplay on a marketing page
- No 3× feature icon grid as the product story
- No inset rounded media card floating in whitespace as the "hero image"
- No side-panel hero + gradient slab
- No tiled collage of AI illustrations
- No decorative bento for its own sake
- No sticky "announcement bar" with confetti copy
- No modal to hide unfinished IA
- Full-bleed product/place imagery beats abstract gradient planes

### Cards
Default: **no cards**.
Cards only when they are the interaction container (price plan, selectable item, feed object).
If removing border/shadow/radius does not hurt understanding → remove them.

### Sections
One job per section. One headline. One short support line. Then content.
Stop the "eyebrow + H2 + paragraph + 3 cards + CTA" automaton.

---

## 5. Components that are guilty until proven innocent

| Component | Verdict |
| --- | --- |
| Pill chips / rounded-full tags as decoration | Ban unless filters/tags need them |
| `LIVE` / `NEW` / `AI` / `BETA` badges for flair | Ban unless real state |
| Green online dots | Only for real presence |
| Avatar stacks of fake users | Ban |
| Metric counters animating to fake numbers | Ban |
| Progress bars to nowhere | Ban |
| Testimonial carousel with LinkedIn-style quotes | Usually ban |
| Pricing "Most popular" ribbon in brand purple | Ban the purple; ribbon OK if earned |
| Cookie-cutter FAQ accordion with AI answers | Rewrite answers |
| Chat widget bubble with sparkles | Ban sparkles |

---

## 6. Motion

Motion = hierarchy and feedback. Not atmosphere generators.

**Ban:**
- Infinite float / bob on hero art
- Parallax that moves more than the product
- Scroll-jacked storytelling for a 2-section landing page
- Elastic spring on every button
- Staggered fade-ins on every paragraph (template theater)
- Confetti on signup by default
- Cursor-following gradient blobs

**Allow:**
- 150–250ms opacity/transform on state change
- One signature transition that matches the brand
- Respect `prefers-reduced-motion`

Ship 2–3 intentional motions max on a marketing page. Not 12.

---

## 7. Imagery

- Prefer real product screenshots, real places, real materials, real people (with rights).
- **Ban** Midjourney "3D clay people in purple void"
- **Ban** abstract plexus / neural net / glowing brain
- **Ban** Lottie of rocket → chart → handshake
- **Ban** unexamined stock that could sell CRM or toothpaste
- Alt text required. Default favicon banned.

---

## 8. Copy & microcopy (same religion)

Write like a specific person. Concrete nouns. Verbs that do work.
If the sentence works for every competitor after a find-replace of the name → rewrite.

### Sentence patterns — delete on sight
- "It's not X — it's Y."
- "No X. No Y. Just Z."
- "In today's fast-paced / digital / ever-evolving…"
- "Welcome to the future of…"
- "Unlock / Unleash / Supercharge / Elevate…"
- "The best part? … The kicker? …"
- "Whether you're a freelancer, a team, or an enterprise…"
- "We're more than just a [noun]."
- "Powered by AI" as a headline
- "Seamless / robust / cutting-edge / best-in-class …"
- "Let me explain" / "Let's dive in" / "Without further ado"
- Excessive em-dash drama in every sentence
- Fake cheer: "Absolutely!" "Great question!" "I'd be happy to help!"

### UI microcopy bans
- "Get started" when "Create account" / "Start trial" / "Add project" is clearer
- "Learn more" as the only secondary CTA (say the destination)
- "Oops! Something went wrong." with no next step
- "Welcome aboard!" empty states
- "Magic awaits" / "Your journey begins"
- Button labels with emoji
- Tooltip novels

### Voice rules
- Short sentences OK. Fragments OK. Specificity mandatory.
- Numbers beat adjectives. "Exports CSV in <2s" beats "blazingly fast exports."
- No hedge pileups ("might possibly help somewhat").
- Sentence case for headings unless brand guide says otherwise.

---

## 9. Banned vocabulary (645 terms)

Never use these in user-facing copy, marketing, onboarding, empty states, or comments meant as product voice.
Derivatives count (`unlocking`, `seamlessly`, `revolutionize`).

If you need the meaning, pick a boring concrete word.

```
10x roi, 10x your, 360 view, 360-degree, A gateway to
a new way to, a shift in thinking, abstract blobs, Actionable insights, actionable intelligence
ah-ha moment, aha loops, aha moment, ai copilot, ai magic
ai sidekick, ai-first, ai-generated testimonial, ai-native, AI-powered
align the stars, all-in-one, always-on, an entirely new way, and so much more
are you ready to, As a matter of fact, as an ai, as of my last knowledge update, as we delve deeper
as we navigate, at scale, at the end of the day, At the forefront of, at your fingertips
aurora background, award-winning, bank-grade security, battle-tested, Bearing in mind that
beautifully crafted, beautifully simple, begin your journey, bespoke solutions, Best practices
Best-in-class, best-in-class features, bird's eye view, birds-eye view, blazingly fast
Bleeding edge, bleeding-edge, blue sky, Blue-sky thinking, boil it down
boil the ocean, brace yourself, bridging the gap, broader landscape, build the future
built different, built for builders, built for the bold, built to empower, built to last
built to perform, built to scale, buttery smooth, buttery ui, Capitalize on the opportunities
carefully crafted, category defining, changing landscape, changing the way, chat-based interface
Circle back, clean and modern, cloud-native, competitive landscape, competitive moat
complex landscape, comprehensive solution, conversational ai, conversational ui, copilot for
core competencies, Core competency, crafted to perfection, create lasting impact, crush it
customer 360, Customer journey, Customer-centric, cutting edge technology, Cutting-edge
Data-driven, days not weeks, deceptively simple, decisions compound, Deep dive
deep-dive, deeply integrated, delightful experience, delightful ux, delve into
Delve into the world of, designed to empower, designed with you in mind, digital landscape, Digital transformation
digital twin, Disruptive innovation, Disruptive technology, dive deep, Dive into
diverse tapestry, do the work, double-click, Drill down, drink our own champagne
drink the kool-aid, drive growth, drive impact, drive meaningful impact, drive results
drive value, Due to the fact that, dynamic landscape, earn attention, earn trust
eat our own dogfood, elegantly simple, embark on, Embark on a journey, empowering teams to
empowering you to, End-to-end, end-to-end solution, engineered to perfection, enterprise-grade
enterprise-ready, eureka moment, Ever-evolving, evolving landscape, fake testimonial
feels like magic, first of its kind, first-class, floating orbs, flywheel effect
for teams of all sizes, Foster a culture of, Foster the development, from startups to enterprises, from zero to
Future-proof, future-ready, Game changer, game-changer, game-changing solution
Given the fact that, glassmorphism ui, glowing orbs, go live in minutes, going forward
gpt-powered, gradient mesh, great question, Groundbreaking way, growth hack
Growth hacking, growth loops, guaranteed roi, Harness the power of, have you ever wondered
helicopter view, helping teams unlock, helping you unlock, herding cats, here to revolutionize
here's the thing, hero metric, hero's journey, High-impact, highly acclaimed
highly scalable, hold space, hold the line, Holistic approach, holistic solution
holistic view, hours not days, human-centric, hyper-personalized, i'd be happy to
imagine if, in a sea of sameness, in a world of, in an ever-evolving, In conclusion
in essence, In light of the fact that, In real-time, In summary, in the ever-evolving landscape
in the realm of, in today's digital age, in today's fast-paced world, In today’s digital age, In today’s digital era
incredibly powerful, indelible mark, industry-defining, industry-leading, Innovative product
Innovative solutions, instant value, intelligent automation, It is advisable, It is crucial to understand
It is essential to consider, It is important to know, It is worth noting, it just works, it's important to note
it's not about, jaw-dropping, join a community of, join the revolution, join thousands
just works, Key insights, Key stakeholders, Key takeaway, key takeaways
Lay the groundwork for, Leading edge, leading-edge, lean in, let that sink in
let's delve, let's delve in, let's dive in, let's uncover, Let’s delve in
Let’s delve into the exciting details, level up, level-set, Leverage resources, lightbulb moment
lightning fast, like a moth to a flame, little did they know, llm-powered, logo cloud
logo wall, lorem testimonial, loved by thousands, lovingly built, lovingly crafted
low hanging fruit, Low-hanging fruit, luxurious feel, machine-first, make an impact
make no mistake, meaningful results, measurable outcomes, mesh gradient hero, meticulously crafted
meticulously designed, meticulously engineered, metrics that matter, micro-interactions galore, military-grade
mind-blowing, minimal yet powerful, minimize friction, minutes not hours, Mission-critical
ml-powered, modern and clean, move the goalposts, Move the needle, natively integrated
Navigate the complexities, Navigate the landscape, needless to say, net it out, network effect
neural network viz, never look back, never seen before, new era, new landscape
next generation, Next-gen, next-gen platform, next-level, next-level security
night and day, no learning curve, no x no y just z, north star, north star metric
north-star, on the horizon, onboarding journey, one click, one metric that matters
One-stop shop, open the kimono, open-ended, orders of magnitude, our mission is to
out-of-the-box, Out-of-the-box thinking, pain point, Pain points, Paradigm shift
paradigm-shifting, Pave the way for, peace of mind, peace-of-mind security, peel the onion
people-first, perfect for everyone, Personalized experience, picture this, pixel-perfect
plexus background, plug-and-play, poised for growth, poised to, poised to disrupt
polished experience, powered by ai, powerfully simple, premium experience, premium feel
prepare to be amazed, Proactive approach, Product innovation, production-ready, proven methodology
proven results, Push the boundaries of, pushing the envelope, Quick wins, quietly building
quietly dominating, quietly transforming, raise the bar, read that again, ready to elevate
ready to revolutionize, ready to transform, ready to unlock, real growth, real impact
real value, redefining the way, reimagining how, reinventing how, Remarkable breakthrough
remarkably simple, Remember that, rest assured, results-driven, retention loops
retention magic, rethinking how, revolutionary approach, rich and diverse tapestry, rich tapestry
robust framework, Robust infrastructure, robust platform, robust solution, say less
scalable platform, scalable solution, scratches the surface, Seamless experience, Seamless integration
seamless workflow, seamlessly integrated, second to none, secret sauce, secret weapon for
send the signal, set-and-forget, shapes the future, shed light, shifting landscape
Sights unseen, silky animations, simple yet powerful, simply powerful, single source of truth
smart automation, Smart goals, so you can focus on, so you can get back to, so you never have to
social proof bar, something bigger, source of truth, Spearhead the initiative, spline 3d
start your journey, State-of-the-art, Strategic alignment, Strategic goals, Streamline processes
surgical precision, surprise and delight, surprisingly easy, system of engagement, system of intelligence
system of record, take a dive into, Take it to the next level, take your x to the next level, tangible results
that's not x that's y, the best part, the better way to, the elephant in the room, the future of
the kicker, the list goes on, the message lands, the modern way to, the only tool you need
the point lands, the smart way to, the world of, think outside the box, this matters because
Thought leadership, thoughtfully designed, tightly integrated, time to value, time-to-value
tip of the iceberg, Touch base, transformative experience, treasure trove, trusted by industry leaders
trusted by thousands, trusted worldwide, tuned to perfection, turnkey solution, unfair advantage
unified view, uniquely positioned, uniquely powerful, Unleash the power of, Unleashing the potential
unlike anything you've seen, Unlock the potential of, unlock the secrets, unlock value, unlock your potential
Unlocking the power, untapped potential, unveil the secrets, unveiling the power, up and running in
used by millions, User journey, user-centric, User-friendly, value add
Value-add, vanity metrics, vast landscape, vibe-coded, Viral content
viral loops, we're on a mission, we've got you covered, we've got your back, welcome to the future
what matters here, whether you're a, white glove, white space, white-glove
whitespace opportunity, why it matters, widely regarded, without further ado, works like magic
world of difference, world-class, world-class support, wow moment, you won't believe
your ai assistant, your unfair advantage, zero friction, zero learning curve, zero to one
accentuate, actionable, adaptive, amidst, Arguably
artisanal, beacon, bespoke, Bustling, catalyze
claymorphism, cognizant, commence, commendable, Complexities
confluence, Consequently, Daunting, dazzle, delve
democratize, demystify, disruptive, dogfooding, ecosystem
eerie, Effortless, elevate, embark, embodiment
empower, encompass, endeavor, endeavour, Enigma
enlighten, entanglement, ephemeral, esteemed, ethereal
Everchanging, facilitate, Foster, frictionless, Furthermore
gamechanging, garner, glassmorphic, Gossamer, grappling
groundbreaking, handcrafted, harness, hitherto, holistic
hurdles, ideate, ideation, ignite, immersive
Indelible, innovative, Insightful, insurmountable, Intricate
intuitive, journey, Labyrinth, Landscape, learnings
Leverage, Metamorphosis, Meticulous, meticulously, Moreover
mosaic, multifaced, multifaceted, myriad, nestled
neumorphic, neumorphism, nuance, nuanced, omnichannel
optimize, orchestrate, paradigm, paramount, pioneering
Pivotal, plethora, predictive, proactive, profound
proprietary, quintessential, Realm, reimagine, Remnant
resonate, revolutionary, Robust, Seamless, seamlessly
showcasing, skyrocket, spearhead, streamline, Subsequently
supercharge, surpass, Symphony, synergistic, synergize
Synergy, tailored, Tapestry, testament, trailblazing
Transformative, turbocharge, turnkey, underpinning, underscore
unleash, unlock, unmatched, unparalleled, unprecedented
unrivaled, utilize, versatile, vibe, vibecoding
vibes, Vibrant, visionary, weighing, Whispering
```

**Count: 645 banned terms/phrases.**


---

## 10. Do this instead (anti-slop positives)

1. **Name a direction in one sentence** before coding: "1970s ski-lodge booking / newsprint editorial / hardware catalog / late-night radio."
2. **Show the product early.** Real UI > metaphor art.
3. **One accent color. One type story. One layout idea.**
4. **Write the headline like a billboard**, not a TED talk.
5. **Steal structure from tasteful references** (Stripe clarity, Craigslist honesty, Bloomberg density, independent magazines) — not from v0 sample apps.
6. **Whitespace is a material.** Don't fill it with cards.
7. **Ship asymmetry or restraint** — both beat template symmetry.
8. **Accessibility is not optional:** contrast, labels, focus rings, heading order, hit targets.
9. **Mobile is a first composition**, not a squeezed desktop.
10. **Edit after generation.** AI drafts. You art-direct.

### Brand-first check (landing)
- Brand/product name is hero-level, not a nav whisper
- Headline does not overpower the brand
- First viewport is one composition, not a dashboard

---

## 11. Agent self-check (run before finishing)

```
[ ] No purple/indigo/violet gradient system
[ ] No decorative box shadows
[ ] No emoji in UI chrome
[ ] No LIVE/NEW/AI vanity badges
[ ] No Inter/Roboto-as-brand default
[ ] No glassmorphism / aurora glow hero
[ ] No 3-icon feature grid as the story
[ ] No fake logos / fake quotes
[ ] No banned words from §9
[ ] No "It's not X, it's Y" copy
[ ] First viewport passes brand test
[ ] Cards only where interaction needs them
[ ] Motion count ≤ 3 intentional moments
[ ] Real product visual present
[ ] Concrete headline (what it does + for whom)
[ ] §13 workflow done (intent / domain / signature / reject defaults)
[ ] §15 pre-emit gates A–D all pass
```

Any unchecked box → fix before presenting.

---

## 12. Prompt stub (paste into design tasks)

```
Design constraints (mandatory):
- Zero purple/indigo/blue-purple gradients
- Zero decorative box-shadows; borders/spacing for separation
- Zero emoji in UI
- Zero LIVE/pill vanity badges
- No Inter/Roboto/system as brand personality — pick distinctive fonts
- No glassmorphism, aurora blobs, neon glow, spline heroes
- No 3-column icon feature grid
- No cards unless interactive
- No AI vocabulary (delve, seamless, unlock, robust, cutting-edge, journey…)
- Headline must state concrete job-to-be-done
- Show real product UI, not abstract metaphor
- One composition first viewport; brand-first
Aesthetic direction: {SPECIFIC_DIRECTION}
Reference sites for taste (structure only): {REFS}
```

---

## 13. Agent workflow (do this — from community skills)

**Before any markup/code**, answer in writing (do not skip):

1. **Intent** — Who uses this? What job? What emotion after 3 seconds? (`joshuadavidthomas/agent-skills`)
2. **Domain** — Name the real-world reference (newsprint, clinic intake, ski lodge, hardware catalog). UI must inherit materials from that domain, not "SaaS." (`Cuuper22/anti-slop-design`, `anthropics/skills`)
3. **Color world** — One accent, max saturation <80%. Off-white base, not `#FFF`; dark surfaces not `#000`. Name tokens from domain (`kiln-orange`, `newsprint-cream`), not `primary-500`. (`Leonxlnx/taste-skill`, `Cuuper22/anti-slop-design`)
4. **Signature** — One memorable element only (type treatment, rule, crop, motion, material). If you cannot name it in one phrase, you have none. (`anthropics/skills`, `joshuadavidthomas/agent-skills`)
5. **Reject defaults** — List 3 things you will NOT do because every AI builder does them. Include at least one **2026 cluster** you are avoiding: cream+serif+terracotta editorial, black+acid minimal, broadsheet newspaper cosplay. (`anthropics/skills`)

**Two-pass token plan** (`anthropics/skills`, `Nutlope/hallmark`):

- **Pass 1 — Lock tokens:** type scale, spacing, radius, motion duration/easing, palette hex. Write them. Do not improvise mid-render.
- **Pass 2 — Compose:** build only from locked tokens. New color/size/radius mid-file = fail; stop and revise Pass 1.

**Structural variety** (`Nutlope/hallmark`):

- Do not ship the same skeleton (hero → 3 features → CTA) with palette swap.
- Vary section rhythm: full-bleed vs inset, single column vs split, list vs table vs editorial stack.
- When variance is high (many sections), **fight center-bias** — anchor content left or use asymmetric grids; do not default every block to `text-align: center`. (`Leonxlnx/taste-skill`)

**SwiftUI / native UI** (`wholiver/swiftui-design-skill`):

1. Start from **context** (platform, density, existing chrome) — do not paint over everything.
2. **Placeholder > bad art** — gray rects / SF Symbols until real assets exist.
3. Ship **variants** (compact/regular, light emphasis), not one "final" screen in a vacuum.
4. **System-first** — use platform type, spacing, materials; custom only where brand requires it.
5. Do not fill every pixel — negative space is composition.
6. Real product state beats decorative chrome.

**Copy discipline** (`Nutlope/hallmark`, gstack `design-review`):

- No invented metrics, user counts, awards, or "trusted by" unless user supplied facts.
- Run **delete 30%** pass: cut weakest adjectives and duplicate claims; keep concrete nouns.

**Mobile-first widths** — compose and sanity-check at **320, 375, 414, 768** px; do not design desktop-only then squeeze. (`Nutlope/hallmark`)

---

## 14. Extra hard bans (from GitHub skills)

| Ban | Source |
| --- | --- |
| **2026 default aesthetic clusters** as whole look: cream+serif+terracotta "editorial," black+acid neon minimal, faux broadsheet newspaper | `anthropics/skills` |
| **Fake browser chrome** — decorative URL bar, traffic-light window frame, "app inside a browser" mock unless showing real embed | `Nutlope/hallmark` |
| **Italic headers** as default personality / section titles | `Nutlope/hallmark` |
| **Mid-render token improvisation** — new hex, radius, or shadow invented during composition | `Nutlope/hallmark` |
| **Invented social proof** — metrics, logos, quotes, star counts, "join N users" without source data | `Nutlope/hallmark` |
| **Pure `#000` / pure `#FFF`** as full UI surfaces | `Cuuper22/anti-slop-design`, `Leonxlnx/taste-skill` |
| **More than one saturated accent** or accent with saturation ≥80% | `Leonxlnx/taste-skill` |
| **Custom cursors** (trail, blob, branded pointer) | `Leonxlnx/taste-skill` |
| **Oversized screaming H1** — headline scale without typographic system or measure control | `Leonxlnx/taste-skill` |
| **Placeholder-as-label** — `Lorem ipsum`, `Your text here`, `Product name`, `Feature 1` shipped as visible UI copy | gstack `design-review` |
| **Visited links indistinguishable** from default links | gstack `design-review` |
| **Emoji or bitmap icons** where SVG/system symbol suffices | `nextlevelbuilder/ui-ux-pro-max` |
| **Generic token names** (`primary`, `accent`, `brand-purple`) without domain meaning | `joshuadavidthomas/agent-skills` |
| **Domain-blind radius** — same `rounded-2xl` everywhere; radius must match domain | `Cuuper22/anti-slop-design` |
| **Domain-blind motion** — bounce/elastic on everything; motion must match domain restraint | `Cuuper22/anti-slop-design` |
| **Cards that do not earn existence** — static marketing blocks wrapped in card chrome | gstack `design-review` |
| **Space Grotesk / Plus Jakarta / DM Sans / Poppins as lazy "distinctive"** when used as every project's display face | `bytedance/deer-flow` frontend-design, Hallmark |
| **Heroicons-only icon religion** — vary or drop icons; editorial often needs none | `Cuuper22/anti-slop-design` |
| **Uniform 8px radius on everything** | `Cuuper22/anti-slop-design` |
| **AI-drawn SVG clipart / CSS silhouettes** as product art | `wholiver/swiftui-design-skill` |

---

## 15. Pre-emit gates (must pass)

Run **before** presenting UI or copy. Any fail → fix, do not ship.

### A. Six-axis critique (`Nutlope/hallmark`)

| Axis | Question |
| --- | --- |
| **Structure** | Is layout rhythm different from hero→3feat→CTA template? |
| **Tokens** | Are all colors/type/spacing/radius from locked Pass-1 set? |
| **Signature** | Is there exactly one deliberate memorable element? |
| **Domain** | Could a stranger name the reference world (not "startup")? |
| **Copy** | Zero invented stats, zero banned vocabulary, zero placeholder labels? |
| **Platform** | Native/system patterns respected; not every surface custom-skinned? |

### B. Four visual tests (`joshuadavidthomas/agent-skills`)

- **Swap test** — Swap headline with a competitor name. Still works? → headline too generic; rewrite.
- **Squint test** — Blur eyes. One clear hierarchy level? Or gray mush of same-weight boxes?
- **Signature test** — Cover logo. Still recognizable as *this* direction? If not → add signature, not more decoration.
- **Token test** — grep output for raw hex/radius outside token block → fail.

### C. Accessibility & touch (`nextlevelbuilder/ui-ux-pro-max`)

- [ ] Text contrast **≥4.5:1** (normal), **≥3:1** (large type)
- [ ] Interactive targets **≥44×44 pt/px** (or equivalent hit area)
- [ ] Visible **focus** states; logical **heading order**
- [ ] **`prefers-reduced-motion`** honored — no essential info only in animation

### D. Composition & copy (gstack `design-review`, `Cuuper22/anti-slop-design`)

- [ ] **Cards earn existence** — each card is interactive, selectable, or pricing/plan object; else remove chrome
- [ ] **Delete 30%** pass done on marketing copy
- [ ] **No placeholder-as-label** in visible strings
- [ ] **Visited link** style distinct from default links
- [ ] Checked layouts at **320 / 375 / 414 / 768** widths
- [ ] **Off-white** backgrounds, not clinical `#FFF` sheets
- [ ] **H1 scale** fits type system — not default `text-6xl` scream

### E. Agent self-check merge

All §11 boxes **plus** §15 A–D. Unchecked → fix before presenting.

### F. Meta gates (from research corpus)

- **Cluster rule:** one tell maybe OK; **four or more** Instant-fail / §14 tells on one screen = slop. Fix before ship. (Krebs Show HN audits, Booplex)
- **DESIGN.md lock:** if project has no design direction file, write one (palette ≤3 hues, font duo, radius, spacing, banned list, named direction) before broad UI codegen. "Clean modern SaaS" alone = blocked. (`unslop-preflight`, practitioner consensus)
- **8–12 signature breaks:** change type + layout + color + motion together — palette swap alone is still the same template.
- **P0 vs P1:** P0 = layperson spots AI (purple gradient, Inter-everywhere, LIVE pill, emoji icons). P1 = designer spots (uniform cards, fade-up-all, left-stripe). Clear all P0 first. (`avoid-ai-design`, tastecheck)
- **Fake urgency / community fluff copy:** ban act-now timers, "join thousands," "you're not alone," "join the revolution" unless user supplied real proof.

---

## 16. Attribution (skills mined)

Community skill repos synthesized for §13–§15 (high-signal rules only; overlaps with §0–§12 deduped):

| Repo | Contributed |
| --- | --- |
| [anthropics/skills](https://github.com/anthropics/skills) (`frontend-design`) | Bold direction, signature element, two-pass tokens, reject 2026 aesthetic clusters |
| [Nutlope/hallmark](https://github.com/Nutlope/hallmark) | Structural variety, 6-axis pre-emit critique, locked tokens, honest copy, fake browser chrome ban, italic-header ban, mobile width set |
| [Leonxlnx/taste-skill](https://github.com/Leonxlnx/taste-skill) (~64k★) | Max one accent sat<80%, no pure black, anti-center-bias, no custom cursors, no oversized H1 |
| [pbakaus/impeccable](https://github.com/pbakaus/impeccable) | Detector lineage (cardocalypse, Lucide-over-heading, gray-on-color); design vocabulary for agents |
| [joshuadavidthomas/agent-skills](https://github.com/joshuadavidthomas/agent-skills) | Intent Qs before code, domain/color-world/signature/reject-defaults, swap/squint/signature/token tests |
| [Cuuper22/anti-slop-design](https://github.com/Cuuper22/anti-slop-design) | Domain-first protocol, 15-rule checklist, off-white/not-#000, domain radius & motion |
| [wholiver/swiftui-design-skill](https://github.com/wholiver/swiftui-design-skill) | Context-first, placeholder>bad art, variants not one final, system-first, SF Symbols |
| [nextlevelbuilder/ui-ux-pro-max-skill](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill) | a11y priority, 44×44 touch, SVG over emoji, contrast floors |
| [garrytan/gstack](https://github.com/garrytan/gstack) (`design-review`) | Cards earn existence, delete-30% copy, no placeholder-as-label, visited-link distinction |
| [bytedance/deer-flow](https://github.com/bytedance/deer-flow) (`frontend-design`) | Never converge on Space Grotesk / same fonts across generations; bold tone commit |

*Research corpus (Tavily + Composer):* Krebs 16-pattern / Show HN audits, Sailop encyclopedia, Fountain Institute, Hugo Garcez vibecoded-design-tells, Kobak excess-vocabulary, Wikipedia Signs of AI writing.

*Also surveyed:* `binjuhor/shadcn-lar` anti-slop rules, Microsoft `frontend-design-review`, `jalaalrd/anti-ai-slop-writing`, `educlopez/ui-craft`, `h3nryprod01/design-taste` — patterns folded where non-duplicative.


## Sources (research trail)

Discourse and writeups used to build this banlist (2024–2026):

- [Why Your AI Keeps Building the Same Purple Gradient Website](https://prg.sh/ramblings/Why-Your-AI-Keeps-Building-the-Same-Purple-Gradient-Website) — Tailwind indigo → LLM median
- [AI Slop Web Design Guide (925studios)](https://www.925studios.co/blog/ai-slop-web-design-guide) — Inter + purple-blue + card monoculture
- [7 Tells that a UI is AI-Generated (Fountain Institute / Jeff Humble)](https://pages.thefountaininstitute.com/posts/7-tells-that-a-ui-is-ai-generated) — neon, glow, emoji, purple, cardocalypse, rainbow tabs, status dots
- Paul Bakaus — AI slop design tells (cardocalypse, Lucide-over-headings, glass/glow lazy cool)
- [@itsolelehmann on X](https://x.com/itsolelehmann/status/2037215657649983917) — purple + Inter + same hero, every time
- [I Analyzed 100 Vibe-Coded Websites (DEV)](https://dev.to/kaplich/i-analyzed-100-vibe-coded-websites-and-found-these-common-mistakes-5275) — OG, alt, heading hierarchy, favicon rot
- [Ban. — How to AI / Ruben](https://ruben.substack.com/p/delve) — delve/tapestry/realm library
- [Jodie Cook ban list](https://www.jodiecook.com/ban-list) — academic sludge + AI giveaway phrases
- [500 ChatGPT Overused Words (God of Prompt)](https://godofprompt.ai/blog/500-chatgpt-overused-words-heres-how-to-avoid-them)
- [300+ AI Words to Avoid (ContentBeta)](https://www.contentbeta.com/blog/list-of-words-overused-by-ai)
- [Most Common ChatGPT Words 2026 (Walter Writes)](https://walterwrites.ai/most-common-chatgpt-words-to-avoid)
- Pattern: "It's not X — it's Y" / em-dash addiction called out across writing Twitter

- GitHub skill corpus (Hallmark, Taste-Skill, Anthropic frontend-design, Impeccable, SwiftUI anti-slop, ui-ux-pro-max, gstack design-review, Cuuper22) — see §16

This doctrine will drift as models change. When a new default aesthetic appears (new font, new gradient, new layout meme), add it to §0.

---

*For indie hackers who ship daily and still want to look like they meant it.*
