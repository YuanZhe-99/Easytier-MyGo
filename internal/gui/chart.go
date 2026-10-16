package gui

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// chartWindows are the selectable spans of the traffic chart.
var chartWindows = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// niceSteps are the top values the vertical axis snaps to (×1024ⁿ).
var niceSteps = []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}

// axisTop picks the vertical scale's top for a peak: the next "nice"
// 1024-based value above it, so the axis reads 64 KB/s, 128 KB/s, …
func axisTop(peak float64) float64 {
	if peak <= 0 || math.IsNaN(peak) {
		return 1024
	}
	unit := 1.0
	for peak > unit*1024 {
		unit *= 1024
	}
	for _, s := range niceSteps {
		if s*unit >= peak {
			return s * unit
		}
	}
	return 1024 * unit
}

// trafficChart draws download and upload over the chosen window: areas
// with a monotone line, breaks for gaps, a hover read-out.
func (a *App) trafficChart(c *ui.Context, all []localapi.TrafficPoint) {
	t := c.Theme()
	k := tok(c)
	a.mu.Lock()
	span := chartWindows[a.window5m]
	sel := &a.chartSel
	a.mu.Unlock()

	end := time.Now()
	if len(all) > 0 && all[len(all)-1].Time.After(end) {
		end = all[len(all)-1].Time
	}
	start := end.Add(-span)
	var pts []localapi.TrafficPoint
	peak := 0.0
	for _, p := range all {
		if p.Time.Before(start) {
			continue
		}
		pts = append(pts, p)
		if !p.Gap {
			peak = max(peak, p.Rx, p.Tx)
		}
	}
	top := axisTop(peak)
	x := chart.NewTime(start, end)
	y := chart.NewLinear(0, top)
	y.Format = func(v float64) string { return formatRate(v) }
	yt := []chart.Tick{{At: 0, Value: 0, Label: formatRate(0)}, {At: 0.5, Value: top / 2, Label: formatRate(top / 2)}, {At: 1, Value: top, Label: formatRate(top)}}
	xt := x.Ticks(4)

	n := len(pts)
	xs := make([]float32, n)
	rx := make([]float32, n)
	tx := make([]float32, n)
	nan := float32(math.NaN())
	for i, p := range pts {
		xs[i] = x.At(p.Time)
		if p.Gap {
			rx[i], tx[i] = nan, nan
			continue
		}
		rx[i], tx[i] = y.At(p.Rx), y.At(p.Tx)
	}
	describe := func(i int) string {
		p := pts[i]
		if p.Gap {
			return L.ChartGap(p.Time.Format("15:04:05"))
		}
		return L.ChartPoint(p.Time.Format("15:04:05"), formatRate(p.Rx), formatRate(p.Tx))
	}
	label := func(side chart.Side, tick chart.Tick) {
		ui.Text(c, tick.Label).NoWrap().FontSize(sizeSmall).TextColor(t.TextMuted)
	}
	readout := L.ChartHint()
	if i := *sel; i >= 0 && i < n {
		readout = describe(i)
	}
	ui.Text(c, readout).FontSize(sizeSmall).TextColor(t.TextMuted)
	chart.Plot(c, chart.Axes{Left: yt, Bottom: xt, Label: label}, func(area ui.Element) {
		area.Draw(func(p *ui.Painter, r ui.Rect) {
			for _, tick := range yt {
				p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, k.hairline)
			}
			if n < 2 {
				return
			}
			p.FillPath(chart.Area(r, xs, nil, rx, chart.CurveMonotone), k.download.Alpha(0.14))
			p.StrokePath(chart.Line(r, xs, rx, chart.CurveMonotone), 2, k.download)
			p.FillPath(chart.Area(r, xs, nil, tx, chart.CurveMonotone), k.upload.Alpha(0.10))
			p.StrokePath(chart.Line(r, xs, tx, chart.CurveMonotone), 2, k.upload)
		})
		if n > 0 {
			chart.Select(c, area, sel, xs, describe)
		}
		if i := *sel; i >= 0 && i < n && !pts[i].Gap {
			chart.RuleX(c, xs[i], 1).Background(t.TextMuted.Alpha(0.6))
			chart.Point(c, xs[i], rx[i], 9, 9).Radius(4.5).Background(k.download).Border(2, t.Surface)
			chart.Point(c, xs[i], tx[i], 9, 9).Radius(4.5).Background(k.upload).Border(2, t.Surface)
		}
	}).Label(L.ChartLabel()).Description(fmt.Sprintf("%s, %s", L.TrafficDownload(), L.TrafficUpload())).Height(180).GapX(8).GapY(6)
}
