package vt10x

import "testing"

// Sequences with a '<', '=' or '>' prefix or an intermediate byte are not the
// plain CSI commands sharing their final byte. Claude Code sends the kitty
// keyboard ones at startup; read as DECRC they moved the cursor to the row
// saved by an earlier ESC 7.
func TestForeignCSIKeepsCursor(t *testing.T) {
	for _, seq := range []string{
		"\x1b[>5u", "\x1b[<u", "\x1b[?u", "\x1b[=1;1u", // kitty keyboard
		"\x1b[?s",          // XTSAVE, not DECSC
		"\x1b[>4;2m",       // XTMODKEYS, not SGR
		"\x1b[1;1;1;1;7$r", // DECCARA, not DECSTBM
	} {
		vt := New(WithSize(20, 10))
		vt.Write([]byte("\x1b[2;3H\x1b7\x1b[6;8H\x1b[1m"))
		vt.Write([]byte(seq))
		cur := vt.Cursor()
		if cur.X != 7 || cur.Y != 5 {
			t.Errorf("%q moved cursor to %d,%d; want 5,7", seq, cur.Y, cur.X)
		}
		if cur.Attr.Mode&(1<<2) == 0 {
			t.Errorf("%q reset SGR attributes", seq)
		}
	}
}

func TestPlainCSIUStillRestoresCursor(t *testing.T) {
	vt := New(WithSize(20, 10))
	vt.Write([]byte("\x1b[2;3H\x1b[s\x1b[6;8H\x1b[u"))
	if cur := vt.Cursor(); cur.X != 2 || cur.Y != 1 {
		t.Errorf("CSI u restored to %d,%d; want 1,2", cur.Y, cur.X)
	}
}
