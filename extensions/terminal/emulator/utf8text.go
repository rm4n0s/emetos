package emulator

import "unicode/utf8"

// AppendValidUTF8 appends src to dst as UTF-8 text with every ill-formed sequence replaced by U+FFFD, one for each maximal
// subpart of an ill-formed sequence (Unicode 15, section 3.9, table 3-8): the same text the emulator reads when it is fed src,
// so a terminal that is fed the result draws the same cells as one fed src. A valid sequence that src ends in the middle of is
// not appended; held is the number of bytes of it at the end of src, to be put in front of the next chunk. A caller that has
// the end of the stream passes final to have it replaced with U+FFFD too.
func AppendValidUTF8(dst, src []byte, final bool) (out []byte, held int) {
	i := 0
	for i < len(src) {
		b := src[i]
		if b < utf8.RuneSelf {
			// a run of ASCII is copied as it is
			j := i + 1
			for j < len(src) && src[j] < utf8.RuneSelf {
				j++
			}
			dst = append(dst, src[i:j]...)
			i = j
			continue
		}
		need := utf8Len(b)
		if need == 0 {
			dst = utf8.AppendRune(dst, utf8.RuneError)
			i++
			continue
		}
		rest := src[i:]
		k := maximalPrefix(rest)
		switch {
		case k >= need:
			dst = append(dst, rest[:need]...)
			i += need
		case k == len(rest) && !final:
			return dst, len(rest) // the sequence goes on in the next chunk
		default:
			dst = utf8.AppendRune(dst, utf8.RuneError)
			i += k
		}
	}
	return dst, 0
}
