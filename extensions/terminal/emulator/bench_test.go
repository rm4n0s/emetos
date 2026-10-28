package emulator

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The inputs of the throughput benchmarks. Each is built once, about 8 MiB, from a seeded generator, and fed in chunks of
// 64 KiB like the reads of the terminal's process.
const (
	benchRows = 45
	benchCols = 160
	chunkSize = 64 << 10
	dataSize  = 8 << 20
)

// plainASCII is text lines of varying length, as `cat` of a source file or a log writes them.
func plainASCII() []byte {
	rnd := rand.New(rand.NewSource(1))
	var sb strings.Builder
	words := []string{"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog", "func", "return", "err", "nil", "package", "import", "0123456789"}
	for sb.Len() < dataSize {
		n := 20 + rnd.Intn(120)
		line := 0
		for line < n {
			w := words[rnd.Intn(len(words))]
			sb.WriteString(w)
			sb.WriteByte(' ')
			line += len(w) + 1
		}
		sb.WriteString("\r\n")
	}
	return []byte(sb.String())
}

// colorful changes the color every 8 characters, with the sequences a syntax-highlighting program or `ls --color` writes: the
// 256-color and the 16-color forms, bold, and true color taken from a theme of 64 colors.
func colorful() []byte {
	rnd := rand.New(rand.NewSource(2))
	theme := make([][3]int, 64)
	for i := range theme {
		theme[i] = [3]int{rnd.Intn(256), rnd.Intn(256), rnd.Intn(256)}
	}
	var sb strings.Builder
	const alpha = "abcdefghijklmnopqrstuvwxyz0123456789 "
	col := 0
	for sb.Len() < dataSize {
		switch rnd.Intn(5) {
		case 0, 1:
			fmt.Fprintf(&sb, "\x1b[38;5;%dm", rnd.Intn(256))
		case 2, 3:
			fmt.Fprintf(&sb, "\x1b[%d;%dm", rnd.Intn(2), 30+rnd.Intn(8))
		default:
			c := theme[rnd.Intn(len(theme))]
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", c[0], c[1], c[2])
		}
		for i := 0; i < 8; i++ {
			sb.WriteByte(alpha[rnd.Intn(len(alpha))])
		}
		col += 8
		if col >= 120 {
			sb.WriteString("\x1b[0m\r\n")
			col = 0
		}
	}
	return []byte(sb.String())
}

// rainbow is the worst case for the table of styles: a new true color every 8 characters, never repeated (a `lolcat`).
func rainbow() []byte {
	var sb strings.Builder
	col := 0
	for i := 0; sb.Len() < dataSize; i++ {
		fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", i%256, (i/256)%256, (i/65536)%256)
		sb.WriteString("abcdefgh")
		col += 8
		if col >= 120 {
			sb.WriteString("\x1b[0m\r\n")
			col = 0
		}
	}
	return []byte(sb.String())
}

// cjkEmoji is UTF-8 text of wide characters, emoji and some Latin.
func cjkEmoji() []byte {
	rnd := rand.New(rand.NewSource(3))
	var sb strings.Builder
	pool := []rune("日本語のテキスト中文汉字한국어あいうえおカタカナ😀🎉🚀✨ abcdefg é")
	col := 0
	for sb.Len() < dataSize {
		r := pool[rnd.Intn(len(pool))]
		sb.WriteRune(r)
		col += runeWidth(r)
		if col >= 100 {
			sb.WriteString("\r\n")
			col = 0
		}
	}
	return []byte(sb.String())
}

// regionScroll scrolls a region of the screen with line feeds, the way a pager or an editor with a status line does.
func regionScroll() []byte {
	var sb strings.Builder
	sb.WriteString("\x1b[2;44r\x1b[44;1H")
	for i := 0; sb.Len() < dataSize; i++ {
		fmt.Fprintf(&sb, "line number %d of the scrolling region with some more text to fill it\n", i)
	}
	return []byte(sb.String())
}

// cursorMotion moves the cursor all over the screen and writes a few characters, as a full-screen program redraws.
func cursorMotion() []byte {
	rnd := rand.New(rand.NewSource(4))
	var sb strings.Builder
	for sb.Len() < dataSize {
		fmt.Fprintf(&sb, "\x1b[%d;%dHx\x1b[%dC\x1b[%dBy\x1b[%dD\x1b[%dAz", 1+rnd.Intn(benchRows), 1+rnd.Intn(benchCols), rnd.Intn(5), rnd.Intn(3), rnd.Intn(5), rnd.Intn(3))
	}
	return []byte(sb.String())
}

// feedAll writes data in chunks and returns the time it took.
func feedAll(e *Emulator, data []byte) time.Duration {
	start := time.Now()
	for off := 0; off < len(data); off += chunkSize {
		end := min(off+chunkSize, len(data))
		if _, err := e.Write(data[off:end]); err != nil {
			panic(err)
		}
	}
	return time.Since(start)
}

func benchmarkThroughput(b *testing.B, data []byte) {
	e := New(benchRows, benchCols, Options{Scrollback: 10000})
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		feedAll(e, data)
	}
}

func BenchmarkPlainASCII(b *testing.B)   { benchmarkThroughput(b, plainASCII()) }
func BenchmarkColorChange(b *testing.B)  { benchmarkThroughput(b, colorful()) }
func BenchmarkCJKAndEmoji(b *testing.B)  { benchmarkThroughput(b, cjkEmoji()) }
func BenchmarkRainbow(b *testing.B)      { benchmarkThroughput(b, rainbow()) }
func BenchmarkRegionScroll(b *testing.B) { benchmarkThroughput(b, regionScroll()) }
func BenchmarkCursorMotion(b *testing.B) { benchmarkThroughput(b, cursorMotion()) }
func BenchmarkSnapshotBusy(b *testing.B) {
	e := New(benchRows, benchCols, Options{Scrollback: 10000})
	data := plainASCII()
	var f Frame
	e.Snapshot(&f)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Write(data[:chunkSize]); err != nil {
			b.Fatal(err)
		}
		e.Snapshot(&f)
	}
}

func BenchmarkSnapshotIdle(b *testing.B) {
	e := New(benchRows, benchCols, Options{Scrollback: 10000})
	var f Frame
	e.Snapshot(&f)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Snapshot(&f)
	}
}

// mbps is the best throughput of runs runs over data, in MB/s (10^6 bytes, as the budgets of SPEC §14 count).
func mbps(data []byte, runs int) float64 {
	best := 0.0
	for i := 0; i < runs; i++ {
		e := New(benchRows, benchCols, Options{Scrollback: 10000})
		d := feedAll(e, data)
		if v := float64(len(data)) / d.Seconds() / 1e6; v > best {
			best = v
		}
	}
	return best
}

// TestThroughputBudgets holds the emulator to the budgets of SPEC §14 "Terminal throughput": a test fails when the best of
// several runs is below the budget. It is skipped under the race detector and with -short. The numbers are logged (go test
// -v -run Budgets) for docs/PERFORMANCE.md.
func TestThroughputBudgets(t *testing.T) {
	if raceEnabled {
		t.Skip("the budgets are for the build without the race detector")
	}
	if testing.Short() {
		t.Skip("budgets are measured in the full run")
	}
	cases := []struct {
		name   string
		data   func() []byte
		budget float64 // MB/s
	}{
		{"plain ASCII", plainASCII, 400},
		{"a color change every 8 characters", colorful, 100},
		{"CJK and emoji", cjkEmoji, 150},
		// SPEC §14 gives no budget for these two; the floors are Emetos's own (DECISIONS-TO-REVIEW 123).
		{"scrolling in a region", regionScroll, 200},
		{"cursor motion", cursorMotion, 60},
		{"a new true color every 8 characters", rainbow, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data()
			got := mbps(data, 5)
			t.Logf("%s: %.0f MB/s (budget %.0f)", tc.name, got, tc.budget)
			if got < tc.budget {
				t.Errorf("%.0f MB/s is below the budget of %.0f MB/s", got, tc.budget)
			}
		})
	}
}

// TestScrollbackMemory holds 10,000 lines of scrollback at 160 columns to 16 MB (SPEC §14 "Terminal memory").
func TestScrollbackMemory(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector changes the memory")
	}
	var before, after runtimeMem
	before.read()
	e := New(45, 160, Options{Scrollback: 10000})
	line := strings.Repeat("0123456789abcdef", 10) + "\r\n"
	for i := 0; i < 11000; i++ {
		if _, err := e.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if sb := e.Scrollback(); sb != 10000 {
		t.Fatalf("scrollback = %d", sb)
	}
	after.read()
	used := int64(after.heap) - int64(before.heap)
	t.Logf("10,000 lines of scrollback at 160 columns: %.2f MB of heap (limit 16 MB)", float64(used)/1e6)
	if used > 16_000_000 {
		t.Errorf("%d bytes for the scrollback, limit 16,000,000", used)
	}
	runtimeKeepAlive(e)
}
