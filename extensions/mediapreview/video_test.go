package mediapreview

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// videoRig is Media Preview over the fake API, with a fake FFmpeg, sound device and clock.
type videoRig struct {
	*rig
	media *fakeMedia
	sink  *fakeSink
	clock *fakeClock
	info  ffmpeg.Info
	// findErr is what finding FFmpeg answers; finds counts the finds.
	findErr error
	finds   int
}

func newVideoRig(t *testing.T) *videoRig {
	r := &videoRig{rig: newRig(t), media: &fakeMedia{duration: testInfo.Duration}, sink: &fakeSink{gain: 1}, clock: newFakeClock(), info: testInfo}
	r.x.findFFmpeg = func(context.Context, string) (ffmpeg.Programs, error) {
		r.finds++ // read on the UI thread only after the post
		return ffmpeg.Programs{FFmpeg: "/fake/ffmpeg", FFprobe: "/fake/ffprobe", Version: "6.1.1"}, r.findErr
	}
	r.x.probe = func(context.Context, ffmpeg.Programs, string) (ffmpeg.Info, error) { return r.info, nil }
	r.x.newMedia = func(ffmpeg.Programs, string) media { return r.media }
	r.x.newSink = func(context.Context, *widgets.WidgetsBinding) (sink, error) { return r.sink, nil }
	r.x.clockOf = func(*widgets.WidgetsBinding) clock { return r.clock }
	return r
}

// openVideo opens a video, shows it in a view of 480 x 320, and waits for its first frame.
func (r *videoRig) openVideo(state json.RawMessage, wrap func(widgets.Widget) widgets.Widget) (*fakeapi.FakeFileEditor, *videoTab, *gluttertest.Tester) {
	r.t.Helper()
	fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), state)
	if err != nil {
		r.t.Fatal(err)
	}
	v := r.x.videos[len(r.x.videos)-1]
	var child widgets.Widget = widgets.NewBuilder(fe.Content.Build)
	if wrap != nil {
		child = wrap(child)
	}
	ts := uitest.Pump(r.t, uitest.Themes()[0], gui.Size{Width: 480, Height: 320}, child)
	r.settle(ts, "the player", func() bool { return v.player != nil || v.problem != nil })
	if v.player != nil {
		r.settle(ts, "the first frame", func() bool { return v.player.frame != nil || v.player.err != nil || v.player.playing })
	}
	return fe, v, ts
}

// settle pumps the fake API and the tester until cond holds.
func (r *videoRig) settle(ts *gluttertest.Tester, what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.api.Pump()
		ts.Pump(time.Millisecond)
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// play plays d: the device plays it and the clock moves, in steps of 50 ms, with the queues filled between them.
func (r *videoRig) play(ts *gluttertest.Tester, v *videoTab, d time.Duration) {
	for d > 0 {
		step := min(d, 50*time.Millisecond)
		run := v.player.run
		if run != nil {
			r.settle(ts, "the queues", func() bool {
				return run.framesDone.Load() || len(run.frames) == cap(run.frames)
			})
			r.settle(ts, "the sound", func() bool { return run.soundDone.Load() || r.sink.Queued() > maxQueuedSound-50*time.Millisecond })
		}
		r.sink.Consume(step)
		r.clock.Advance(step)
		d -= step
	}
	ts.Pump(time.Millisecond)
}

func TestAVideoShowsItsFirstFrameBeforePlay(t *testing.T) {
	r := newVideoRig(t)
	_, v, ts := r.openVideo(nil, nil)
	if v.player.playing || v.player.frameAt != 0 {
		t.Errorf("playing %v, frame at %v", v.player.playing, v.player.frameAt)
	}
	if !ts.Exists(gluttertest.ByType[*widgets.RawImage]()) || !ts.Exists(gluttertest.ByText("0:00 / 0:02")) {
		t.Error("the frame or the time is not shown")
	}
	// the frames are decoded at the size of the picture in the view, never above the video's own (4 x 2)
	if v.player.o.width != 4 || v.player.o.height != 2 {
		t.Errorf("decoded at %d x %d", v.player.o.width, v.player.o.height)
	}
}

func TestTheKeysOfThePlayer(t *testing.T) {
	r := newVideoRig(t)
	_, v, ts := r.openVideo(nil, nil)
	p := v.player
	key := func(k services.LogicalKeyboardKey) {
		ts.SendKeyEvent(k)
		ts.Pump(time.Millisecond)
	}
	key(services.LogicalKeyboardKeySpace)
	if !p.playing {
		t.Fatal("Space did not play")
	}
	r.play(ts, v, 500*time.Millisecond)
	if got := p.Position(); got < 450*time.Millisecond || got > 550*time.Millisecond {
		t.Errorf("position %v after half a second", got)
	}
	key(services.LogicalKeyboardKeySpace)
	if p.playing {
		t.Error("Space did not pause")
	}
	key(services.LogicalKeyboardKeyArrowRight)
	if got := p.Position(); got < 1900*time.Millisecond {
		t.Errorf("Right: %v (held to the end of the 2 s video)", got)
	}
	key(services.LogicalKeyboardKeyHome)
	if p.Position() != 0 {
		t.Errorf("Home: %v", p.Position())
	}
	key(services.LogicalKeyboardKeyEnd)
	if p.Position() != 2*time.Second {
		t.Errorf("End: %v", p.Position())
	}
	key(services.LogicalKeyboardKeyArrowLeft)
	if p.Position() != 0 {
		t.Errorf("Left: %v", p.Position())
	}
	key(services.LogicalKeyboardKeyArrowDown)
	key(services.LogicalKeyboardKeyArrowDown)
	if p.volume < 0.79 || p.volume > 0.81 {
		t.Errorf("Down twice: volume %v", p.volume)
	}
	key(services.LogicalKeyboardKeyArrowUp)
	if p.volume < 0.89 || p.volume > 0.91 {
		t.Errorf("Up: volume %v", p.volume)
	}
	key(services.LogicalKeyboardKeyKeyM)
	if !p.muted {
		t.Error("M did not mute")
	}
}

func TestTheCommandsOfThePlayer(t *testing.T) {
	r := newVideoRig(t)
	fe, v, _ := r.openVideo(nil, nil)
	fe.SetActive(true)
	for _, c := range []struct {
		command string
		playing bool
	}{{CommandVideoPlay, true}, {CommandVideoPause, false}, {CommandVideoToggle, true}, {CommandVideoToggle, false}} {
		if err := r.api.Run(c.command, nil); err != nil {
			t.Fatal(err)
		}
		if v.player.playing != c.playing {
			t.Errorf("after %s: playing %v", c.command, v.player.playing)
		}
	}
}

func TestTheStateOfAVideoTab(t *testing.T) {
	r := newVideoRig(t)
	fe, v, _ := r.openVideo(nil, nil)
	v.player.SetVolume(0.4)
	v.player.SetMuted(true)
	v.player.Seek(1200 * time.Millisecond)
	s, err := fe.SaveState()
	if err != nil || string(s) != `{"position":1.2,"volume":0.4,"muted":true}` {
		t.Errorf("state %s %v", s, err)
	}
	_, v2, _ := r.openVideo(s, nil)
	if v2.player.Position() != 1200*time.Millisecond || v2.player.volume != 0.4 || !v2.player.muted || v2.player.frameAt != 1200*time.Millisecond {
		t.Errorf("restored: position %v, volume %v, muted %v, frame at %v", v2.player.Position(), v2.player.volume, v2.player.muted, v2.player.frameAt)
	}
}

func TestAutoPlayAndLoopFollowTheirSettings(t *testing.T) {
	r := newVideoRig(t)
	r.api.SetSetting(SettingAutoPlay, true)
	r.api.SetSetting(SettingLoop, true)
	_, v, _ := r.openVideo(nil, nil)
	// VS Code's autoplay starts muted
	if !v.player.playing || !v.player.muted || !v.player.loop {
		t.Errorf("playing %v, muted %v, loop %v", v.player.playing, v.player.muted, v.player.loop)
	}
	// a tab that comes back does not play by itself
	_, v2, _ := r.openVideo(json.RawMessage(`{"position":0.5}`), nil)
	if v2.player.playing {
		t.Error("a restored tab played")
	}
}

func TestAHiddenTabPauses(t *testing.T) {
	r := newVideoRig(t)
	_, v, ts := r.openVideo(nil, nil)
	v.player.Play()
	ts.PumpWidget(widgets.NewSizedBox(nil)) // the tab's widget goes
	if v.player.playing {
		t.Error("it plays on while hidden")
	}
}

func TestClosingAVideoTabEndsTheProcesses(t *testing.T) {
	r := newVideoRig(t)
	fe, v, ts := r.openVideo(nil, nil)
	v.player.Play()
	r.play(ts, v, 100*time.Millisecond)
	fe.Close()
	r.settle(ts, "the processes to end", func() bool { _, _, live := r.media.runs(); return live == 0 })
	if len(r.x.videos) != 0 {
		t.Errorf("%d video tabs left", len(r.x.videos))
	}
}

func TestAMissingFFmpeg(t *testing.T) {
	r := newVideoRig(t)
	r.findErr = errors.New("FFmpegNotFound", "ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin")
	_, v, ts := r.openVideo(nil, nil)
	if v.player != nil {
		t.Fatal("a player without FFmpeg")
	}
	want := "Playing videos needs FFmpeg. ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin."
	if !ts.Exists(gluttertest.ByText(want)) {
		t.Fatalf("no %q", want)
	}
	for _, b := range []string{"Set Path...", "Instructions", "Check Again"} {
		if !ts.Exists(gluttertest.ByText(b)) {
			t.Errorf("no button %s", b)
		}
	}
	if cmd, ok := requirements()[0].Install.CommandFor(extension.SystemFamily()); ok {
		ts.Tap(gluttertest.ByText("Copy Install Command"))
		ts.Pump(time.Millisecond)
		if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{cmd.Command}) {
			t.Errorf("copied %v", got)
		}
	}
	// FFmpeg installed meanwhile: Check Again finds it
	r.findErr = nil
	ts.Tap(gluttertest.ByText("Check Again"))
	r.settle(ts, "the player", func() bool { return v.player != nil })
	if r.finds != 2 {
		t.Errorf("FFmpeg was looked for %d times", r.finds)
	}
}

func TestACodecFFmpegCannotDecodeIsSaid(t *testing.T) {
	r := newVideoRig(t)
	r.media.frameErr = errors.New("FFmpegCodecUnsupported", "This FFmpeg cannot decode hevc.", "codec", "hevc")
	_, _, ts := r.openVideo(nil, nil)
	r.settle(ts, "the message", func() bool {
		return ts.Exists(gluttertest.ByText("This FFmpeg cannot decode hevc. Fedora's ffmpeg-free leaves it out; see the instructions."))
	})
	if !ts.Exists(gluttertest.ByText("Instructions")) || ts.Exists(gluttertest.ByText("Set Path...")) {
		t.Error("the buttons of a missing FFmpeg are shown for a codec")
	}
}

func TestADeletedVideo(t *testing.T) {
	r := newVideoRig(t)
	fe, v, ts := r.openVideo(nil, nil)
	v.player.Play()
	fe.Change(true)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("The file was deleted.")) || v.player != nil {
		t.Error("the deleted video still plays")
	}
	r.settle(ts, "the processes to end", func() bool { _, _, live := r.media.runs(); return live == 0 })
}

func TestAClientsViewPlaysMuted(t *testing.T) {
	r := newVideoRig(t)
	_, v, _ := r.openVideo(nil, func(w widgets.Widget) widgets.Widget { return uitest.OnScreen(extension.Origin(7), w) })
	if !v.player.forceMute || v.player.gain() != 0 {
		t.Errorf("force mute %v, gain %v", v.player.forceMute, v.player.gain())
	}
}

func TestGoldenVideoPlayer(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name+" first frame", func(t *testing.T) {
			r := newVideoRig(t)
			r.info.Width, r.info.Height = 320, 180
			fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), nil)
			if err != nil {
				t.Fatal(err)
			}
			v := r.x.videos[0]
			ts := uitest.Pump(t, th, gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the first frame", func() bool { return v.player != nil && v.player.frame != nil })
			uitest.MatchesGolden(ts, "video_first_frame_"+th.Name)
		})
		t.Run(th.Name+" playing", func(t *testing.T) {
			r := newVideoRig(t)
			r.info.Width, r.info.Height = 320, 180
			fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), nil)
			if err != nil {
				t.Fatal(err)
			}
			v := r.x.videos[0]
			ts := uitest.Pump(t, th, gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the first frame", func() bool { return v.player != nil && v.player.frame != nil })
			v.player.SetVolume(0.6)
			v.player.Play()
			r.play(ts, v, 700*time.Millisecond)
			uitest.MatchesGolden(ts, "video_playing_"+th.Name)
		})
		t.Run(th.Name+" codec", func(t *testing.T) {
			r := newVideoRig(t)
			r.media.frameErr = errors.New("FFmpegCodecUnsupported", "This FFmpeg cannot decode h264.", "codec", "h264")
			fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), nil)
			if err != nil {
				t.Fatal(err)
			}
			ts := uitest.Pump(t, th, gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the message", func() bool { return ts.Exists(gluttertest.ByText("Instructions")) })
			uitest.MatchesGolden(ts, "video_codec_"+th.Name)
		})
	}
}

func TestAChangeThatChangesNothingKeepsThePlayer(t *testing.T) {
	r := newVideoRig(t)
	fe, v, ts := r.openVideo(nil, nil)
	p := v.player
	fe.Change(false) // the watcher's first event: the file is the same
	r.settle(ts, "a frame", func() bool { return true })
	if v.player != p || r.finds != 1 || v.loading {
		t.Errorf("the player was made again: same %v, %d finds", v.player == p, r.finds)
	}
	// a real change reads the file again and keeps the position
	p.Seek(800 * time.Millisecond)
	r.write("clip.mp4", []byte("another video, longer"))
	fe.Change(false)
	r.settle(ts, "the new player", func() bool { return v.player != nil && v.player != p && v.player.frame != nil })
	if r.finds != 2 || v.player.Position() != 800*time.Millisecond {
		t.Errorf("%d finds, position %v", r.finds, v.player.Position())
	}
}

func TestPlayBeforeThePlayerIsReady(t *testing.T) {
	r := newVideoRig(t)
	fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), nil)
	if err != nil {
		t.Fatal(err)
	}
	fe.SetActive(true)
	v := r.x.videos[0]
	// FFmpeg is still being found: Play waits for the player
	if err := r.api.Run(CommandVideoPlay, nil); err != nil {
		t.Fatal(err)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
	r.settle(ts, "the player", func() bool { return v.player != nil })
	if !v.player.playing || v.player.muted {
		t.Errorf("playing %v, muted %v", v.player.playing, v.player.muted)
	}
}

// Space pressed before the player is made (the desktop test, step 36.9: a click on the picture and Space right after the tab opens)
// plays the video when it is, as the command does; a second Space before then takes it back.
func TestSpaceBeforeThePlayerIsReady(t *testing.T) {
	for _, tc := range []struct {
		name   string
		spaces int
		play   bool
	}{
		{"one Space plays", 1, true},
		{"two Spaces leave it paused", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newVideoRig(t)
			fe, err := r.api.OpenFileEditor(VideoEditorID, r.write("clip.mp4", []byte("a video")), nil)
			if err != nil {
				t.Fatal(err)
			}
			fe.SetActive(true)
			v := r.x.videos[0]
			ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
			if v.player != nil {
				t.Fatal("the player was made before FFmpeg was found")
			}
			for range tc.spaces {
				ts.SendKeyEvent(services.LogicalKeyboardKeySpace)
				ts.Pump(time.Millisecond)
			}
			r.settle(ts, "the player", func() bool { return v.player != nil })
			if v.player.playing != tc.play {
				t.Errorf("playing %v, want %v", v.player.playing, tc.play)
			}
		})
	}
}

func TestAFileWithNoStreams(t *testing.T) {
	r := newVideoRig(t)
	r.info = ffmpeg.Info{Duration: time.Second}
	_, v, _ := r.openVideo(nil, nil)
	appErr, ok := errors.FromError(v.problem)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", v.problem)
	}
	if !appErr.HasRoute("videoTab.loaded.VideoHasNoStreams") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if v.player != nil {
		t.Error("a player was made")
	}
}

func TestASystemWithNoSound(t *testing.T) {
	var binding *widgets.WidgetsBinding
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 100, Height: 100}, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		binding = widgets.WidgetsBindingOf(bc)
		return widgets.NewSizedBox(nil)
	}))
	ts.Audio().SetUnavailable(true)
	s, err := openAudioSink(t.Context(), binding, nil)
	if s != nil {
		s.Close()
		t.Error("a sink was opened")
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("openAudioSink.VideoSoundUnavailable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
