package ffmpeg

import (
	"os"
	"strconv"
	"strings"
)

// alive tells whether the process is running: a zombie waiting for its parent to read its end counts as gone.
func alive(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// the state follows the name, which is in parentheses
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || i+2 >= len(s) || s[i+2] != 'Z'
}
