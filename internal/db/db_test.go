package db

import "testing"

func TestSQLiteDriverPathLeavesNonWindowsUntouched(t *testing.T) {
	t.Parallel()

	prev := currentDBGOOS
	currentDBGOOS = func() string { return "darwin" }
	t.Cleanup(func() { currentDBGOOS = prev })

	input := "/tmp/animate.db"
	if got := sqliteDriverPath(input); got != input {
		t.Fatalf("sqliteDriverPath(%q) = %q, want unchanged", input, got)
	}
}

func TestSQLiteDriverPathUsesWALOnWindows(t *testing.T) {
	prev := currentDBGOOS
	currentDBGOOS = func() string { return "windows" }
	t.Cleanup(func() { currentDBGOOS = prev })

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain path",
			input: `C:\AnimateAutoTool\data\app.db`,
			want:  `C:\AnimateAutoTool\data\app.db?_pragma=journal_mode(WAL)`,
		},
		{
			name:  "existing query",
			input: `C:\AnimateAutoTool\data\app.db?cache=shared`,
			want:  `C:\AnimateAutoTool\data\app.db?cache=shared&_pragma=journal_mode(WAL)`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteDriverPath(tc.input); got != tc.want {
				t.Fatalf("sqliteDriverPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
