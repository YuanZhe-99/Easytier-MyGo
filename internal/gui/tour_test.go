package gui

import (
	"math"
	"testing"
)

// TestAffineParts: scaling, then slanting, then turning by the parts gives
// back the map they were split from, for the computers' tops and for maps
// that turn either way.
func TestAffineParts(t *testing.T) {
	cases := map[string][4]float64{
		"identity":   {1, 0, 0, 1},
		"turned":     {0.8, 0.6, -0.6, 0.8},
		"slanted":    {1.2, 0.3, -0.5, 0.7},
		"turned-ccw": {0.9, -0.4, 0.5, 0.6},
	}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		pb := computerModel(goos)
		o, ux, dv := pb.at(0, 0, 0), pb.at(10, 0, 0), pb.at(0, 0, -10)
		cases[goos] = [4]float64{float64(ux[0]-o[0]) / 10, float64(ux[1]-o[1]) / 10, float64(dv[0]-o[0]) / 10, float64(dv[1]-o[1]) / 10}
	}
	for name, m := range cases {
		sx, sy, skew, turn := affineParts(m[0], m[1], m[2], m[3])
		k := math.Tan(skew * math.Pi / 180)
		s, c := math.Sincos(turn * math.Pi / 180)
		// Rotation · Slant · Scale, column by column.
		got := [4]float64{c * sx, s * sx, c*k*sy - s*sy, s*k*sy + c*sy}
		for i := range got {
			if math.Abs(got[i]-m[i]) > 1e-9 {
				t.Errorf("%s: parts %v %v %v %v give %v, want %v", name, sx, sy, skew, turn, got, m)
				break
			}
		}
	}
}

// TestTourReplayChangesNothing: the tour shown again from Settings starts
// at the run mode in use and, closed, goes back to Settings › Run mode
// with the run mode unchanged, whatever was looked at.
func TestTourReplayChangesNothing(t *testing.T) {
	a := demoApp(t)
	a.settingsOpen, a.settingsAt = true, setRunMode
	a.openTour()
	if a.settingsOpen || a.tour == nil || !a.tour.replay || a.tour.mode != choiceFollow {
		t.Fatalf("open: settings %v, tour %+v", a.settingsOpen, a.tour)
	}
	a.tour.mode, a.tour.step = choiceBackground, tourReady
	a.closeTour()
	if a.tour != nil || !a.settingsOpen || a.settingsAt != setRunMode {
		t.Fatalf("closed: settings %v at %v, tour %+v", a.settingsOpen, a.settingsAt, a.tour)
	}
	if a.prefs.Mode != ModeHelper || a.currentChoice() != choiceFollow {
		t.Errorf("closing changed the run mode: %v / %v", a.prefs.Mode, a.currentChoice())
	}
}
