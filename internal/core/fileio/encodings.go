package fileio

import (
	"sort"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// Encoding ids of the encodings that have a byte order mark.
const (
	UTF8    = "utf8"
	UTF8BOM = "utf8bom"
	UTF16LE = "utf16le"
	UTF16BE = "utf16be"
)

// Encoding is a text encoding offered in VS Code's "files.encoding" list: its id
// (the value of the setting), its labels and its place in the list.
type Encoding struct {
	// ID is VS Code's id, such as "utf8", "windows1252" or "shiftjis".
	ID string
	// LabelLong is the label of the encoding picker, such as "Western (Windows 1252)".
	LabelLong string
	// LabelShort is the label of the status bar, such as "Windows 1252".
	LabelShort string
	// Order is the place in VS Code's list, from 1.
	Order int
	// EncodeOnly means the encoding is offered for saving but is not one a file
	// is read as: "utf8bom" is what a file with a UTF-8 byte order mark is
	// detected as, but "Reopen with Encoding" does not list it.
	EncodeOnly bool

	// enc is the codec of golang.org/x/text; it is nil for the two UTF-8
	// encodings, which Emetos handles itself.
	enc encoding.Encoding
}

// encodings is VS Code's SUPPORTED_ENCODINGS (workbench/services/textfile/common/encoding.ts)
// without the ones golang.org/x/text has no codec for: cp1125 (Cyrillic CP 1125),
// cp857 (Turkish CP 857), big5hkscs (Big5-HKSCS), iso885911 (Latin/Thai ISO
// 8859-11), koi8ru (KOI8-RU), koi8t (KOI8-T) and gb2312 (GB 2312).
var encodings = []Encoding{
	{ID: UTF8, LabelLong: "UTF-8", LabelShort: "UTF-8", Order: 1},
	{ID: UTF8BOM, LabelLong: "UTF-8 with BOM", LabelShort: "UTF-8 with BOM", Order: 2, EncodeOnly: true},
	{ID: UTF16LE, LabelLong: "UTF-16 LE", LabelShort: "UTF-16 LE", Order: 3, enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)},
	{ID: UTF16BE, LabelLong: "UTF-16 BE", LabelShort: "UTF-16 BE", Order: 4, enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)},
	{ID: "windows1252", LabelLong: "Western (Windows 1252)", LabelShort: "Windows 1252", Order: 5, enc: charmap.Windows1252},
	{ID: "iso88591", LabelLong: "Western (ISO 8859-1)", LabelShort: "ISO 8859-1", Order: 6, enc: charmap.ISO8859_1},
	{ID: "iso88593", LabelLong: "Western (ISO 8859-3)", LabelShort: "ISO 8859-3", Order: 7, enc: charmap.ISO8859_3},
	{ID: "iso885915", LabelLong: "Western (ISO 8859-15)", LabelShort: "ISO 8859-15", Order: 8, enc: charmap.ISO8859_15},
	{ID: "macroman", LabelLong: "Western (Mac Roman)", LabelShort: "Mac Roman", Order: 9, enc: charmap.Macintosh},
	{ID: "cp437", LabelLong: "DOS (CP 437)", LabelShort: "CP437", Order: 10, enc: charmap.CodePage437},
	{ID: "windows1256", LabelLong: "Arabic (Windows 1256)", LabelShort: "Windows 1256", Order: 11, enc: charmap.Windows1256},
	{ID: "iso88596", LabelLong: "Arabic (ISO 8859-6)", LabelShort: "ISO 8859-6", Order: 12, enc: charmap.ISO8859_6},
	{ID: "windows1257", LabelLong: "Baltic (Windows 1257)", LabelShort: "Windows 1257", Order: 13, enc: charmap.Windows1257},
	{ID: "iso88594", LabelLong: "Baltic (ISO 8859-4)", LabelShort: "ISO 8859-4", Order: 14, enc: charmap.ISO8859_4},
	{ID: "iso885914", LabelLong: "Celtic (ISO 8859-14)", LabelShort: "ISO 8859-14", Order: 15, enc: charmap.ISO8859_14},
	{ID: "windows1250", LabelLong: "Central European (Windows 1250)", LabelShort: "Windows 1250", Order: 16, enc: charmap.Windows1250},
	{ID: "iso88592", LabelLong: "Central European (ISO 8859-2)", LabelShort: "ISO 8859-2", Order: 17, enc: charmap.ISO8859_2},
	{ID: "cp852", LabelLong: "Central European (CP 852)", LabelShort: "CP 852", Order: 18, enc: charmap.CodePage852},
	{ID: "windows1251", LabelLong: "Cyrillic (Windows 1251)", LabelShort: "Windows 1251", Order: 19, enc: charmap.Windows1251},
	{ID: "cp866", LabelLong: "Cyrillic (CP 866)", LabelShort: "CP 866", Order: 20, enc: charmap.CodePage866},
	{ID: "iso88595", LabelLong: "Cyrillic (ISO 8859-5)", LabelShort: "ISO 8859-5", Order: 22, enc: charmap.ISO8859_5},
	{ID: "koi8r", LabelLong: "Cyrillic (KOI8-R)", LabelShort: "KOI8-R", Order: 23, enc: charmap.KOI8R},
	{ID: "koi8u", LabelLong: "Cyrillic (KOI8-U)", LabelShort: "KOI8-U", Order: 24, enc: charmap.KOI8U},
	{ID: "iso885913", LabelLong: "Estonian (ISO 8859-13)", LabelShort: "ISO 8859-13", Order: 25, enc: charmap.ISO8859_13},
	{ID: "windows1253", LabelLong: "Greek (Windows 1253)", LabelShort: "Windows 1253", Order: 26, enc: charmap.Windows1253},
	{ID: "iso88597", LabelLong: "Greek (ISO 8859-7)", LabelShort: "ISO 8859-7", Order: 27, enc: charmap.ISO8859_7},
	{ID: "windows1255", LabelLong: "Hebrew (Windows 1255)", LabelShort: "Windows 1255", Order: 28, enc: charmap.Windows1255},
	{ID: "iso88598", LabelLong: "Hebrew (ISO 8859-8)", LabelShort: "ISO 8859-8", Order: 29, enc: charmap.ISO8859_8},
	{ID: "iso885910", LabelLong: "Nordic (ISO 8859-10)", LabelShort: "ISO 8859-10", Order: 30, enc: charmap.ISO8859_10},
	{ID: "iso885916", LabelLong: "Romanian (ISO 8859-16)", LabelShort: "ISO 8859-16", Order: 31, enc: charmap.ISO8859_16},
	{ID: "windows1254", LabelLong: "Turkish (Windows 1254)", LabelShort: "Windows 1254", Order: 32, enc: charmap.Windows1254},
	{ID: "iso88599", LabelLong: "Turkish (ISO 8859-9)", LabelShort: "ISO 8859-9", Order: 33, enc: charmap.ISO8859_9},
	{ID: "windows1258", LabelLong: "Vietnamese (Windows 1258)", LabelShort: "Windows 1258", Order: 35, enc: charmap.Windows1258},
	{ID: "gbk", LabelLong: "Simplified Chinese (GBK)", LabelShort: "GBK", Order: 36, enc: simplifiedchinese.GBK},
	{ID: "gb18030", LabelLong: "Simplified Chinese (GB18030)", LabelShort: "GB18030", Order: 37, enc: simplifiedchinese.GB18030},
	{ID: "cp950", LabelLong: "Traditional Chinese (Big5)", LabelShort: "Big5", Order: 38, enc: traditionalchinese.Big5},
	{ID: "shiftjis", LabelLong: "Japanese (Shift JIS)", LabelShort: "Shift JIS", Order: 40, enc: japanese.ShiftJIS},
	{ID: "eucjp", LabelLong: "Japanese (EUC-JP)", LabelShort: "EUC-JP", Order: 41, enc: japanese.EUCJP},
	{ID: "euckr", LabelLong: "Korean (EUC-KR)", LabelShort: "EUC-KR", Order: 42, enc: korean.EUCKR},
	{ID: "windows874", LabelLong: "Thai (Windows 874)", LabelShort: "Windows 874", Order: 43, enc: charmap.Windows874},
	{ID: "cp865", LabelLong: "Nordic DOS (CP 865)", LabelShort: "CP 865", Order: 48, enc: charmap.CodePage865},
	{ID: "cp850", LabelLong: "Western European DOS (CP 850)", LabelShort: "CP 850", Order: 49, enc: charmap.CodePage850},
}

func init() {
	sort.SliceStable(encodings, func(i, j int) bool { return encodings[i].Order < encodings[j].Order })
}

// Encodings returns every encoding Emetos can read and write, in the order of
// VS Code's list. The slice is a copy.
func Encodings() []Encoding {
	return append([]Encoding(nil), encodings...)
}

// LookupEncoding finds an encoding by its VS Code id.
func LookupEncoding(id string) (Encoding, bool) {
	for _, e := range encodings {
		if e.ID == id {
			return e, true
		}
	}
	return Encoding{}, false
}
