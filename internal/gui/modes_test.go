package gui

import "testing"

// TestWindowsArg guards the helper install on Windows: the bundle path
// contains "EasyTier MyGo", and an unquoted space split it in two.
func TestWindowsArg(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`install`, `install`},
		{`S-1-5-21-1-2-3-1001`, `S-1-5-21-1-2-3-1001`},
		{`C:\Users\a\AppData\Local\Programs\EasyTier MyGo\bin`, `"C:\Users\a\AppData\Local\Programs\EasyTier MyGo\bin"`},
		{`C:\dir with space\`, `"C:\dir with space\\"`},
		{`say "hi"`, `"say \"hi\""`},
		{`a\"b`, `"a\\\"b"`},
		{``, `""`},
	} {
		if got := windowsArg(tc.in); got != tc.want {
			t.Errorf("windowsArg(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
