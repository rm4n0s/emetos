package workbench

import (
	"reflect"
	"testing"
)

func TestLabelWorkspaceTabs(t *testing.T) {
	cases := []struct {
		name    string
		folders []string
		want    []string // the Text of each label
	}{
		{"nothing", nil, []string{}},
		{"one folder", []string{"/home/me/api"}, []string{"api"}},
		{"different names need nothing", []string{"/home/me/api", "/home/me/notes"}, []string{"api", "notes"}},
		{"no folder", []string{""}, []string{"No Folder"}},
		{"same name, the nearest folder tells them apart", []string{"/home/me/work/api", "/home/me/home/api"}, []string{"api — work", "api — home"}},
		{"three of the same name", []string{"/a/x/api", "/a/y/api", "/b/z/api"}, []string{"api — x", "api — y", "api — z"}},
		{"the nearer folders are the same", []string{"/a/x/src", "/b/x/src"}, []string{"src — a/x", "src — b/x"}},
		{"only the tabs of the same name are labelled", []string{"/w/api", "/h/api", "/w/notes"}, []string{"api — w", "api — h", "notes"}},
		{"a tab with a shorter path keeps its parent", []string{"/api", "/home/api"}, []string{"api — /", "api — home"}},
		{"a trailing slash is ignored", []string{"/w/api/", "/h/api"}, []string{"api — w", "api — h"}},
		{"two tabs without a folder stay the same", []string{"", ""}, []string{"No Folder", "No Folder"}},
		{"a folder named like the no folder tab", []string{"/w/No Folder", ""}, []string{"No Folder", "No Folder"}},
		{"the root", []string{"/"}, []string{"/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := make([]string, 0, len(tc.folders))
			for _, l := range LabelWorkspaceTabs(tc.folders) {
				got = append(got, l.Text())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestTabLabelParts(t *testing.T) {
	l := LabelWorkspaceTabs([]string{"/w/api", "/h/api"})[0]
	if l.Name != "api" || l.Description != "w" {
		t.Errorf("got %+v, want the name api and the description w", l)
	}
}
