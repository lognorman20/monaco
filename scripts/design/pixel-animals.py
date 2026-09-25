#!/usr/bin/env python3
"""Draws the eight pixel animals that stand in for a member's photo, into the app's asset catalog.

Each animal is a 16 by 16 grid of letters: `.` shows the background wash, `B` the body, `D` the
body's dark, `L` the body's light (belly, face), `E` the eyes, `W` an eye highlight, `A` the accent
(nose, beak, cheeks). Rendered at 32 pixels a cell, so a 512 pixel square stays crisp down to the
22 point faces on a proposal card. Run from the repo root; it rewrites the image sets in place.
"""
import struct, zlib, pathlib, sys

INK = (15, 41, 28)
WHITE = (255, 255, 255)

ANIMALS = {
    "fox": dict(bg=(220, 232, 208), B=(226, 126, 66), D=(152, 74, 30), L=(250, 238, 216), A=(58, 36, 30), rows=[
        "................",
        "...D........D...",
        "..DBD......DBD..",
        "..DBBD....DBBD..",
        "..DBBBDDDDBBBD..",
        "..DBBBBBBBBBBD..",
        "..DBBBBBBBBBBD..",
        "..DBLLBBBBLLBD..",
        "..DLLELLLLELLD..",
        "..DLLLLLLLLLLD..",
        "...DLLLLAALLD...",
        "...DBLLLLLLBD...",
        "....DBBLLBBD....",
        ".....DDDDDD.....",
        "................",
        "................",
    ]),
    "owl": dict(bg=(222, 226, 242), B=(156, 114, 82), D=(104, 72, 46), L=(226, 198, 158), A=(240, 190, 70), rows=[
        "................",
        "..D..........D..",
        "..DD........DD..",
        "..DBBDDDDDDBBD..",
        "..DBBBBBBBBBBD..",
        ".DBLWWLBBLWWLBD.",
        ".DBLWELBBLWELBD.",
        ".DBLLLLBBLLLLBD.",
        ".DBBBBLAALBBBBD.",
        ".DBBLLLLLLLLBBD.",
        ".DBBLBLLLLBLBBD.",
        ".DBBLLBLLBLLBBD.",
        "..DBBLLLLLLBBD..",
        "...DBBBBBBBBD...",
        "....DDAADAAD....",
        "................",
    ]),
    "penguin": dict(bg=(245, 232, 196), B=(44, 52, 60), D=(22, 28, 34), L=(246, 246, 240), A=(245, 170, 60), rows=[
        "................",
        ".....DDDDDD.....",
        "....DBBBBBBD....",
        "...DBBBBBBBBD...",
        "...DBLLBBLLBD...",
        "...DBLELBLELBD..",
        "...DBLLLAALLBD..",
        "..DDBBLLLLLLBDD.",
        ".DBBDBLLLLLLBDBD",
        ".DBBDBLLLLLLBDBD",
        ".DBBDBLLLLLLBDBD",
        "..DDDBLLLLLLBDD.",
        "....DBBLLLLBBD..",
        ".....DBBBBBBD...",
        "....AAADDDAAA...",
        "................",
    ]),
    "rabbit": dict(bg=(214, 232, 220), B=(228, 224, 216), D=(158, 152, 144), L=(252, 252, 250), A=(240, 150, 160), rows=[
        "....DD....DD....",
        "...DBAD..DABD...",
        "...DBAD..DABD...",
        "...DBAD..DABD...",
        "...DBBD..DBBD...",
        "...DBBBDDBBBD...",
        "..DBBBBBBBBBBD..",
        ".DBBBBBBBBBBBBD.",
        ".DBBEBBBBBBEBBD.",
        ".DBBBBBBAABBBBD.",
        ".DBBBBBLAALBBBD.",
        ".DBBBBBBLLBBBBD.",
        "..DBBBBBBBBBBD..",
        "...DBBBBBBBBD...",
        "....DDDDDDDD....",
        "................",
    ]),
    "cat": dict(bg=(244, 226, 200), B=(142, 142, 154), D=(92, 92, 104), L=(232, 232, 238), A=(240, 150, 160), rows=[
        "................",
        "..D.........D...",
        "..DD.......DD...",
        "..DBD.....DBD...",
        "..DBBDDDDDBBD...",
        "..DBBBBBBBBBD...",
        ".DBBBBBBBBBBBD..",
        ".DBBEBBBBBEBBD..",
        ".DBBBBBBBBBBBD..",
        ".DBBBBBBABBBBD..",
        ".DBBBBBLALBBBD..",
        ".DBBBBBBLBBBBD..",
        "..DBBBBBBBBBD...",
        "...DBBBBBBBD....",
        "....DDDDDDD.....",
        "................",
    ]),
    "frog": dict(bg=(236, 222, 236), B=(112, 182, 92), D=(62, 122, 56), L=(202, 236, 182), A=(244, 122, 112), rows=[
        "................",
        "...DDD....DDD...",
        "..DWWWD..DWWWD..",
        "..DWEWD..DWEWD..",
        "..DWWWDDDDWWWD..",
        ".DBBBBBBBBBBBBD.",
        ".DBBBBBBBBBBBBD.",
        ".DBBBBBBBBBBBBD.",
        ".DBBDBBBBBBDBBD.",
        ".DBBBDDDDDDBBBD.",
        ".DBBLLLLLLLLBBD.",
        ".DBBLLLLLLLLBBD.",
        "..DBBLLLLLLBBD..",
        "...DBBBBBBBBD...",
        "....DDDDDDDD....",
        "................",
    ]),
    "bear": dict(bg=(214, 228, 240), B=(162, 112, 72), D=(112, 72, 40), L=(232, 202, 162), A=(52, 36, 30), rows=[
        "................",
        "..DDD......DDD..",
        ".DBLBD....DBLBD.",
        ".DBBBDDDDDDBBBD.",
        "..DBBBBBBBBBBD..",
        ".DBBBBBBBBBBBBD.",
        ".DBBEBBBBBBEBBD.",
        ".DBBBBBLLLLBBBD.",
        ".DBBBBLLAALLBBD.",
        ".DBBBBLLLLLLBBD.",
        ".DBBBBBLLLLBBBD.",
        ".DBBBBBBBBBBBBD.",
        "..DBBBBBBBBBBD..",
        "...DBBBBBBBBD...",
        "....DDDDDDDD....",
        "................",
    ]),
    "whale": dict(bg=(240, 226, 202), B=(92, 142, 192), D=(52, 92, 142), L=(222, 236, 246), A=(240, 150, 160), rows=[
        "................",
        "......L.L.......",
        ".......L........",
        "......DDD.......",
        "....DDBBBDD.....",
        "..DDBBBBBBBDD...",
        ".DBBBBBBBBBBBD..",
        ".DBBBEBBBBBBBBD.",
        ".DBBBBBBBBBBBBDD",
        ".DBLLLLLLLLBBBD.",
        ".DBLLLLLLLLBBD..",
        "..DBLLLLLLBBD...",
        "...DDBBBBBDD....",
        ".....DDDDD......",
        "................",
        "................",
    ]),
}

def png(width, height, rows_rgba):
    raw = b"".join(b"\x00" + bytes(row) for row in rows_rgba)
    def chunk(kind, data):
        c = kind + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))

def render(spec, scale=32):
    rows = spec["rows"]
    assert len(rows) == 16 and all(len(r) == 16 for r in rows), "every animal is 16 by 16"
    colors = {".": spec["bg"], "B": spec["B"], "D": spec["D"], "L": spec["L"], "A": spec["A"], "E": INK, "W": WHITE}
    out = []
    for r in rows:
        line = bytearray()
        for ch in r:
            rgb = colors[ch]
            line += bytes(rgb) * scale + b""  # placeholder, fixed below
        # rebuild with alpha
        line = bytearray()
        for ch in r:
            line += (bytes(colors[ch]) + b"\xff") * scale
        for _ in range(scale):
            out.append(line)
    return png(16 * scale, 16 * scale, out)

def main():
    root = pathlib.Path(__file__).resolve().parents[2] / "apps/mobile/Monaco/Assets.xcassets/Avatars"
    root.mkdir(parents=True, exist_ok=True)
    (root / "Contents.json").write_text('{\n  "info" : {\n    "author" : "xcode",\n    "version" : 1\n  }\n}\n')
    for name, spec in ANIMALS.items():
        folder = root / f"avatar-{name}.imageset"
        folder.mkdir(exist_ok=True)
        (folder / f"avatar-{name}.png").write_bytes(render(spec))
        (folder / "Contents.json").write_text(
            '{\n  "images" : [\n    {\n      "filename" : "avatar-%s.png",\n      "idiom" : "universal"\n    }\n  ],\n  "info" : {\n    "author" : "xcode",\n    "version" : 1\n  }\n}\n' % name)
    print(f"drew {len(ANIMALS)} animals into {root}")
    if len(sys.argv) > 1:
        sheet = pathlib.Path(sys.argv[1]); sheet.parent.mkdir(parents=True, exist_ok=True)
        # a contact sheet for review: eight animals in a row at 128px, drawn as one PNG
        scale = 8
        rows_out = []
        for y in range(16 * scale):
            line = bytearray()
            for name, spec in ANIMALS.items():
                colors = {".": spec["bg"], "B": spec["B"], "D": spec["D"], "L": spec["L"], "A": spec["A"], "E": INK, "W": WHITE}
                r = spec["rows"][y // scale]
                for ch in r:
                    line += (bytes(colors[ch]) + b"\xff") * scale
                line += (bytes((248, 245, 238)) + b"\xff") * 8
            rows_out.append(line)
        sheet.write_bytes(png(len(rows_out[0]) // 4, len(rows_out), rows_out))
        print("sheet:", sheet)

if __name__ == "__main__":
    main()
