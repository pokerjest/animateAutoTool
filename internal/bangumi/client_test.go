package bangumi

import "testing"

func TestNormalizeBangumiImageURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "protocol relative",
			in:   "//lain.bgm.tv/pic/cover/l/test.jpg",
			want: "https://lain.bgm.tv/pic/cover/l/test.jpg",
		},
		{
			name: "http absolute",
			in:   "http://lain.bgm.tv/pic/cover/l/test.jpg",
			want: "https://lain.bgm.tv/pic/cover/l/test.jpg",
		},
		{
			name: "https absolute",
			in:   "https://lain.bgm.tv/pic/cover/l/test.jpg",
			want: "https://lain.bgm.tv/pic/cover/l/test.jpg",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBangumiImageURL(tc.in); got != tc.want {
				t.Fatalf("normalizeBangumiImageURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
