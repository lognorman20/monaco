#!/usr/bin/env bash
# compose.sh — cuts the demo film from docs/demo/clips/*.mov and the beat list below.
#
# Produces docs/demo/out/monaco-demo-vertical.mp4 (1080x1920) and
# docs/demo/out/monaco-demo-site.mp4 (1920x1080: the phone on paper, the caption beside it).
# Needs ffmpeg and ImageMagick (`magick`); all type is set by ImageMagick and overlaid, so
# ffmpeg needs no text filter. Clips that are missing are skipped, so the film can be cut
# while it is still being shot.
#
# Beats: name | seconds | caption. A clip is trimmed to `seconds` from MONACO_DEMO_LEAD
# (default 1.0s, the stillness every take begins with).
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
clips="$root/docs/demo/clips"
out="$root/docs/demo/out"
work="$out/work"
mkdir -p "$work"
lead="${MONACO_DEMO_LEAD:-1.0}"
font="${MONACO_DEMO_FONT:-/System/Library/Fonts/Avenir Next.ttc}"
icon="$root/apps/mobile/Monaco/Assets.xcassets/AppIcon.appiconset/AppIcon-Light.png"
paper="#F8F5EE"; ink="#0F291C"; muted="#55645B"
fps=30

beats=(
  "login|6|Sign in with a text. No seed phrase."
  "start-cabal|10|Start a cabal. Set the rules once."
  "invite-join|10|Friends join with a code."
  "fund|9|Everyone puts money in the pot."
  "stocks|10|Real stocks, live prices. Tokenized on Solana."
  "propose|10|Propose a buy. Make the case."
  "vote|12|The cabal votes. A majority buys."
  "chat|6|Talk it through in the cabal."
  "pre-ipo|8|Pre-IPO names too."
  "bot|12|Or let a bot trade a budget."
  "profit|8|Take profit whenever you like."
  "board-outro|7|trymonaco.xyz"
)

# A title card as one PNG: the icon, the wordmark, one line. Type is ImageMagick's.
card_png() { # card_png <out.png> <W> <H> <title> <line>
  local o=$1 w=$2 h=$3 title=$4 line=$5
  magick -size "${w}x${h}" "xc:$paper" \
    \( "$icon" -resize 220x220 \( +clone -alpha extract -fill black -colorize 100 -fill white -draw "roundrectangle 0,0 219,219 48,48" \) -alpha off -compose CopyOpacity -composite \) -gravity center -geometry "+0-200" -composite \
    -font "$font" -fill "$ink" -pointsize 96 -gravity center -annotate "+0-10" "$title" \
    -font "$font" -fill "$muted" -pointsize 44 -gravity center -annotate "+0+110" "$line" \
    "$o"
}

# The card as video: fades in and out on the paper.
card() { # card <out.mp4> <W> <H> <seconds> <title> <line>
  local o=$1 w=$2 h=$3 s=$4
  card_png "$work/card-$(basename "$o" .mp4).png" "$w" "$h" "$5" "$6"
  ffmpeg -y -loglevel error -loop 1 -i "$work/card-$(basename "$o" .mp4).png" -t "$s" -r $fps \
    -vf "fade=t=in:st=0:d=0.5,fade=t=out:st=$(echo "$s-0.5" | bc):d=0.5" -pix_fmt yuv420p "$o"
}

# A caption as a transparent PNG the size of the frame.
caption_png() { # caption_png <out.png> <W> <H> <text> <mode: band|side>
  local o=$1 w=$2 h=$3 text=$4 mode=$5
  if [[ "$mode" == "band" ]]; then
    magick -size "${w}x${h}" xc:none \
      -fill "${paper}EB" -draw "rectangle 0,$((h - 260)) $w,$h" \
      -font "$font" -fill "$ink" -pointsize 52 -gravity south -annotate "+0+112" "$text" "$o"
  else
    magick -size "${w}x${h}" xc:none \
      -font "$font" -fill "$ink" -pointsize 60 -interline-spacing 10 -size 900x -background none \
      caption:"$text" -gravity west -geometry "+150+0" -composite "$o"
  fi
}

fade_expr() { # the overlay's alpha ramps in over the first half second and out before the cut
  local s=$1
  echo "fade=t=in:st=0.3:d=0.5:alpha=1,fade=t=out:st=$(echo "$s-0.7" | bc):d=0.6:alpha=1"
}

# One beat, vertical: the recording filled to 1080x1920, the caption band over the bottom.
beat_vertical() { # <clip> <seconds> <caption> <out>
  local c=$1 s=$2 cap=$3 o=$4
  caption_png "$work/cap-$(basename "$o" .mp4).png" 1080 1920 "$cap" band
  ffmpeg -y -loglevel error -ss "$lead" -t "$s" -i "$c" -loop 1 -t "$s" -i "$work/cap-$(basename "$o" .mp4).png" -r $fps \
    -filter_complex "[0:v]scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,setsar=1[v];[1:v]format=rgba,$(fade_expr "$s")[c];[v][c]overlay=0:0:shortest=1" \
    -pix_fmt yuv420p -an "$o"
}

# One beat, site: the phone with rounded corners on an ink bezel, centred right; caption left.
beat_site() { # <clip> <seconds> <caption> <out>
  local c=$1 s=$2 cap=$3 o=$4
  local pw=396 ph=858 px=1188 py=105
  [[ -f "$work/mask-phone.png" ]] || magick -size "${pw}x${ph}" xc:none -fill white -draw "roundrectangle 0,0 $((pw - 1)),$((ph - 1)) 44,44" "$work/mask-phone.png"
  [[ -f "$work/paper-site.png" ]] || magick -size 1920x1080 "xc:$paper" -fill "$ink" -draw "roundrectangle $((px - 12)),$((py - 12)) $((px + pw + 11)),$((py + ph + 11)) 56,56" "$work/paper-site.png"
  caption_png "$work/cap-$(basename "$o" .mp4).png" 1920 1080 "$cap" side
  ffmpeg -y -loglevel error -loop 1 -t "$s" -i "$work/paper-site.png" -ss "$lead" -t "$s" -i "$c" -loop 1 -t "$s" -i "$work/mask-phone.png" -loop 1 -t "$s" -i "$work/cap-$(basename "$o" .mp4).png" -r $fps \
    -filter_complex "[1:v]scale=${pw}:${ph}:force_original_aspect_ratio=increase,crop=${pw}:${ph},setsar=1[p];[2:v]format=gray[m];[p][m]alphamerge[phone];[0:v][phone]overlay=${px}:${py}:shortest=1[bg];[3:v]format=rgba,$(fade_expr "$s")[c];[bg][c]overlay=0:0:shortest=1" \
    -pix_fmt yuv420p -an "$o"
}

assemble() { # assemble <suffix> <W> <H>
  local suffix=$1 w=$2 h=$3 parts=()
  card "$work/intro-$suffix.mp4" "$w" "$h" 4 "Monaco" "The hedge fund with your friends."
  parts+=("$work/intro-$suffix.mp4")
  for beat in "${beats[@]}"; do
    IFS='|' read -r name secs cap <<<"$beat"
    local clip="$clips/$name.mov"
    if [[ ! -f "$clip" ]]; then echo "skip $name (no clip)"; continue; fi
    local o="$work/$name-$suffix.mp4"
    if [[ "$suffix" == "vertical" ]]; then beat_vertical "$clip" "$secs" "$cap" "$o"; else beat_site "$clip" "$secs" "$cap" "$o"; fi
    parts+=("$o")
  done
  card "$work/outro-$suffix.mp4" "$w" "$h" 4 "Monaco" "trymonaco.xyz"
  parts+=("$work/outro-$suffix.mp4")

  # Cross-dissolve every join by 0.3s.
  local n=${#parts[@]} inputs=() filter="" offset=0 prev="[0:v]"
  for ((k=0; k<n; k++)); do inputs+=(-i "${parts[$k]}"); done
  for ((k=1; k<n; k++)); do
    local d; d=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "${parts[$((k-1))]}")
    offset=$(echo "$offset + $d - 0.3" | bc)
    local label="[v$k]"; [[ $k -eq $((n-1)) ]] && label="[vout]"
    filter+="${prev}[$k:v]xfade=transition=fade:duration=0.3:offset=$offset$label;"
    prev="$label"
  done
  filter="${filter%;}"
  local music="$root/docs/demo/music.m4a" audio=()
  if [[ -f "$music" ]]; then audio=(-i "$music" -map "[vout]" -map "$n:a" -shortest -af "volume=0.35"); else audio=(-map "[vout]"); fi
  ffmpeg -y -loglevel error "${inputs[@]}" "${audio[@]}" -filter_complex "$filter" -r $fps -pix_fmt yuv420p -movflags +faststart "$out/monaco-demo-$suffix.mp4"
  echo "wrote $out/monaco-demo-$suffix.mp4 ($n parts)"
}

assemble vertical 1080 1920
assemble site 1920 1080
