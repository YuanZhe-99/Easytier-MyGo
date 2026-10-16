package app

// A small area/line chart for the instance's traffic curve. It is drawn
// with the ui painter rather than a chart library: the series is short
// (one window of points) and the rules that matter are about gaps, not
// about plotting features.
//
// Reading rules (report §8.2): a gap is a break in the line, never a dip
// to zero; a partial interval is drawn but not filled; the vertical scale
// adapts to the window's peak so a quiet network still shows a shape.

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// trafficChart renders rx/tx bytes-per-second over the whole window. The
// caller sizes it; it fills its box.
func trafficChart(c *ui.Context, points []localapi.TrafficPoint, height float32) {
	t := c.Theme()
	ui.Box(c).FillWidth().Height(height).Draw(func(p *ui.Painter, r ui.Rect) {
		if len(points) < 2 {
			return
		}
		p.Fill(r, t.Surface, 6)

		const padTop, padBottom, padSide = 6.0, 4.0, 4.0
		plotH := r.H - padTop - padBottom
		plotW := r.W - 2*padSide
		if plotH <= 4 || plotW <= 8 {
			return
		}

		peak := 0.0
		for _, pt := range points {
			if pt.Gap {
				continue
			}
			if pt.RxBytesPS > peak {
				peak = pt.RxBytesPS
			}
			if pt.TxBytesPS > peak {
				peak = pt.TxBytesPS
			}
		}
		if peak <= 0 {
			// A flat zero window: draw the baseline, not a misleading
			// empty box.
			p.Fill(ui.Rect{X: r.X + padSide, Y: r.Y + padTop + plotH, W: plotW, H: 1}, t.Border, 0)
			return
		}
		// Leave a little headroom so the peak does not touch the top.
		scale := float64(plotH) / (peak * 1.15)

		xAt := func(i int) float32 {
			return r.X + padSide + plotW*float32(i)/float32(len(points)-1)
		}
		yAt := func(v float64) float32 {
			return r.Y + padTop + plotH - float32(v*scale)
		}

		// A faint baseline at zero.
		p.Fill(ui.Rect{X: r.X + padSide, Y: r.Y + padTop + plotH, W: plotW, H: 1}, t.Border, 0)

		drawSeries := func(value func(localapi.TrafficPoint) float64, fill bool, width float32, color ui.Color) {
			var line ui.Path
			var area ui.Path
			started := false
			areaStarted := false
			zero := r.Y + padTop + plotH
			for i, pt := range points {
				if pt.Gap {
					// Break the line: a new segment starts at the next
					// real point. The area closes before the break.
					if areaStarted {
						area.LineTo(xAt(i-1), zero).Close()
						areaStarted = false
					}
					started = false
					continue
				}
				x, y := xAt(i), yAt(value(pt))
				if !started {
					line.MoveTo(x, y)
					started = true
				} else {
					line.LineTo(x, y)
				}
				if fill {
					if !areaStarted {
						area.MoveTo(x, zero)
						areaStarted = true
					}
					area.LineTo(x, y)
				}
			}
			if areaStarted {
				area.LineTo(xAt(len(points)-1), zero).Close()
				// A very light wash under the line; the stroke carries the
				// shape.
				p.FillPath(&area, color.Alpha(0.18))
			}
			p.StrokePath(&line, width, color)
		}

		// Received and sent are the two series. Rx is drawn first and a
		// little thicker so both stay readable where they coincide (ping
		// and request/response traffic is symmetric).
		drawSeries(func(pt localapi.TrafficPoint) float64 { return pt.RxBytesPS }, true, 2, t.Accent)
		drawSeries(func(pt localapi.TrafficPoint) float64 { return pt.TxBytesPS }, false, 1.5, t.Success)
	})
}

// trafficSummary is the "current / peak" line under the chart.
//
// current is false when the latest interval is a gap: the last measured
// value is then not "now", and printing it as if it were would present
// stale evidence as current. measured reports whether the window holds
// any real measurement at all; with none, a numeric zero would be a lie.
func trafficSummary(points []localapi.TrafficPoint) (rxNow, txNow, peak float64, current, measured bool) {
	for _, pt := range points {
		if pt.Gap {
			continue
		}
		measured = true
		if pt.RxBytesPS > peak {
			peak = pt.RxBytesPS
		}
		if pt.TxBytesPS > peak {
			peak = pt.TxBytesPS
		}
	}
	if !measured {
		return 0, 0, 0, false, false
	}
	last := points[len(points)-1]
	if last.Gap {
		return 0, 0, peak, false, true
	}
	return last.RxBytesPS, last.TxBytesPS, peak, true, true
}

// trafficWindow reports the span of the curve, for the chart's caption.
func trafficWindow(points []localapi.TrafficPoint) time.Duration {
	if len(points) < 2 {
		return 0
	}
	return points[len(points)-1].Time.Sub(points[0].Time)
}

// humanRate renders a bytes-per-second value for people.
func humanRate(bps float64) string {
	switch {
	case bps < 0:
		return "—"
	case bps < 1000:
		return itoa(int(bps)) + " B/s"
	case bps < 1000*1000:
		return oneDecimal(bps/1000) + " KB/s"
	case bps < 1000*1000*1000:
		return oneDecimal(bps/1_000_000) + " MB/s"
	default:
		return oneDecimal(bps/1_000_000_000) + " GB/s"
	}
}

// oneDecimal formats with one decimal place without pulling in fmt.
func oneDecimal(v float64) string {
	whole := int(v)
	frac := int((v - float64(whole)) * 10)
	if frac < 0 {
		frac = 0
	}
	if frac > 9 {
		frac = 9
	}
	return itoa(whole) + "." + itoa(frac)
}
