package workspace

import "testing"

// TestIncrementFileName has the 52 cases of the tests of incrementFileName in VS Code
// (src/vs/workbench/contrib/files/test/browser/fileActions.test.ts); the large number 9007199254740992
// is beyond the limit of both implementations.
func TestIncrementFileName(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		folder bool
		naming IncrementalNaming
		want   string
	}{
		{"Increment file name without any version", "test.js", false, NamingSimple, "test copy.js"},
		{"Increment file name with suffix version", "test copy.js", false, NamingSimple, "test copy 2.js"},
		{"Increment file name with suffix version with leading zeros", "test copy 005.js", false, NamingSimple, "test copy 6.js"},
		{"Increment file name with suffix version, too big number", "test copy 9007199254740992.js", false, NamingSimple, "test copy 9007199254740992 copy.js"},
		{"Increment file name with just version in name", "copy.js", false, NamingSimple, "copy copy.js"},
		{"Increment file name with just version in name, v2", "copy 2.js", false, NamingSimple, "copy 2 copy.js"},
		{"Increment file name without any extension or version", "test", false, NamingSimple, "test copy"},
		{"Increment file name without any extension or version, trailing dot", "test.", false, NamingSimple, "test copy."},
		{"Increment file name without any extension or version, leading dot", ".test", false, NamingSimple, ".test copy"},
		{"Increment file name without any extension or version, leading dot v2", "..test", false, NamingSimple, ". copy.test"},
		{"Increment file name without any extension but with suffix version", "test copy 5", false, NamingSimple, "test copy 6"},
		{"Increment folder name without any version", "test", true, NamingSimple, "test copy"},
		{"Increment folder name with suffix version", "test copy", true, NamingSimple, "test copy 2"},
		{"Increment folder name with suffix version, leading zeros", "test copy 005", true, NamingSimple, "test copy 6"},
		{"Increment folder name with suffix version, too big number", "test copy 9007199254740992", true, NamingSimple, "test copy 9007199254740992 copy"},
		{"Increment folder name with just version in name", "copy", true, NamingSimple, "copy copy"},
		{"Increment folder name with just version in name, v2", "copy 2", true, NamingSimple, "copy 2 copy"},
		{"Increment folder name \"with extension\" but without any version", "test.js", true, NamingSimple, "test.js copy"},
		{"Increment folder name \"with extension\" and with suffix version", "test.js copy 5", true, NamingSimple, "test.js copy 6"},
		{"Increment file/folder name with suffix version, special case 1", "test copy 0", true, NamingSimple, "test copy"},
		{"Increment file/folder name with suffix version, special case 2", "test copy 1", true, NamingSimple, "test copy 2"},
		{"Increment file name without any version", "test.js", false, NamingSmart, "test.1.js"},
		{"Increment folder name without any version", "test", true, NamingSmart, "test.1"},
		{"Increment file name with suffix version", "test.1.js", false, NamingSmart, "test.2.js"},
		{"Increment file name with suffix version with trailing zeros", "test.001.js", false, NamingSmart, "test.002.js"},
		{"Increment file name with suffix version with trailing zeros, changing length", "test.009.js", false, NamingSmart, "test.010.js"},
		{"Increment file name with suffix version with '-' as separator", "test-1.js", false, NamingSmart, "test-2.js"},
		{"Increment file name with suffix version with '-' as separator, trailing zeros", "test-001.js", false, NamingSmart, "test-002.js"},
		{"Increment file name with suffix version with '-' as separator, trailing zeros, changnig length", "test-099.js", false, NamingSmart, "test-100.js"},
		{"Increment file name with suffix version with '_' as separator", "test_1.js", false, NamingSmart, "test_2.js"},
		{"Increment folder name with suffix version", "test.1", true, NamingSmart, "test.2"},
		{"Increment folder name with suffix version, trailing zeros", "test.001", true, NamingSmart, "test.002"},
		{"Increment folder name with suffix version with '-' as separator", "test-1", true, NamingSmart, "test-2"},
		{"Increment folder name with suffix version with '_' as separator", "test_1", true, NamingSmart, "test_2"},
		{"Increment file name with suffix version, too big number", "test.9007199254740992.js", false, NamingSmart, "test.9007199254740992.1.js"},
		{"Increment folder name with suffix version, too big number", "test.9007199254740992", true, NamingSmart, "test.9007199254740992.1"},
		{"Increment file name with prefix version", "1.test.js", false, NamingSmart, "2.test.js"},
		{"Increment file name with just version in name", "1.js", false, NamingSmart, "2.js"},
		{"Increment file name with just version in name, too big number", "9007199254740992.js", false, NamingSmart, "9007199254740992.1.js"},
		{"Increment file name with prefix version, trailing zeros", "001.test.js", false, NamingSmart, "002.test.js"},
		{"Increment file name with prefix version with '-' as separator", "1-test.js", false, NamingSmart, "2-test.js"},
		{"Increment file name with prefix version with '_' as separator", "1_test.js", false, NamingSmart, "2_test.js"},
		{"Increment file name with prefix version, too big number", "9007199254740992.test.js", false, NamingSmart, "9007199254740992.test.1.js"},
		{"Increment file name with just version and no extension", "001004", false, NamingSmart, "001005"},
		{"Increment file name with just version and no extension, too big number", "9007199254740992", false, NamingSmart, "9007199254740992.1"},
		{"Increment file name with no extension and no version", "file", false, NamingSmart, "file1"},
		{"Increment file name with no extension", "file1", false, NamingSmart, "file2"},
		{"Increment file name with no extension, too big number", "file9007199254740992", false, NamingSmart, "file9007199254740992.1"},
		{"Increment folder name with prefix version", "1.test", true, NamingSmart, "2.test"},
		{"Increment folder name with prefix version, too big number", "9007199254740992.test", true, NamingSmart, "9007199254740992.test.1"},
		{"Increment folder name with prefix version, trailing zeros", "001.test", true, NamingSmart, "002.test"},
		{"Increment folder name with prefix version  with '-' as separator", "1-test", true, NamingSmart, "2-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" "+tt.in, func(t *testing.T) {
			if got := IncrementFileName(tt.in, tt.folder, tt.naming); got != tt.want {
				t.Errorf("IncrementFileName(%q, %v, %s) = %q, want %q", tt.in, tt.folder, tt.naming, got, tt.want)
			}
		})
	}
}

// TestIncrementFileNameChain checks that repeated pastes make the names the Explorer shows.
func TestIncrementFileNameChain(t *testing.T) {
	tests := []struct {
		name   string
		start  string
		folder bool
		naming IncrementalNaming
		want   []string
	}{
		{"simple file", "x.txt", false, NamingSimple, []string{"x copy.txt", "x copy 2.txt", "x copy 3.txt"}},
		{"simple folder", "d", true, NamingSimple, []string{"d copy", "d copy 2", "d copy 3"}},
		{"smart file", "x.txt", false, NamingSmart, []string{"x.1.txt", "x.2.txt", "x.3.txt"}},
		{"smart no extension", "x", false, NamingSmart, []string{"x1", "x2"}},
		{"unknown value is simple", "x.txt", false, "bogus", []string{"x copy.txt", "x copy 2.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := tt.start
			for i, want := range tt.want {
				name = IncrementFileName(name, tt.folder, tt.naming)
				if name != want {
					t.Fatalf("step %d = %q, want %q", i+1, name, want)
				}
			}
		})
	}
}

func TestValidateFileName(t *testing.T) {
	taken := map[string]bool{"taken.txt": true, "dir": true}
	exists := func(name string) bool { return taken[name] }
	long := make([]byte, MaxNameBytes+1)
	for i := range long {
		long[i] = 'a'
	}
	tests := []struct {
		name    string
		input   string
		current string
		want    Severity // 0 for a valid name
		content string
	}{
		{"valid", "new.txt", "", 0, ""},
		{"valid with folders", "a/b/c.txt", "", 0, ""},
		{"valid trailing slash makes a folder", "newdir/", "", 0, ""},
		{"empty", "", "", SeverityError, "A file or folder name must be provided."},
		{"only spaces", "   ", "", SeverityError, "A file or folder name must be provided."},
		{"only slashes", "///", "", SeverityError, "A file or folder name must be provided."},
		{"leading slash", "/etc/passwd", "", SeverityError, "A file or folder name cannot start with a slash."},
		{"leading backslash", "\\x", "", SeverityError, "A file or folder name cannot start with a slash."},
		{"exists", "taken.txt", "", SeverityError, "A file or folder **taken.txt** already exists at this location. Please choose a different name."},
		{"exists as a folder with a slash", "dir/", "", SeverityError, "A file or folder **dir** already exists at this location. Please choose a different name."},
		{"the name it has is fine", "taken.txt", "taken.txt", 0, ""},
		{"dot", ".", "", SeverityError, "The name **.** is not valid as a file or folder name. Please choose a different name."},
		{"dot dot", "a/../b", "", SeverityError, "The name **a/../b** is not valid as a file or folder name. Please choose a different name."},
		{"stars are escaped", "*/..", "", SeverityError, "The name **\\*/..** is not valid as a file or folder name. Please choose a different name."},
		{"too long", string(long), "", SeverityError, "The name **" + string(long[:MaxNameBytes]) + "...** is not valid as a file or folder name. Please choose a different name."},
		{"leading space", " a.txt", "", SeverityWarning, "Leading or trailing whitespace detected in file or folder name."},
		{"trailing space in a part", "a /b.txt", "", SeverityWarning, "Leading or trailing whitespace detected in file or folder name."},
		{"tabs are trimmed", "\ta.txt\t", "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateFileName(tt.input, tt.current, exists)
			if tt.want == 0 {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %q", tt.content)
			}
			if got.Severity != tt.want || got.Content != tt.content {
				t.Errorf("got %+v, want severity %v content %q", got, tt.want, tt.content)
			}
		})
	}
}

func TestCleanNewName(t *testing.T) {
	tests := []struct {
		in    string
		rel   string
		slash bool
	}{
		{"a.txt", "a.txt", false},
		{"dir/", "dir", true},
		{"a/b//c.txt", "a/b/c.txt", false},
		{"\ta\t", "a", false},
	}
	for _, tt := range tests {
		rel, slash := CleanNewName(tt.in)
		if rel != tt.rel || slash != tt.slash {
			t.Errorf("CleanNewName(%q) = %q, %v, want %q, %v", tt.in, rel, slash, tt.rel, tt.slash)
		}
	}
}
