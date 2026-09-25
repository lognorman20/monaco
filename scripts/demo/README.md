# Shooting the demo

The film is `docs/demo/storyboard.md`. This folder records and cuts it.

## Setup, once

1. A simulator to shoot on (iPhone 17 Pro, iOS 26). Keep it for the film; every clip is shot on the same device at the same status bar.
2. The backend in demo mode, so three accounts can move fake money on camera:
   ```bash
   DEMO_MODE=1 CHART_SOURCE=yahoo API_ADDR=127.0.0.1:8082 just run backend
   ```
   Every account starts with $1,000. Balances reset when the backend restarts, so shoot the money beats in one session.
3. The app built for the simulator (`just build mobile`), then `export MONACO_SIM_UDID=<udid>` and `export MONACO_APP=<path to Monaco.app>` for the first clip so the build is installed.
4. Sign each account in once and give it its name in onboarding (Maya, Jordan, Priya; see the storyboard). Sign out from Profile to switch; the film cuts between phones, so one simulator is enough.

## A clip

```bash
scripts/demo/record-clip.sh start-cabal 14
```

launches the app fresh against the demo backend and records for 14 seconds. Drive the flow during those seconds, by hand on the simulator or with an agent through the accessibility tree (`scripts/demo/clips.json` lists every step by its accessibility identifier). Start still, end still. If anything goes wrong, record again; a take is cheap.

The order matters, because later beats need the state earlier ones create: sign-in, start the cabal, join twice, fund, then the stocks and the proposal, then the vote, chat, pre-IPO, the bot, cash out, the boards. The storyboard has the account per beat.

## The cut

```bash
scripts/demo/compose.sh
```

writes `docs/demo/out/monaco-demo-vertical.mp4` and `monaco-demo-site.mp4`. Missing clips are skipped, so the cut can be watched while the shoot is half done. A music bed at `docs/demo/music.m4a` is mixed in when present.

## Faces

Nobody uploads a photo: a member without one is a pixel animal, the same one everywhere. That is the film's cast.
