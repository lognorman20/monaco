#!/usr/bin/env bash
# compose.sh [beat ...] — cuts the demo film from docs/demo/clips/*.mov and the beat list below.
#
# Produces docs/demo/out/monaco-demo-vertical.mp4 (1080x1920) and
# docs/demo/out/monaco-demo-site.mp4 (1920x1080: the phone on paper, the caption beside it).
# Needs ffmpeg and ImageMagick (`magick`); all type is set by ImageMagick and overlaid, so
# ffmpeg needs no text filter. Clips that are missing are skipped, so the film can be cut
# while it is still being shot. Naming beats on the command line cuts only those, without
# the title cards, which is the quick way to check one.
#
# A beat is: clip name | speed | caption | segments. A take is recorded at the pace the
# driver taps, with dead air between actions; the segments (in-out, in seconds of the
# take, comma separated) keep the moments that matter and the speed plays them a little
# faster than life, so a beat lands in the seconds the storyboard gives it. An empty
# caption keeps the frame clean. One take can carry two beats with their own captions:
# "start-cabal#name" and "start-cabal#rules" both read docs/demo/clips/start-cabal.mov.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
clips="$root/docs/demo/clips"
out="$root/docs/demo/out"
work="$out/work"
mkdir -p "$work"
font="${MONACO_DEMO_FONT:-/System/Library/Fonts/Avenir Next.ttc}"
icon="$root/apps/mobile/Monaco/Assets.xcassets/AppIcon.appiconset/AppIcon-Light.png"
paper="#F8F5EE"; ink="#0F291C"; muted="#55645B"
fps=30

beats=(
  "login|1.4|Sign in|12.5-15.5,24-26.5,33-37.5"
  "onboarding|1.3|Create your profile|5-7.5,11-13.5,17.5-21.5"
  "start-cabal#name|1.5|Start a cabal|6.5-9,15.5-18.5"
  "start-cabal#rules|1.5|Set the rules|29.5-32.5,44-46.5,51-53.5,58.5-63.5"
  "invite-join|1.2|Invite your friends with a code|5.5-8.5,19.5-23"
  "join-jordan|1.5|A paste and they're in!|7.5-10.5,19.5-22.5,24-27,37.5-40.5,51.5-54.5"
  "fund|1.3|Fund the pot|5.5-8.5,20.5-23.5,25.5-27"
  "join-priya|1.5|Anyone with the code can join|5.5-8.5,14.5-17.5,22-25,38-39.9,46.5-50.5"
  "fund-priya|1.4|Everyone owns a slice|11.5-14.5,16.8-17.9,20.6-23.2,27-30.5"
  "fund-maya|1.3||28.5-31.5,33.2-34.3,35-37.8"
  "stocks|1.5|Browse real stocks|3-6.5,9.5-20,23-25.2"
  "propose|1.6|Propose a buy|3.5-6.5,8.5-11,17.5-22,25.5-28.5,31.5-35,37.5-41"
  "vote-priya|1.2|Everyone votes|5-9,16.5-20.5"
  "vote-maya|1.3|Majority wins, the cabal buys|4-6,8.5-10.5,16.5-20.5,42.5-46"
  "chat-jordan|1.3|Talk it over|8.5-11,20.5-23,30-33.5"
  "chat-maya|1.3||2-5,15.5-20"
  "pre-ipo|1.3|Pre-IPO too|3-6,12-15,23.5-27.5,35-38.5"
  "bot|1.4|Add a trading bot|6.5-9.5,13-15.5,23.5-26,32.5-35,39.5-43.5"
  "vote-bot|1.2||0-5"
  "bot-key|1.4|Connect it to ClawPump|2.5-5,24-27,38.5-41.5,55.5-59.5"
  "bot-activity|1.2|Watch it trade|0.3-3.3,4-7"
  "profit|1.3|Cash out any time|1-4,25-28,34-38.5,55.8-57.5"
  "board-outro|1.2|See who's up|1.5-5,15.5-20.7"
)

# A title card as one PNG: the icon, the wordmark, one line. Type is ImageMagick's.
card_png() { # card_png <out.png> <W> <H> <title> <line>
  local o=$1 w=$2 h=$3 title=$4 line=$5
  magick -size "${w}x${h}" "xc:$paper" \
    \( "$icon" -resize 220x220 \( -size 220x220 xc:none -fill white -draw "roundrectangle 0,0 219,219 48,48" \) -compose DstIn -composite \) -gravity center -geometry "+0-200" -compose Over -composite \
    -font "$font" -fill "$ink" -pointsize 96 -gravity center -annotate "+0-10" "$title" \
    -font "$font" -fill "$muted" -pointsize 44 -gravity center -annotate "+0+110" "$line" \
    "$o"
}

# The card as video: fades in from the paper and out to it. The PNG is decoded to RGB
# before the fade; ImageMagick writes it with a palette, and fading a palette image
# turned every glyph into a block.
card() { # card <out.mp4> <W> <H> <seconds> <title> <line>
  local o=$1 w=$2 h=$3 s=$4
  card_png "$work/card-$(basename "$o" .mp4).png" "$w" "$h" "$5" "$6"
  ffmpeg -y -loglevel error -loop 1 -i "$work/card-$(basename "$o" .mp4).png" -t "$s" -r $fps \
    -vf "format=rgb24,fade=t=in:st=0:d=0.5:color=$paper,fade=t=out:st=$(echo "$s-0.5" | bc):d=0.5:color=$paper,format=yuv420p" -pix_fmt yuv420p "$o"
}

# A caption as a transparent PNG the size of the frame, wrapped to the column it sits in.
caption_png() { # caption_png <out.png> <W> <H> <text> <mode: below|side>
  local o=$1 w=$2 h=$3 text=$4 mode=$5
  if [[ "$mode" == "below" ]]; then
    magick -size "${w}x${h}" xc:none \
      -font "$font" -fill "$ink" -pointsize 54 -interline-spacing 6 -size 940x -background none -gravity center \
      caption:"$text" -gravity south -geometry "+0+96" -composite "$o"
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

# The take's segments, played at the beat's speed and joined, as one filter chain from
# input label $1 to output label $4. Prints the chain; the seconds it lasts land in $dur.
dur=0
cut_filter() { # cut_filter <input label> <segments> <speed> <output label>
  local in=$1 segs=$2 speed=$3 outl=$4 k=0 split="" chain="" join=""
  local -a parts
  IFS=',' read -ra parts <<<"$segs"
  dur=0
  for p in "${parts[@]}"; do
    local a=${p%-*} b=${p#*-}
    split+="[i$k]"
    chain+="[i$k]trim=start=$a:end=$b,setpts=(PTS-STARTPTS)/$speed[s$k];"
    join+="[s$k]"
    dur=$(echo "$dur + ($b - $a) / $speed" | bc -l)
    k=$((k + 1))
  done
  printf '%ssplit=%d%s;%s%sconcat=n=%d:v=1:a=0[%s];' "$in" "$k" "$split" "$chain" "$join" "$k" "$outl"
}

# One beat, vertical: the phone with rounded corners on an ink bezel, centred on the paper
# with the caption below it. The take is 402 by 874 points, narrower than 9:16, so filling
# the frame would cut the status bar and the tab bar off; the paper takes the difference.
beat_vertical() { # <clip> <speed> <caption> <segments> <out>
  local c=$1 speed=$2 cap=$3 segs=$4 o=$5
  local pw=690 ph=1500 px=195 py=120
  [[ -f "$work/mask-phone-v.png" ]] || magick -size "${pw}x${ph}" xc:none -fill white -draw "roundrectangle 0,0 $((pw - 1)),$((ph - 1)) 76,76" "$work/mask-phone-v.png"
  [[ -f "$work/paper-vertical.png" ]] || magick -size 1080x1920 "xc:$paper" -fill "$ink" -draw "roundrectangle $((px - 16)),$((py - 16)) $((px + pw + 15)),$((py + ph + 15)) 92,92" "$work/paper-vertical.png"
  local cut; cut=$(cut_filter "[1:v]" "$segs" "$speed" "cut")
  local phone="[cut]scale=${pw}:${ph}:force_original_aspect_ratio=increase,crop=${pw}:${ph},setsar=1,fps=$fps[p];[2:v]format=gray[m];[p][m]alphamerge[phone];[0:v][phone]overlay=${px}:${py}:shortest=1"
  if [[ -z "$cap" ]]; then
    ffmpeg -y -loglevel error -loop 1 -t "$dur" -i "$work/paper-vertical.png" -i "$c" -loop 1 -t "$dur" -i "$work/mask-phone-v.png" \
      -filter_complex "${cut}${phone}[out]" -map "[out]" -pix_fmt yuv420p -an "$o"
    return
  fi
  caption_png "$work/cap-$(basename "$o" .mp4).png" 1080 1920 "$cap" below
  ffmpeg -y -loglevel error -loop 1 -t "$dur" -i "$work/paper-vertical.png" -i "$c" -loop 1 -t "$dur" -i "$work/mask-phone-v.png" -loop 1 -t "$dur" -i "$work/cap-$(basename "$o" .mp4).png" \
    -filter_complex "${cut}${phone}[bg];[3:v]format=rgba,$(fade_expr "$dur")[c];[bg][c]overlay=0:0:shortest=1[out]" \
    -map "[out]" -pix_fmt yuv420p -an "$o"
}

# One beat, site: the phone with rounded corners on an ink bezel, centred right; caption left.
beat_site() { # <clip> <speed> <caption> <segments> <out>
  local c=$1 speed=$2 cap=$3 segs=$4 o=$5
  local pw=396 ph=858 px=1188 py=105
  [[ -f "$work/mask-phone.png" ]] || magick -size "${pw}x${ph}" xc:none -fill white -draw "roundrectangle 0,0 $((pw - 1)),$((ph - 1)) 44,44" "$work/mask-phone.png"
  [[ -f "$work/paper-site.png" ]] || magick -size 1920x1080 "xc:$paper" -fill "$ink" -draw "roundrectangle $((px - 12)),$((py - 12)) $((px + pw + 11)),$((py + ph + 11)) 56,56" "$work/paper-site.png"
  local cut; cut=$(cut_filter "[1:v]" "$segs" "$speed" "cut")
  local phone="[cut]scale=${pw}:${ph}:force_original_aspect_ratio=increase,crop=${pw}:${ph},setsar=1,fps=$fps[p];[2:v]format=gray[m];[p][m]alphamerge[phone];[0:v][phone]overlay=${px}:${py}:shortest=1"
  if [[ -z "$cap" ]]; then
    ffmpeg -y -loglevel error -loop 1 -t "$dur" -i "$work/paper-site.png" -i "$c" -loop 1 -t "$dur" -i "$work/mask-phone.png" \
      -filter_complex "${cut}${phone}[out]" -map "[out]" -pix_fmt yuv420p -an "$o"
    return
  fi
  caption_png "$work/cap-$(basename "$o" .mp4).png" 1920 1080 "$cap" side
  ffmpeg -y -loglevel error -loop 1 -t "$dur" -i "$work/paper-site.png" -i "$c" -loop 1 -t "$dur" -i "$work/mask-phone.png" -loop 1 -t "$dur" -i "$work/cap-$(basename "$o" .mp4).png" \
    -filter_complex "${cut}${phone}[bg];[3:v]format=rgba,$(fade_expr "$dur")[c];[bg][c]overlay=0:0:shortest=1[out]" \
    -map "[out]" -pix_fmt yuv420p -an "$o"
}

# The segments' duration has to be known before ffmpeg runs (the caption loops for it), so
# the cut filter is built once here and its $dur reused by beat_vertical / beat_site.
assemble() { # assemble <suffix> <W> <H> [beat ...]
  local suffix=$1 w=$2 h=$3; shift 3
  local only=("$@") parts=()
  if [[ ${#only[@]} -eq 0 ]]; then
    card "$work/intro-$suffix.mp4" "$w" "$h" 4 "Monaco" "The hedge fund with your friends."
    parts+=("$work/intro-$suffix.mp4")
  fi
  for beat in "${beats[@]}"; do
    IFS='|' read -r name speed cap segs <<<"$beat"
    local take="${name%%#*}"
    if [[ ${#only[@]} -gt 0 ]] && [[ ! " ${only[*]} " == *" $take "* ]]; then continue; fi
    local clip="$clips/$take.mov"
    if [[ ! -f "$clip" ]]; then echo "skip $name (no clip)"; continue; fi
    local o="$work/${name//#/-}-$suffix.mp4"
    cut_filter "[0:v]" "$segs" "$speed" "cut" >/dev/null
    # MONACO_DEMO_REUSE=1 keeps a beat already rendered from the same take, so a change
    # to the cards or the join does not re-encode every beat.
    if [[ "${MONACO_DEMO_REUSE:-0}" == "1" && -f "$o" && "$o" -nt "$clip" ]]; then
      printf '  %-12s %5.1fs (kept)\n' "$name" "$dur"; parts+=("$o"); continue
    fi
    if [[ "$suffix" == "vertical" ]]; then beat_vertical "$clip" "$speed" "$cap" "$segs" "$o"; else beat_site "$clip" "$speed" "$cap" "$segs" "$o"; fi
    printf '  %-12s %5.1fs\n' "$name" "$dur"
    parts+=("$o")
  done
  if [[ ${#only[@]} -eq 0 ]]; then
    card "$work/outro-$suffix.mp4" "$w" "$h" 4 "Monaco" "trymonaco.xyz"
    parts+=("$work/outro-$suffix.mp4")
  fi

  # Cross-dissolve every join by 0.3s. Every part is put on one timebase first: the
  # cards come from a looped PNG and the beats from the recordings, and xfade refuses
  # inputs whose timebases differ.
  local n=${#parts[@]} inputs=() filter="" offset=0 prev="[n0]"
  for ((k=0; k<n; k++)); do inputs+=(-i "${parts[$k]}"); filter+="[$k:v]settb=AVTB,fps=$fps,format=yuv420p[n$k];"; done
  if [[ $n -eq 1 ]]; then
    cp "${parts[0]}" "$out/monaco-demo-$suffix.mp4"; echo "wrote $out/monaco-demo-$suffix.mp4 (1 part)"; return
  fi
  for ((k=1; k<n; k++)); do
    local d; d=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "${parts[$((k-1))]}")
    offset=$(echo "$offset + $d - 0.3" | bc)
    local label="[v$k]"; [[ $k -eq $((n-1)) ]] && label="[vout]"
    filter+="${prev}[n$k]xfade=transition=fade:duration=0.3:offset=$offset$label;"
    prev="$label"
  done
  filter="${filter%;}"
  local music="$root/docs/demo/music.m4a" audio=()
  if [[ -f "$music" ]] && [[ ${#only[@]} -eq 0 ]]; then audio=(-i "$music" -map "[vout]" -map "$n:a" -shortest -af "volume=0.35"); else audio=(-map "[vout]"); fi
  ffmpeg -y -loglevel error "${inputs[@]}" "${audio[@]}" -filter_complex "$filter" -r $fps -pix_fmt yuv420p -movflags +faststart "$out/monaco-demo-$suffix.mp4"
  echo "wrote $out/monaco-demo-$suffix.mp4 ($n parts, $(ffprobe -v error -show_entries format=duration -of csv=p=0 "$out/monaco-demo-$suffix.mp4" | cut -c1-5)s)"
}

assemble vertical 1080 1920 "$@"
assemble site 1920 1080 "$@"
