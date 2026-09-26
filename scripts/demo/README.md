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
scripts/demo/film.py                 # both masters
scripts/demo/film.py stocks vote     # only these takes' beats, to check them
scripts/demo/film.py cards login     # the drawn cards too
```

writes `docs/demo/out/monaco-demo-vertical.mp4` (9:16) and `monaco-demo-site.mp4` (16:9). `--film=launch` cuts the thirty-second waitlist spot instead (`monaco-launch-*.mp4`; its beats are `LAUNCH` in `film.py`). The film is the `BEATS` list at the top of `film.py`: which seconds of each take to keep, how fast to play them, where to punch in, which two takes run side by side, and the cards the script draws itself (the title, the cast, the pot counting up, the sign-off). Pieces are cached under `docs/demo/out/film/` by their spec, so a change to one beat re-renders one beat.

## Faces

Nobody has to upload a photo: a member without one is a pixel animal, the same one everywhere, and the face sheet on Profile lets them pick another. That is the film's cast.
