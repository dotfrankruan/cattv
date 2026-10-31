package terminal

import "testing"

func TestDetectUsesEnvironmentForNonTTY(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	t.Setenv("LINES", "40")
	t.Setenv("CATTV_CELL_WIDTH", "10")
	t.Setenv("CATTV_CELL_HEIGHT", "20")
	g := Detect(nil)
	if g.TTY {
		t.Fatal("nil output reported as TTY")
	}
	if g.Columns != 100 || g.Rows != 40 {
		t.Fatalf("geometry = %+v", g)
	}
	if g.CellWidth != 10 || g.CellHeight != 20 {
		t.Fatalf("cell size = %dx%d", g.CellWidth, g.CellHeight)
	}
	if g.PixelWidth != 1000 || g.PixelHeight != 800 {
		t.Fatalf("pixel size = %dx%d", g.PixelWidth, g.PixelHeight)
	}
}

func TestDetectRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv("COLUMNS", "nope")
	t.Setenv("LINES", "-1")
	t.Setenv("CATTV_CELL_WIDTH", "0")
	g := Detect(nil)
	if g.Columns != defaultColumns || g.Rows != defaultRows {
		t.Fatalf("geometry = %+v", g)
	}
	if g.CellWidth != defaultCellWidth {
		t.Fatalf("cell width = %d", g.CellWidth)
	}
}
