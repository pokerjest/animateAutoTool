package worker

import "testing"

func TestPathWithinRoot(t *testing.T) {
	root := "/media/anime"

	cases := []struct {
		path string
		want bool
	}{
		{path: "/media/anime/Show/episode01.mkv", want: true},
		{path: "/media/anime", want: true},
		{path: "/media/other/Show/episode01.mkv", want: false},
		{path: "/media/anime/../other/Show/episode01.mkv", want: false},
	}

	for _, tc := range cases {
		if got := pathWithinRoot(tc.path, root); got != tc.want {
			t.Fatalf("pathWithinRoot(%q, %q) = %v, want %v", tc.path, root, got, tc.want)
		}
	}
}
