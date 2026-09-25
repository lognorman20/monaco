# Shooting the demo

The film is `docs/demo/storyboard.md`. This folder records and cuts it.

## Setup, once

1. A simulator to shoot on (iPhone 17 Pro, iOS 26). Keep it for the film; every take is shot on the same device at the same status bar.
2. The backend in demo mode, so the accounts can move fake money on camera:
   ```bash
   DEMO_MODE=1 CHART_SOURCE=yahoo API_ADDR=127.0.0.1:8082 just run backend
   ```
   Every account starts with $1,000. Balances reset when the backend restarts, so shoot the money beats in one session.
3. The app built and installed on that simulator, launched with `MONACO_API_BASE_URL=http://127.0.0.1:8082` (`scripts/ios-sim` passes it through).
4. Sign each account in once and give it its name in onboarding (Maya, Jordan, Priya; see the storyboard). Sign out from Profile to switch; the film cuts between phones, so one simulator is enough.

## A take

```bash
export MONACO_SIM_UDID=<udid>
scripts/demo/record-start.sh start-cabal
# drive the flow, by hand or with an agent
scripts/demo/record-stop.sh
```

writes `docs/demo/clips/start-cabal.mov`. `record-clip.sh <name> <seconds>` does the same on a timer, relaunching the app first. Start still and end still; the cut trims the dead air. If anything goes wrong, record again.

Shoot in the storyboard's order, because later beats need the state earlier ones create.

## The cut

```bash
scripts/demo/compose.sh            # both masters
scripts/demo/compose.sh stocks     # one beat, no cards, to check it
MONACO_DEMO_REUSE=1 scripts/demo/compose.sh   # keep beats already rendered; only the join and cards redo
```

writes `docs/demo/out/monaco-demo-vertical.mp4` (9:16) and `monaco-demo-site.mp4` (16:9). Missing takes are skipped. The beat list at the top of `compose.sh` sets each beat's segments, speed and caption.

## Faces

Nobody has to upload a photo: a member without one is a pixel animal, the same one everywhere, and the face sheet on Profile lets them pick another. That is the film's cast.
