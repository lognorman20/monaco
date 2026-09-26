#!/usr/bin/env python3
"""Cut the demo film from the takes in docs/demo/clips.

Two masters: docs/demo/out/monaco-demo-vertical.mp4 (1080x1920) and
docs/demo/out/monaco-demo-site.mp4 (1920x1080). Needs ffmpeg and ImageMagick
(`magick`); all type is set by ImageMagick and animated by ffmpeg expressions, so no
text filter is needed in ffmpeg.

    scripts/demo/film.py                 # both masters
    scripts/demo/film.py stocks vote     # only these takes' beats, to check them
    scripts/demo/film.py cards login     # the drawn cards too

The film is the BEATS list below. A beat plays a few segments of one take a little
faster than life ("a-b" in seconds of the take, "a-b@z:cx,cy" punched in z times
around a point), or two takes side by side, or a card the script draws itself: the
title, the cast, the pot counting up, the sign-off. Every phone settles into place,
every caption slides up, and the joins are hard cuts except where one phone hands
over to another. Punch-ins ("a-b@z:cx,cy") exist but the film does not use them;
a zoom in the middle of a screen read as a glitch.
"""
from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
CLIPS = ROOT / "docs" / "demo" / "clips"
OUT = ROOT / "docs" / "demo" / "out"
WORK = OUT / "film"
ICON = ROOT / "apps/mobile/Monaco/Assets.xcassets/AppIcon.appiconset/AppIcon-Light.png"
AVATARS = ROOT / "apps/mobile/Monaco/Assets.xcassets/Avatars"

FPS = 30
PAPER, INK, MUTED, GOLD, GOLD_DARK = "#F8F5EE", "#0F291C", "#55645B", "#C9A24A", "#7A5C12"
TAKE_PAPER, TAKE_INK = "#F7F4ED", "#091C13"  # as the simulator records the paper and the hero band
HEAD, DEMI, MEDIUM = "Avenir-Next-Bold", "Avenir-Next-Demi-Bold", "Avenir-Next-Medium"
TAKE_W, TAKE_H = 1206, 2622  # the simulator records at 3x

# The cast, in the order they appear.
CAST = [("Maya", "cat"), ("Jordan", "rabbit"), ("Priya", "penguin")]


@dataclass
class Clip:
    """One take, cut to segments. A segment is "a-b" in seconds of the take, "a-b/1.4" at
    its own speed, or "a-b@z:cx,cy" punched in z times around a point."""
    take: str
    segs: str
    speed: float = 1.8
    caption: str = ""
    headline: str = ""             # a hook: big type over the phone instead of a caption
    coins_at: float | None = None  # seconds into the beat: a burst of coins from the phone
    faces_at: float | None = None  # seconds into the beat: the cast bounces up at the foot of the frame
    patches: tuple = ()            # (x, y, w, h[, colour]) in take pixels, painted over: a stray clock stamp
    labels: tuple = ()             # (text, size, right_x, top_y) in take pixels: a figure set in the app's money face
    faces: tuple = ()              # (animal, x, y, size) in take pixels: a pixel animal over an initials disc
    transition: str = "cut"        # how this beat arrives: cut | slideleft | fade | smoothup


@dataclass
class Split:
    """Two takes side by side, each cut like a Clip; the shorter holds its last frame."""
    left: Clip
    right: Clip
    caption: str = ""
    transition: str = "slideup"


@dataclass
class Card:
    kind: str                       # intro | cast | counter | outro
    seconds: float
    transition: str = "fade"
    values: list[str] = field(default_factory=list)


BEATS: list[Clip | Split | Card] = [
    Card("intro", 2.8),
    Card("cast", 2.6, transition="smoothup"),
    Clip("login", "12.5-15,24.5-26.5,34-37", 2.0, "Sign in", transition="smoothup"),
    Clip("onboarding", "5.5-7.5,12-13.5,18-21", 2.0, "Create your profile"),
    Clip("start-cabal", "7-9,16-18.5", 2.0, "Start a cabal"),
    Clip("start-cabal", "30-32.5,44.5-46.5,51.5-53.5,59-63", 2.0, "Set the rules"),
    Clip("invite-join", "6-8.5,20-23", 1.6, "Invite your friends with a code"),
    Split(Clip("join-jordan", "8-10.5,24-27,38-40.5", 1.8), Clip("join-priya", "5.5-8,22-25,47-49.5", 1.8),
          "A paste and they're in!"),
    Split(Clip("fund", "6-8.5,21-23.5,25.5-27", 1.8), Clip("fund-priya", "12-14,21-23,28-30.5", 1.8),
          "Fund the pot", transition="cut"),
    Card("counter", 3.4, transition="fade", values=["$500", "$800", "$1,000"]),
    Clip("stocks", "3-6,10-13,14.5-17,18-20,23-25", 1.8, "Browse real stocks", transition="smoothup"),
    Clip("propose", "4.5-6.5,9.5-11,18.5-21.5,32.5-34.5,38.5-40.5", 1.9, "Propose a buy"),
    Split(Clip("vote-priya", "5-8.5,17-20", 1.6), Clip("vote-maya", "4.5-6.5,9-10.5,17-19.5", 1.6),
          "Everyone votes", transition="slideleft"),
    Clip("vote-maya", "42.5-46", 1.3, "Majority wins, the cabal buys", coins_at=1.2, faces_at=1.3, transition="cut"),
    Split(Clip("chat-jordan", "21-23,30.5-33", 1.6), Clip("chat-maya", "16-20", 1.6), "Talk it over"),
    Clip("pre-ipo", "12.5-15,24-26.5,35.5-38", 1.8, "Pre-IPO too", transition="slideleft"),
    Clip("bot", "7-9.5,24-26,33-35,40-43", 1.9, "Add a trading bot", transition="slideleft"),
    Clip("bot-key", "24.5-27,39-41.5,56-59", 1.8, "Connect it to ClawPump", transition="slideleft"),
    Clip("bot-activity", "0.5-3,4-6.5", 1.4, "Watch it trade", coins_at=2.3),
    Clip("profit", "1.5-4,25.5-27.5,34.5-37.5,56-57.5", 1.8, "Cash out any time", coins_at=4.2, transition="slideleft"),
    Clip("board-outro", "2-4.5,16-20.5", 1.6, "See who's up", faces_at=2.4),
    Card("outro", 3.2),
]

# The launch spot: thirty seconds for the waitlist. One story and one pot, and every figure
# on screen agrees with the one before it: Jordan funds $500, the proposal reads a $500 pot,
# and nothing from later in the shoot appears. Two kinds of touch-up keep it honest: paper
# patches over the clock stamps the app writes in real time (a "34m", a date), which would
# contradict the 9:41 status bar, and one figure re-set in the app's money face where a take
# was shot after more money had arrived than the story shows.
PROPOSAL_STAMPS = ((505, 724, 90, 54, 700, 724),)                    # "34m" beside "Proposed by Jordan"
BOUGHT_STAMPS = PROPOSAL_STAMPS + ((610, 1880, 560, 60, 40, 1880),)  # the fill's date under the tracker
# Priya's phone was shot after she had funded, so its holdings row reads $800; the story
# has $500 in the pot. The row moves up when her ballot replaces the buttons, so the
# figure set over it moves with it. Her collapsed hero band carries a blurred ghost of her
# slice under the status bar, covered with the band's own pixels from beside the island.
PRIYA_CASH_TOP = "if(lt(t,0.42),2075,if(lt(t,0.69),2075-43*(t-0.42)/0.27,2032))"
PRIYA_TOUCHUPS = dict(
    patches=((940, 2028, 230, 96, 480, 2028),                 # the $800 figure, both positions
             # The band darkens toward the top, so each cover comes from its own rows: the
             # clean stretch right of the island for the figures, the stretch between the
             # clock and the island for the labels above them.
             (24, 128, 240, 70, 790, 128), (150, 128, 240, 70, 790, 128), (170, 128, 240, 70, 790, 128),  # "$300.00"
             (1000, 128, 206, 70, 790, 128),                                                              # "$0.00"
             # "Your slice", in the rows above the clock. The faint label on the right sits
             # behind the status icons, whose tops begin at row 77, so it stays.
             (30, 36, 135, 56, 280, 36)),
    labels=(("$500.00", 60, 1155, PRIYA_CASH_TOP),),
)
# Maya's proposal screen predates the animal ballot faces, so "Priya voted yes" wears an
# initial; the penguin goes over it.
PRIYA_DISC = (("penguin", 51, 911, 62),)
VOTES_ROW_STAMP = ((340, 2140, 80, 70, 620, 2140),)         # "27m" on Priya's ballot, under the comment bar
LAUNCH: list = [
    Clip("join-priya", "50.0-53.3", 1.1, headline="Invest with your friends.", transition="fade"),
    Clip("start-cabal", "30.0-32.6/1.8,58.9-59.45/1.5,60.4-61.3/1.5", 1.6, "Start a cabal", transition="slideleft"),
    Clip("join-jordan", "52.4-54.9", 1.3, "Friends join with a code", transition="slideleft"),
    Clip("fund", "20.6-21.9/1.3,26.2-27.15/1.3", 1.3, "Fund the pot", transition="slideleft"),
    Clip("join-priya", "42.2-43.15/1.3,47.9-49.9/1.3", 1.3, "Anyone with the code can join", coins_at=1.05, transition="slideleft"),
    Clip("stocks", "4.5-6.9", 1.2, "Pick a real stock", transition="slideleft"),
    Clip("propose", "2.1-3.4/1.5,5.1-6.1/1.5,18.35-19.15/1.5,27.1-28.5/1.5,37.7-38.15/1.4,38.85-39.9/1.4", 1.5, "Propose a buy", transition="cut"),
    Split(Clip("vote-priya", "17.3-19.7", 1.3, **PRIYA_TOUCHUPS),
          Clip("vote-maya", "17.8-20.2", 1.3, patches=PROPOSAL_STAMPS + VOTES_ROW_STAMP, faces=PRIYA_DISC), "Everyone votes", transition="slideleft"),
    Clip("vote-maya", "43.2-46.1", 1.0, "Majority wins. The cabal buys.", coins_at=0.55, faces_at=0.7, patches=BOUGHT_STAMPS, faces=PRIYA_DISC, transition="slideleft"),
    Clip("pre-ipo", "24.0-24.45/1.4,25.45-27.2/1.4", 1.4, "Pre-IPO too", transition="slideleft"),
    Clip("bot", "33.9-34.9/1.3,39.9-40.35/1.3", 1.3, "Or add a trading bot", transition="slideleft"),
    Card("outro", 6.4),
]

# Frame geometry per master: the phone (w, h, x, y), the split phones, the caption slot.
GEOMETRY = {
    "vertical": dict(size=(1080, 1920), phone=(690, 1500, 195, 120), radius=76,
                     hook=(600, 1304, 240, 500), hook_radius=66,
                     split=[(480, 1044, 48, 330), (480, 1044, 552, 330)],
                     caption=dict(size=54, width=940, gravity="south", offset=(0, 118), align="center"),
                     headline=dict(size=92, width=940, gravity="north", offset=(0, 170), align="center")),
    "site": dict(size=(1920, 1080), phone=(396, 858, 1188, 105), radius=44,
                 hook=(396, 858, 1188, 105), hook_radius=44,
                 split=[(396, 858, 984, 105), (396, 858, 1420, 105)],
                 caption=dict(size=62, width=820, gravity="west", offset=(150, 0), align="west"),
                 headline=dict(size=104, width=880, gravity="west", offset=(150, 0), align="west")),
}


def run(cmd: list[str]) -> None:
    done = subprocess.run(cmd, capture_output=True, text=True)
    if done.returncode != 0:
        sys.stderr.write(done.stderr[-4000:])
        raise SystemExit(f"{cmd[0]} failed ({done.returncode}); see above")


def ffmpeg(*args: str) -> None:
    run(["ffmpeg", "-y", "-loglevel", "error", *args])


def magick(*args: str) -> None:
    run(["magick", *args])


def duration(path: Path) -> float:
    out = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", str(path)],
                         check=True, capture_output=True, text=True).stdout.strip()
    return float(out)


VERSION = 6


def spec_of(beat: object) -> dict:
    """A beat's fields, without the ones left unset, so a new option does not re-render every beat."""
    return {k: (spec_of(v) if hasattr(v, "__dict__") else v) for k, v in beat.__dict__.items() if v is not None and v != ""}


def stamp(name: str, spec: object) -> Path:
    """The output path for a rendered piece, keyed on its spec so a changed beat re-renders."""
    digest = hashlib.sha1(json.dumps(spec, sort_keys=True, default=str).encode()).hexdigest()[:10]
    return WORK / f"{name}-{digest}.mp4"


# --- static pieces: paper, masks, type -------------------------------------------------

def paper(fmt: str) -> Path:
    w, h = GEOMETRY[fmt]["size"]
    p = WORK / f"paper-{fmt}.png"
    if not p.exists():
        magick("-size", f"{w}x{h}", f"xc:{PAPER}", str(p))
    return p


def bezel(fmt: str, phone: tuple[int, int, int, int], radius: int | None = None) -> Path:
    """The paper with one ink rounded rectangle where a phone sits."""
    w, h = GEOMETRY[fmt]["size"]
    pw, ph, px, py = phone
    r = (GEOMETRY[fmt]["radius"] if radius is None else radius) + 16
    p = WORK / f"bezel-{fmt}-{pw}x{ph}-{px}-{py}-{r}.png"
    if not p.exists():
        magick("-size", f"{w}x{h}", f"xc:{PAPER}", "-fill", INK,
               "-draw", f"roundrectangle {px - 14},{py - 14} {px + pw + 13},{py + ph + 13} {r},{r}", str(p))
    return p


def mask(fmt: str, pw: int, ph: int, r: int | None = None) -> Path:
    if r is None:
        r = GEOMETRY[fmt]["radius"] if pw >= 600 or fmt == "site" else 52
    p = WORK / f"mask-{pw}x{ph}-{r}.png"
    if not p.exists():
        magick("-size", f"{pw}x{ph}", "xc:none", "-fill", "white",
               "-draw", f"roundrectangle 0,0 {pw - 1},{ph - 1} {r},{r}", str(p))
    return p


def caption_png(fmt: str, text: str, style: str = "caption") -> Path:
    w, h = GEOMETRY[fmt]["size"]
    c = GEOMETRY[fmt][style]
    font = HEAD if style == "headline" else DEMI
    p = WORK / f"cap-{fmt}-{style}-{hashlib.sha1(text.encode()).hexdigest()[:8]}.png"
    if not p.exists():
        ox, oy = c["offset"]
        magick("-size", f"{w}x{h}", "xc:none", "-font", font, "-fill", INK, "-pointsize", str(c["size"]),
               "-interline-spacing", "6" if style == "caption" else "-4", "-size", f"{c['width']}x", "-background", "none",
               "-gravity", c["align"], f"caption:{text}", "-gravity", c["gravity"],
               "-geometry", f"+{ox}+{oy}", "-composite", str(p))
    return p


def text_png(text: str, font: str, size: int, color: str, name: str) -> Path:
    """A line of type on nothing, trimmed to its ink."""
    p = WORK / f"text-{name}-{hashlib.sha1(f'{text}|{font}|{size}|{color}'.encode()).hexdigest()[:8]}.png"
    if not p.exists():
        magick("-background", "none", "-fill", color, "-font", font, "-pointsize", str(size),
               f"label:{text}", "-trim", "+repage", str(p))
    return p


def png_size(p: Path) -> tuple[int, int]:
    out = subprocess.run(["magick", "identify", "-format", "%w %h", str(p)], check=True, capture_output=True, text=True).stdout
    w, h = out.split()
    return int(w), int(h)


def icon_png(size: int) -> Path:
    p = WORK / f"icon-{size}.png"
    if not p.exists():
        r = int(size * 0.22)
        magick(str(ICON), "-resize", f"{size}x{size}",
               "(", "-size", f"{size}x{size}", "xc:none", "-fill", "white",
               "-draw", f"roundrectangle 0,0 {size - 1},{size - 1} {r},{r}", ")",
               "-compose", "DstIn", "-composite", str(p))
    return p


FACE_PAD = 90  # the canvas is wider than the disc, so a name like Jordan is not clipped


def face_png(animal: str, name: str, size: int) -> Path:
    """One pixel animal on a paper disc with a hairline, the name under it. The canvas is
    (size + FACE_PAD) wide with the disc centred."""
    p = WORK / f"face-{animal}-{size}-{'named' if name else 'bare'}.png"
    if not p.exists():
        src = AVATARS / f"avatar-{animal}.imageset" / f"avatar-{animal}.png"
        disc = size
        w = size + FACE_PAD
        cx = w // 2
        magick("-size", f"{w}x{disc + (66 if name else 0)}", "xc:none",
               "(", str(src), "-filter", "point", "-resize", f"{disc}x{disc}",
               "(", "-size", f"{disc}x{disc}", "xc:none", "-fill", "white", "-draw", f"circle {disc // 2},{disc // 2} {disc // 2},1", ")",
               "-compose", "DstIn", "-composite", ")", "-gravity", "north", "-compose", "Over", "-composite",
               "-fill", "none", "-stroke", "#D9D3C7", "-strokewidth", "3", "-draw", f"circle {cx},{disc // 2} {cx},2",
               "-stroke", "none", "-fill", INK, "-font", DEMI, "-pointsize", str(max(30, size // 5)), "-gravity", "south",
               *(["-annotate", "+0+0", name] if name else []), str(p))
    return p


def coin_png() -> Path:
    p = WORK / "coin.png"
    if not p.exists():
        magick("-size", "44x44", "xc:none", "-fill", GOLD, "-draw", "circle 22,22 22,2",
               "-fill", "none", "-stroke", GOLD_DARK, "-strokewidth", "3", "-draw", "circle 22,22 22,4",
               "-stroke", "none", "-fill", GOLD_DARK, "-font", HEAD, "-pointsize", "26", "-gravity", "center",
               "-annotate", "+0+1", "$", str(p))
    return p


# --- motion ------------------------------------------------------------------------------

def ease_out(t: str, d: float) -> str:
    """1 at the start falling to 0 by d seconds, cubic, as an ffmpeg expression in t."""
    return f"pow(max(0,1-({t})/{d}),3)"


def settle(x: int, y: int, drop: int = 42, d: float = 0.4) -> str:
    """Overlay position for a phone that lands from a little below."""
    return f"x={x}:y='{y}+{drop}*{ease_out('t', d)}':eval=frame"


def caption_layer(fmt: str, text: str, secs: float, idx: int, style: str = "caption") -> tuple[list[str], str]:
    """Inputs and a filter that fade the caption in, slide it up, and fade it out. A
    headline holds to the cut; the beat after it changes the subject."""
    p = caption_png(fmt, text, style)
    out_fade = "" if style == "headline" else f",fade=t=out:st={max(0.0, secs - 0.5):.2f}:d=0.35:alpha=1"
    filt = f"[{idx}:v]format=rgba,fade=t=in:st=0.18:d=0.32:alpha=1{out_fade}[cap];"
    pos = f"x=0:y='26*{ease_out('t-0.18', 0.45)}':eval=frame"
    return ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(p)], filt + "{bg}[cap]overlay=" + pos + "{out}"


def coins(secs: float, at: float, cx: int, cy: int, start_idx: int) -> tuple[list[str], str, int]:
    """Ten coins thrown from (cx, cy) at `at` seconds, falling on the paper."""
    p = coin_png()
    inputs: list[str] = []
    filt = ""
    seeds = [(-420, 1500), (-300, 1750), (-170, 1900), (-60, 2000), (60, 2050), (170, 1950), (300, 1800), (420, 1550), (-230, 1300), (240, 1250)]
    for k, (vx, vy) in enumerate(seeds):
        inputs += ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(p)]
        i = start_idx + k
        tt = f"(t-{at:.2f})"
        x = f"{cx}+{vx}*{tt}"
        y = f"{cy}-{vy}*{tt}+2400*{tt}*{tt}"
        filt += f"{{bg}}[{i}:v]overlay=x='{x}':y='{y}':eval=frame:enable='between(t,{at:.2f},{at + 1.4:.2f})'{{out}};"
    return inputs, filt, start_idx + len(seeds)


def bounce(y: int, start: float, d: float = 0.45, travel: int = 700, lift: int = 26) -> str:
    """An overlay y expression: parked off the bottom, then up to y with a little overshoot."""
    return (f"if(lt(t,{start}),9999,{y}+{travel}*pow(max(0,1-(t-{start})/{d}),2)"
            f"-{lift}*sin(PI*min(1,(t-{start})/{d})))")


def cast_layer(fmt: str, secs: float, at: float, start_idx: int, phone: tuple | None = None) -> tuple[list[str], str, int]:
    """The three faces, bouncing up in a row: at the foot of the vertical frame, under the
    caption on the site frame."""
    W, H = GEOMETRY[fmt]["size"]
    size = 150 if fmt == "vertical" else 110
    gap = 12
    total = 3 * (size + FACE_PAD) + 2 * gap
    if fmt == "vertical":
        pw, ph, px, py = phone or GEOMETRY[fmt]["phone"]
        x0, y = (W - total) // 2, py + ph - size + 30
    else:
        x0, y = 150, H // 2 + 96
    inputs: list[str] = []
    filt = ""
    idx = start_idx
    for k, (_, a) in enumerate(CAST):
        inputs += ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(face_png(a, "", size))]
        filt += f"{{bg}}[{idx}:v]overlay=x={x0 + k * (size + FACE_PAD + gap)}:y='{bounce(y, at + 0.12 * k)}':eval=frame{{out}};"
        idx += 1
    return inputs, filt, idx


def chain(pieces: str, first: str, last: str) -> str:
    """Thread {bg}/{out} placeholders through a run of overlay steps."""
    steps = [s for s in pieces.split(";") if s]
    out = ""
    cur = first
    for k, s in enumerate(steps):
        nxt = last if k == len(steps) - 1 else f"[s{k}_{abs(hash(pieces)) % 9973}]"
        if "{bg}" in s:
            out += s.replace("{bg}", cur).replace("{out}", nxt) + ";"
            cur = nxt
        else:
            out += s + ";"
    return out


# --- the takes ---------------------------------------------------------------------------

def parse_segs(spec: str, speed: float) -> list[tuple[float, float, float, float, float, float]]:
    """"a-b", "a-b/1.4" or "a-b@z:cx,cy" -> (a, b, z, cx, cy, speed). The punch centre
    carries a comma, so the tokens are re-joined in pairs where one carries an @."""
    tokens = [t.strip() for t in spec.split(",")]
    segs = []
    k = 0
    while k < len(tokens):
        tok = tokens[k]
        sp = speed
        if "/" in tok:
            tok, rate = tok.split("/")
            sp = float(rate)
        if "@" in tok:
            ab, punch = tok.split("@")
            z, cx = punch.split(":")
            cy = tokens[k + 1]
            k += 2
            a, b = ab.split("-")
            segs.append((float(a), float(b), float(z), float(cx), float(cy), sp))
        else:
            a, b = tok.split("-")
            segs.append((float(a), float(b), 1.0, 0.5, 0.5, sp))
            k += 1
    return segs


def money_png(text: str, size: int) -> Path:
    """A figure in the app's money face, ink on nothing, for setting over a patched one."""
    p = WORK / f"money-{hashlib.sha1(f'{text}-{size}'.encode()).hexdigest()[:8]}.png"
    if not p.exists():
        magick("-background", "none", "-fill", INK, "-font", DEMI, "-pointsize", str(size), f"label:{text}", "-trim", "+repage", str(p))
    return p


def disc_png(animal: str, size: int) -> Path:
    """One pixel animal cut to a disc, the way the app draws a face."""
    p = WORK / f"disc-{animal}-{size}.png"
    if not p.exists():
        src = AVATARS / f"avatar-{animal}.imageset" / f"avatar-{animal}.png"
        magick(str(src), "-filter", "point", "-resize", f"{size}x{size}",
               "(", "-size", f"{size}x{size}", "xc:none", "-fill", "white", "-draw", f"circle {size // 2},{size // 2} {size // 2},1", ")",
               "-compose", "DstIn", "-composite", str(p))
    return p


def cut_take(clip: Clip) -> tuple[Path, float]:
    """Render the take's segments, sped up and punched, at take resolution. Returns (path, seconds).

    The simulator only records a frame when the screen changes, so a segment that opens in
    stillness would otherwise start at the next change instead of where it says. The take
    is filled to a constant rate first, and every cut lands on the second it names."""
    segs = parse_segs(clip.segs, clip.speed)
    secs = sum((b - a) / sp for a, b, _, _, _, sp in segs)
    out = stamp(f"cut-{clip.take}", [clip.take, clip.segs, clip.speed, list(clip.patches), list(clip.labels), list(clip.faces)])
    if out.exists():
        return out, secs
    n = len(segs)
    inputs = ["-i", str(CLIPS / f"{clip.take}.mov")]
    filt = f"[0:v]fps={FPS},split={n}" + "".join(f"[i{k}]" for k in range(n)) + ";"
    for k, (a, b, z, cx, cy, sp) in enumerate(segs):
        f = f"[i{k}]trim=start={a}:end={b},setpts=(PTS-STARTPTS)/{sp}"
        if z > 1:
            f += (f",crop=w=iw/{z}:h=ih/{z}:x=(iw-iw/{z})*{cx}:y=(ih-ih/{z})*{cy},"
                  f"scale={TAKE_W}:{TAKE_H}")
        filt += f + f"[s{k}];"
    filt += "".join(f"[s{k}]" for k in range(n)) + f"concat=n={n}:v=1:a=0,fps={FPS}[c0];"
    cur = "[c0]"
    # A patch copies a flat region of the same frame over the stamp, so the colour is the
    # frame's own; a colour painted in would land a few levels off after the YUV round trip
    # and show as a faint box. A patch with a colour string is a flat fill for dark areas,
    # where those levels are invisible.
    for m, patch in enumerate(clip.patches):
        x, y, w, h = patch[:4]
        if len(patch) == 5:
            filt += f"{cur}drawbox=x={x}:y={y}:w={w}:h={h}:color={patch[4]}:t=fill[p{m}];"
        else:
            sx, sy = patch[4], patch[5]
            filt += (f"{cur}split[pa{m}][pb{m}];[pb{m}]crop={w}:{h}:{sx}:{sy}[pc{m}];"
                     f"[pa{m}][pc{m}]overlay=x={x}:y={y}[p{m}];")
        cur = f"[p{m}]"
    extra = 1
    for j, (text, size, right_x, top_y) in enumerate(clip.labels):
        png = money_png(text, size)
        lw, lh = png_size(png)
        inputs += ["-i", str(png)]
        filt += f"{cur}[{extra}:v]overlay=x={right_x - lw}:y='{top_y}':eval=frame[l{j}];"
        cur = f"[l{j}]"
        extra += 1
    for j, (animal, x, y, size) in enumerate(clip.faces):
        inputs += ["-i", str(disc_png(animal, size))]
        filt += f"{cur}[{extra}:v]overlay=x={x}:y={y}[f{j}];"
        cur = f"[f{j}]"
        extra += 1
    filt += f"{cur}format=yuv420p[v]"
    ffmpeg(*inputs, "-filter_complex", filt, "-map", "[v]",
           "-c:v", "libx264", "-preset", "veryfast", "-crf", "16", "-an", str(out))
    return out, secs


def phone_filter(idx: int, pw: int, ph: int, mask_idx: int, label: str) -> str:
    return (f"[{idx}:v]scale={pw}:{ph}:force_original_aspect_ratio=increase,crop={pw}:{ph},setsar=1,"
            f"fps={FPS}[p{label}];[{mask_idx}:v]format=gray[m{label}];[p{label}][m{label}]alphamerge[{label}];")


def render_clip(fmt: str, beat: Clip, name: str) -> tuple[Path, float]:
    cut, secs = cut_take(beat)
    out = stamp(f"beat-{fmt}-{name}", [fmt, spec_of(beat), VERSION])
    if out.exists():
        return out, secs
    g = GEOMETRY[fmt]
    phone = g["hook"] if beat.headline else g["phone"]
    radius = g["hook_radius"] if beat.headline else g["radius"]
    pw, ph, px, py = phone
    inputs = ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(bezel(fmt, phone, radius)), "-i", str(cut),
              "-loop", "1", "-t", f"{secs:.3f}", "-i", str(mask(fmt, pw, ph, radius))]
    filt = phone_filter(1, pw, ph, 2, "phone")
    # The bezel is drawn on the paper, so it settles with the phone: draw it as a layer.
    filt += f"[0:v]crop={pw + 28}:{ph + 28}:{px - 14}:{py - 14}[bz];"
    inputs += ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(paper(fmt))]
    # A beat that continues the last one on a hard cut keeps the phone where it is.
    drop = 0 if beat.transition == "cut" else 42
    steps = (f"{{bg}}[bz]overlay={settle(px - 14, py - 14, drop)}{{out}};"
             f"{{bg}}[phone]overlay={settle(px, py, drop)}{{out}};")
    idx = 4
    if beat.coins_at is not None:
        cin, cf, idx = coins(secs, beat.coins_at, px + pw // 2, py + ph // 2, idx)
        inputs += cin
        steps += cf
    if beat.faces_at is not None:
        cin, cf, idx = cast_layer(fmt, secs, beat.faces_at, idx, phone)
        inputs += cin
        steps += cf
    if beat.headline:
        cin, cf = caption_layer(fmt, beat.headline, secs, idx, "headline")
        inputs += cin
        steps += cf
        idx += 1
    if beat.caption:
        cin, cf = caption_layer(fmt, beat.caption, secs, idx)
        inputs += cin
        steps += cf
    filt += chain(steps, "[3:v]", "[out]")
    ffmpeg(*inputs, "-filter_complex", filt.rstrip(";"), "-map", "[out]", "-t", f"{secs:.3f}",
           "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-an", str(out))
    return out, secs


def render_split(fmt: str, beat: Split, name: str) -> tuple[Path, float]:
    lcut, lsecs = cut_take(beat.left)
    rcut, rsecs = cut_take(beat.right)
    secs = max(lsecs, rsecs)
    out = stamp(f"beat-{fmt}-{name}", [fmt, spec_of(beat), VERSION])
    if out.exists():
        return out, secs
    g = GEOMETRY[fmt]
    (lw, lh, lx, ly), (rw, rh, rx, ry) = g["split"]
    r = g["radius"] + 16
    ink = WORK / f"ink-split-{fmt}.png"
    if not ink.exists():
        w, h = g["size"]
        magick("-size", f"{w}x{h}", "xc:none", "-fill", INK,
               "-draw", f"roundrectangle {lx - 14},{ly - 14} {lx + lw + 13},{ly + lh + 13} {r},{r}",
               "-draw", f"roundrectangle {rx - 14},{ry - 14} {rx + rw + 13},{ry + rh + 13} {r},{r}", str(ink))
    inputs = ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(paper(fmt)), "-i", str(lcut), "-i", str(rcut),
              "-loop", "1", "-t", f"{secs:.3f}", "-i", str(mask(fmt, lw, lh)),
              "-loop", "1", "-t", f"{secs:.3f}", "-i", str(ink)]
    # Hold the shorter side on its last frame so both run to the cut.
    filt = (f"[1:v]tpad=stop_mode=clone:stop_duration={secs - lsecs + 0.5:.3f}[l0];"
            f"[2:v]tpad=stop_mode=clone:stop_duration={secs - rsecs + 0.5:.3f}[r0];"
            f"[3:v]format=gray,split=2[mL][mR];"
            f"[l0]scale={lw}:{lh}:force_original_aspect_ratio=increase,crop={lw}:{lh},setsar=1,fps={FPS}[pL];"
            f"[pL][mL]alphamerge[L];"
            f"[r0]scale={rw}:{rh}:force_original_aspect_ratio=increase,crop={rw}:{rh},setsar=1,fps={FPS}[pR];"
            f"[pR][mR]alphamerge[R];")
    steps = (f"{{bg}}[4:v]overlay=x=0:y='42*{ease_out('t', 0.4)}':eval=frame{{out}};"
             f"{{bg}}[L]overlay={settle(lx, ly)}{{out}};"
             f"{{bg}}[R]overlay={settle(rx, ry)}{{out}};")
    idx = 5
    if beat.caption:
        cin, cf = caption_layer(fmt, beat.caption, secs, idx)
        inputs += cin
        steps += cf
    filt += chain(steps, "[0:v]", "[out]")
    ffmpeg(*inputs, "-filter_complex", filt.rstrip(";"), "-map", "[out]", "-t", f"{secs:.3f}",
           "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-an", str(out))
    return out, secs


# --- the cards ---------------------------------------------------------------------------

def cover_png(w: int, h: int, color: str) -> Path:
    p = WORK / f"cover-{w}x{h}-{color.strip('#')}.png"
    if not p.exists():
        magick("-size", f"{w}x{h}", f"xc:{color}", str(p))
    return p


def typed(add, x: int, y: int, layer: Path, start: float, d: float) -> str:
    """Overlay a text layer at (x, y) and reveal it left to right from `start` over `d`
    seconds: a paper cover slides right off it, with an ink cursor on the cover's edge."""
    w, h = png_size(layer)
    i_text = add(layer)
    i_cover = add(cover_png(w + 24, h + 16, PAPER))
    i_cursor = add(cover_png(5, h + 8, INK))
    prog = f"min(1,max(0,(t-{start})/{d}))"
    return (f"{{bg}}[{i_text}:v]overlay=x={x}:y={y}{{out}};"
            f"{{bg}}[{i_cover}:v]overlay=x='{x}+{w}*{prog}':y={y - 8}:eval=frame{{out}};"
            f"{{bg}}[{i_cursor}:v]overlay=x='{x}+{w}*{prog}':y={y - 4}:eval=frame:"
            f"enable='between(t,{start},{start + d + 0.5})'{{out}};")


def pop(idx: int, size: int, x: int, y: int, start: float) -> str:
    """Overlay a square layer that scales up with a little overshoot. The layer is a
    `size` px canvas with the artwork centred at two thirds; zoompan crops into it, which
    reads as growth."""
    on = f"((on/{FPS})-{start})"
    z = (f"if(lt({on},0),1,if(lt({on},0.28),1+0.75*({on}/0.28),"
         f"if(lt({on},0.48),1.75-0.25*(({on}-0.28)/0.2),1.5)))")
    return (f"[{idx}:v]zoompan=z='{z}':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)'"
            f":d=1:s={size}x{size}:fps={FPS}[pop{idx}];"
            f"{{bg}}[pop{idx}]overlay=x={x}:y={y}:enable='gte(t,{start})'{{out}};")


def render_card(fmt: str, beat: Card, name: str) -> tuple[Path, float]:
    secs = beat.seconds
    out = stamp(f"card-{fmt}-{name}", [fmt, spec_of(beat), VERSION])
    if out.exists():
        return out, secs
    W, H = GEOMETRY[fmt]["size"]
    vertical = fmt == "vertical"
    inputs = ["-loop", "1", "-t", f"{secs:.3f}", "-i", str(paper(fmt))]
    filt = ""
    steps = ""
    idx = 1

    def add(p: Path) -> int:
        nonlocal idx
        inputs.extend(["-loop", "1", "-t", f"{secs:.3f}", "-i", str(p)])
        idx += 1
        return idx - 1

    if beat.kind in ("intro", "outro"):
        icon_size = 300 if vertical else 240
        canvas = int(icon_size * 1.5)
        icon = WORK / f"icon-canvas-{canvas}.png"
        if not icon.exists():
            magick("-size", f"{canvas}x{canvas}", "xc:none", str(icon_png(icon_size)), "-gravity", "center",
                   "-composite", str(icon))
        word = text_png("Monaco", HEAD, 150 if vertical else 128, INK, f"word-{fmt}")
        line = "The hedge fund with your friends."
        tag = text_png(line, MEDIUM, 54 if vertical else 50, MUTED, f"tag-{fmt}-{beat.kind}")
        ww, wh = png_size(word)
        tw, th = png_size(tag)
        if vertical:
            ix, iy = (W - canvas) // 2, (470 if beat.kind == "intro" else 250)
            wx, wy = (W - ww) // 2, (900 if beat.kind == "intro" else 760)
            tx, ty = (W - tw) // 2, (1090 if beat.kind == "intro" else 950)
        elif beat.kind == "outro":
            ix, iy = (W - canvas) // 2, 30
            wx, wy = (W - ww) // 2, 420
            tx, ty = (W - tw) // 2, 580
        else:
            ix, iy = (W - canvas) // 2 - 300, 330
            block = wh + 22 + th
            wx, wy = ix + canvas + 34, iy + canvas // 2 - block // 2
            tx, ty = wx + 6, wy + wh + 22
        i_icon, i_tag = add(icon), add(tag)
        steps += pop(i_icon, canvas, ix, iy, 0.1)
        steps += typed(add, wx, wy, word, 0.45, 0.55)
        steps += (f"[{i_tag}:v]format=rgba,fade=t=in:st=1.05:d=0.35:alpha=1[tag];"
                  f"{{bg}}[tag]overlay=x={tx}:y='{ty}+30*{ease_out('t-1.05', 0.45)}':eval=frame{{out}};")
        if beat.kind == "outro":
            # The ask, then the cast comes back to wave the film off.
            ask = text_png("Join the waitlist", DEMI, 62 if vertical else 50, INK, f"ask-{fmt}")
            site_line = text_png("trymonaco.xyz", HEAD, 104 if vertical else 84, INK, f"site-{fmt}")
            aw, ah = png_size(ask)
            sw, sh = png_size(site_line)
            ay = 1210 if vertical else 700
            sy = ay + ah + (28 if vertical else 18)
            steps += (f"[{add(ask)}:v]format=rgba,fade=t=in:st=1.45:d=0.3:alpha=1[ask];"
                      f"{{bg}}[ask]overlay=x={(W - aw) // 2}:y='{ay}+30*{ease_out('t-1.45', 0.45)}':eval=frame{{out}};")
            steps += typed(add, (W - sw) // 2, sy, site_line, 1.85, 0.6)
            size = 150 if vertical else 110
            faces = [face_png(a, "", size) for _, a in CAST]
            gap = 10
            total = 3 * (size + FACE_PAD) + 2 * gap
            fy = (sy + sh + 120) if vertical else (sy + sh + 60)
            for k, f in enumerate(faces):
                i = add(f)
                fx = (W - total) // 2 + k * (size + FACE_PAD + gap)
                start = 2.5 + 0.12 * k
                steps += (f"{{bg}}[{i}:v]overlay=x={fx}:y='if(lt(t,{start}),{H + 10},"
                          f"{fy}+700*pow(max(0,1-(t-{start})/0.45),2)-26*sin(PI*min(1,(t-{start})/0.45)))':eval=frame{{out}};")
    elif beat.kind == "cast":
        title = text_png("Meet the cabal", HEAD, 96 if vertical else 84, INK, f"cast-title-{fmt}")
        tw, th = png_size(title)
        size = 260 if vertical else 200
        gap = 20
        total = 3 * (size + FACE_PAD) + 2 * gap
        tx, ty = (W - tw) // 2, (420 if vertical else 200)
        steps += typed(add, tx, ty, title, 0.1, 0.5)
        fy = 760 if vertical else 430
        for k, (n, a) in enumerate(CAST):
            i = add(face_png(a, n, size))
            fx = (W - total) // 2 + k * (size + FACE_PAD + gap)
            start = 0.55 + 0.16 * k
            steps += (f"{{bg}}[{i}:v]overlay=x={fx}:y='if(lt(t,{start}),{H + 10},"
                      f"{fy}+900*pow(max(0,1-(t-{start})/0.5),2)-34*sin(PI*min(1,(t-{start})/0.5)))':eval=frame{{out}};")
    elif beat.kind == "counter":
        label = text_png("In the pot", DEMI, 60 if vertical else 54, MUTED, f"pot-label-{fmt}")
        lw, lh = png_size(label)
        i_l = add(label)
        ly = 640 if vertical else 300
        steps += (f"[{i_l}:v]format=rgba,fade=t=in:st=0.05:d=0.3:alpha=1[lab];"
                  f"{{bg}}[lab]overlay=x={(W - lw) // 2}:y={ly}{{out}};")
        ny = 760 if vertical else 400
        step = secs / (len(beat.values) + 0.6)
        for k, value in enumerate(beat.values):
            num = text_png(value, HEAD, 230 if vertical else 200, INK, f"pot-{fmt}-{k}-{value.replace('$', '').replace(',', '')}")
            nw, nh = png_size(num)
            i = add(num)
            t0 = 0.25 + k * step
            t1 = t0 + step
            last = k == len(beat.values) - 1
            fade = f"fade=t=in:st={t0:.2f}:d=0.1:alpha=1" + ("" if last else f",fade=t=out:st={t1:.2f}:d=0.22:alpha=1")
            steps += f"[{i}:v]format=rgba,{fade}[num{k}];"
            x = (W - nw) // 2
            enter = f"{ny}+520*pow(max(0,1-(t-{t0})/0.32),2)-22*sin(PI*min(1,(t-{t0})/0.32))"
            leave = f"{ny}-520*pow(min(1,(t-{t1})/0.28),2)"
            y = f"if(lt(t,{t0}),{H + 10},{enter})" if last else f"if(lt(t,{t0}),{H + 10},if(lt(t,{t1}),{enter},{leave}))"
            steps += f"{{bg}}[num{k}]overlay=x={x}:y='{y}':eval=frame{{out}};"
            cin, cf, idx = coins(secs, t0 + 0.1, W // 2, ny + nh // 2, idx)
            inputs += cin
            steps += cf
    filt += chain(steps, "[0:v]", "[out]")
    ffmpeg(*inputs, "-filter_complex", filt.rstrip(";"), "-map", "[out]", "-t", f"{secs:.3f}",
           "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-an", str(out))
    return out, secs


# --- the join ----------------------------------------------------------------------------

TRANSITION_SECONDS = {"cut": 0.0, "fade": 0.25, "slideleft": 0.28, "slideup": 0.3, "smoothup": 0.35}


def assemble(fmt: str, only: list[str], beats: list, stem: str) -> None:
    W, H = GEOMETRY[fmt]["size"]
    parts: list[tuple[Path, float, str]] = []
    want_cards = not only or "cards" in only
    only = [o for o in only if o != "cards"]
    for k, beat in enumerate(beats):
        if isinstance(beat, Card):
            if not want_cards:
                continue
            name = f"{beat.kind}-{k}"
            path, secs = render_card(fmt, beat, name)
        elif isinstance(beat, Split):
            if only and beat.left.take not in only and beat.right.take not in only:
                continue
            name = f"{beat.left.take}+{beat.right.take}-{k}"
            path, secs = render_split(fmt, beat, name)
        else:
            if only and beat.take not in only:
                continue
            name = f"{beat.take}-{k}"
            path, secs = render_clip(fmt, beat, name)
        print(f"  {name:<28} {secs:5.1f}s")
        parts.append((path, secs, beat.transition))

    n = len(parts)
    inputs: list[str] = []
    filt = ""
    for k, (p, _, _) in enumerate(parts):
        inputs += ["-i", str(p)]
        # fps sets a 1/FPS timebase, so the common timebase is set after it.
        filt += f"[{k}:v]fps={FPS},format=yuv420p,settb=AVTB[n{k}];"
    prev = "[n0]"
    acc = duration(parts[0][0])
    for k in range(1, n):
        dk = duration(parts[k][0])
        tr = parts[k][2]
        td = TRANSITION_SECONDS.get(tr, 0.0)
        label = "[vout]" if k == n - 1 else f"[v{k}]"
        if td == 0:
            # concat hands out a 1/FPS timebase; the next xfade wants AVTB again.
            filt += f"{prev}[n{k}]concat=n=2:v=1:a=0,settb=AVTB{label};"
            acc += dk
        else:
            filt += f"{prev}[n{k}]xfade=transition={tr}:duration={td}:offset={acc - td:.3f}{label};"
            acc += dk - td
        prev = label
    if n == 1:
        filt += "[n0]null[vout];"
    music = ROOT / "docs" / "demo" / "music.m4a"
    audio: list[str] = []
    if music.exists() and not only:
        inputs += ["-i", str(music)]
        audio = ["-map", f"{n}:a", "-shortest", "-af", "volume=0.35"]
    target = OUT / f"{stem}-{fmt}.mp4"
    total = sum(duration(p) for p, _, _ in parts) - sum(TRANSITION_SECONDS.get(tr, 0.0) for _, _, tr in parts[1:])
    filt += f"[vout]fade=t=in:st=0:d=0.35:color={PAPER},fade=t=out:st={total - 0.6:.2f}:d=0.6:color={PAPER}[vfinal];"
    ffmpeg(*inputs, "-filter_complex", filt.rstrip(";"), "-map", "[vfinal]", *audio,
           "-r", str(FPS), "-c:v", "libx264", "-preset", "medium", "-crf", "19", "-pix_fmt", "yuv420p",
           "-movflags", "+faststart", str(target))
    print(f"wrote {target} ({n} parts, {duration(target):.1f}s)")


FILMS = {"tour": ("monaco-demo", BEATS), "launch": ("monaco-launch", LAUNCH)}


def main(argv: list[str]) -> None:
    WORK.mkdir(parents=True, exist_ok=True)
    film = "tour"
    for a in argv:
        if a.startswith("--film="):
            film = a.split("=", 1)[1]
    only = [a for a in argv if not a.startswith("-")]
    stem, beats = FILMS[film]
    for fmt in ("vertical", "site"):
        print(fmt)
        assemble(fmt, only, beats, stem)


if __name__ == "__main__":
    main(sys.argv[1:])
