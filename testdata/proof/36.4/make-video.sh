#!/bin/sh
# Makes the video of the proof of step 36.4: 10 s of FFmpeg's test pattern with a 440 Hz sine, in MPEG-4 Part 2 and AAC, which every
# FFmpeg encodes (Fedora's ffmpeg-free too).
# Usage: testdata/proof/36.4/make-video.sh <dir>
set -eu
dir=${1:?usage: make-video.sh <dir>}
mkdir -p "$dir"
ffmpeg -hide_banner -loglevel error -nostdin -y \
	-f lavfi -i testsrc=duration=10:size=640x360:rate=25 \
	-f lavfi -i sine=frequency=440:duration=10 \
	-c:v mpeg4 -q:v 4 -c:a aac -shortest "$dir/test.mp4"
echo "$dir/test.mp4"
