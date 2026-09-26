#!/usr/bin/env bash
# feature-clip.sh <out.mp4> <part> ...
#   <take>:<start>-<end>[:<speed>]   a stretch of docs/demo/clips/<take>.mov, sped up (default 1.4)
#   still:<image.png>:<seconds>      a screenshot held for that long (a screen with nothing moving
#                                    records as no frames at all, so it is shown as a still)
# Joins the parts into one phone-only clip for showing a feature.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$1"; shift
inputs=(); filt=""; k=0
for spec in "$@"; do
  if [[ "$spec" == still:* ]]; then
    IFS=':' read -r _ image secs <<<"$spec"
    inputs+=(-loop 1 -t "$secs" -i "$image")
    filt+="[$k:v]scale=1206:2622:force_original_aspect_ratio=increase,crop=1206:2622,fps=30,format=yuv420p,settb=AVTB[p$k];"
  else
    IFS=':' read -r take range speed <<<"$spec"
    speed="${speed:-1.4}"
    a="${range%-*}"; b="${range#*-}"
    inputs+=(-i "$root/docs/demo/clips/$take.mov")
    filt+="[$k:v]trim=start=$a:end=$b,setpts=(PTS-STARTPTS)/$speed,scale=1206:2622,fps=30,format=yuv420p,settb=AVTB[p$k];"
  fi
  k=$((k + 1))
done
filt+=$(for ((i=0; i<k; i++)); do printf '[p%d]' "$i"; done)"concat=n=$k:v=1:a=0[v]"
ffmpeg -y -loglevel error "${inputs[@]}" -filter_complex "$filt" -map "[v]" -c:v libx264 -preset medium -crf 20 -pix_fmt yuv420p -movflags +faststart "$out"
echo "wrote $out ($(ffprobe -v error -show_entries format=duration -of csv=p=0 "$out" | cut -c1-5)s)"
