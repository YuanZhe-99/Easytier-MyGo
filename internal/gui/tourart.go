package gui

import (
	"math"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// Layout and timing of the tour's illustrations (doc/en-us/gui.md, "First
// run"). Each is drawn in an artW×artH space and scaled to fit its column.
const (
	artW        float32 = 320
	artH        float32 = 580
	artPeerR    float32 = 22  // a peer's round node
	artTileR    float32 = 11  // an app's tile
	artDash     float32 = 3   // a flow's dash
	artDashGap  float32 = 7   // and the gap after it
	artFlowW    float32 = 2   // a flow's width
	artOrbR     float32 = 11  // the running network on the run-mode layers
	artCamera   float64 = 420 // the camera's distance from a model's centre
	artFlowTime         = 900 * time.Millisecond
	artPulse            = 1800 * time.Millisecond
	artSleep            = 8 * time.Second // awake, asleep and back
)

// The illustrations' fixed colors: the computers are real objects and keep
// their finish in both appearances; the light's, the check's, the sleeping
// light's.
var (
	artLED      = ui.Hex("#34D399")
	artLEDSleep = ui.Hex("#F59E0B")
	artWhite    = ui.Hex("#FFFFFF")
)

// artPlatform is the system whose computer the illustrations show; tests
// set it to draw each one.
var artPlatform = runtime.GOOS

// artState is what an illustration shows. The view animates the values and
// the illustration only draws them.
type artState struct {
	step  tourStep
	tun   float32 // the device step: 1 with a virtual device, 0 without
	mode  float32 // the run mode looked at, easing between 0, 1 and 2
	login float32 // open at login: 1 on
	barY  float32 // the startup step's online bar, its top
	barH  float32 // and its height
	wait  float32 // the startup step: 1 while networks wait for the app
	pop   float32 // the ready step's check, 0 to 1
	still bool    // Reduce Motion: nothing moves by itself
	mode0 runChoice
}

// artColors are an illustration's theme colors.
type artColors struct {
	text, muted, card, edge, flow, inbound, cross, accent, good ui.Color
}

// artPainter draws in the illustration's space.
type artPainter struct {
	p   *ui.Painter
	col artColors
	now time.Time
	st  artState
}

// drawArt draws the illustration of s into r, centred and scaled to fit.
// Side effects: asks for the next frame while something moves.
func drawArt(p *ui.Painter, r ui.Rect, s artState, col artColors) {
	scale := min(r.W/artW, r.H/artH)
	ox, oy := r.X+(r.W-artW*scale)/2, r.Y+(r.H-artH*scale)/2
	if !s.still {
		p.AnimationFrame()
	}
	d := artPainter{p: p, col: col, now: p.Now(), st: s}
	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(ox, oy)), 0, 0, func() {
		switch s.step {
		case tourDevice:
			d.deviceScene()
		case tourModes:
			d.modesScene()
		case tourStartup:
			d.startupScene()
		case tourAwake:
			d.awakeScene()
		default:
			d.networkScene(s.step == tourReady)
		}
	})
}

// phase is how far a loop of period has come, from 0 to 1 (0 when still).
func (d artPainter) phase(period time.Duration) float32 {
	if d.st.still {
		return 0
	}
	return float32(d.now.UnixMilli()%period.Milliseconds()) / float32(period.Milliseconds())
}

// text draws s with its top-left corner at x, y (align 0), centred on x
// (align 1) or ending at x (align 2), wrapping at width when it is not 0.
func (d artPainter) text(x, y float32, s string, size float32, weight int, c ui.Color, align int, width float32) {
	if c.A == 0 { // a span without a color would take the default one
		return
	}
	span := ui.Span{Text: s, Size: size, Weight: weight, Color: c}
	w, _ := d.p.MeasureText(width, span)
	switch align {
	case 1:
		x -= w / 2
	case 2:
		x -= w
	}
	d.p.RichText(x, y, width, span)
}

// flow draws a dashed line from (x0, y0) to (x1, y1) whose dashes move
// toward its end, as data going that way.
func (d artPainter) flow(x0, y0, x1, y1 float32, c ui.Color, moving bool) {
	dx, dy := x1-x0, y1-y0
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n == 0 || c.A == 0 {
		return
	}
	period := artDash + artDashGap
	off := float32(0)
	if moving {
		off = d.phase(artFlowTime) * period
	}
	var path ui.Path
	for s := off - period; s < n; s += period {
		a, b := max(s, 0), min(s+artDash, n)
		if b <= a {
			continue
		}
		path.MoveTo(x0+dx*a/n, y0+dy*a/n).LineTo(x0+dx*b/n, y0+dy*b/n)
	}
	d.p.StrokePath(&path, artFlowW, c)
}

// peer draws a device of the network: a round node with its icon.
func (d artPainter) peer(x, y float32, icon *ui.SVG) {
	node := (&ui.Path{}).Circle(x, y, artPeerR)
	d.p.FillPath(node, d.col.card)
	d.p.StrokePath(node, 1.5, d.col.edge)
	d.p.Icon(icon, ui.Rect{X: x - 10, Y: y - 10, W: 20, H: 20}, d.col.text)
}

// chip draws a rounded label, at most the width of the space: its left
// edge at x (or its centre, centred), its middle at y.
func (d artPainter) chip(x, y float32, s string, face, ink ui.Color, centred bool) {
	span := ui.Span{Text: s, Size: sizeSmall, Weight: 600, Color: ink}
	w, h := d.p.MeasureText(0, span)
	cw := w + 20
	if centred {
		x -= cw / 2
	}
	x = min(max(x, 4), artW-4-cw)
	d.p.Fill(ui.Rect{X: x, Y: y - 12, W: cw, H: 24}, face, 12)
	d.p.RichText(x+10, y-h/2, 0, span)
}

// computer draws this computer in perspective with its top's centre at
// (x, y), and its light, lit or asleep.
func (d artPainter) computer(x, y float32, asleep float32) {
	m := computerModel(artPlatform)
	m.draw(d.p, x, y)
	led := artLED.Mix(artLEDSleep, asleep)
	d.p.FillPath((&ui.Path{}).Circle(x+m.led[0], y+m.led[1], 2.4), led)
}

// networkScene is the welcome's and the ready step's: this computer and
// three devices of its network, data flowing between them; ready adds a
// check and the run mode chosen.
func (d artPainter) networkScene(ready bool) {
	c := d.col
	d.flow(160, 310, 60, 192, c.flow, true)
	d.flow(160, 142, 160, 300, c.flow, true)
	d.flow(160, 310, 260, 192, c.flow, true)
	d.peer(60, 170, iconLaptop)
	d.peer(160, 120, iconPhone)
	d.peer(260, 170, iconServer)
	if !ready {
		d.text(60, 128, L.TourArtLaptop(), sizeSmall, 400, c.muted, 1, 0)
		d.text(160, 78, L.TourArtPhone(), sizeSmall, 400, c.muted, 1, 0)
		d.text(260, 128, L.TourArtNAS(), sizeSmall, 400, c.muted, 1, 0)
	}
	bob := float32(0)
	if !d.st.still {
		bob = -2.5 + 2.5*float32(math.Cos(2*math.Pi*float64(d.phase(5*time.Second))))
	}
	d.computer(160, 330+bob, 0)
	if !ready {
		d.text(160, 432, L.TourArtThis(), 13, 600, c.text, 1, 0)
		return
	}
	if d.st.pop > 0 {
		k := d.st.pop
		d.p.Transform(ui.Scaling(k, k), 214, 300, func() {
			badge := (&ui.Path{}).Circle(214, 300, 17)
			d.p.FillPath(badge, c.good)
			d.p.StrokePath(badge, 3, c.card)
			check := (&ui.Path{}).MoveTo(207, 300).LineTo(212, 305).LineTo(221, 295)
			d.p.StrokePath(check, 2.6, artWhite)
		})
	}
	mode := modeTitle(d.st.mode0)
	d.text(160, 432, mode, 13, 600, c.text, 1, 0)
	when := L.TourArtWhenOpen()
	if d.st.mode0 == choiceBackground {
		when = L.TourArtFromStartup()
	}
	d.text(160, 452, when, sizeSmall, 400, c.muted, 1, 0)
}

// deviceScene is the virtual device step: apps on this computer reach the
// network through the virtual device, or are stopped without it, while
// other devices reach this computer either way.
func (d artPainter) deviceScene() {
	c, tun := d.col, d.st.tun
	for i, icon := range []*ui.SVG{iconGlobe, iconTerminal, iconGamepad} {
		x := float32(80 + 80*i)
		d.p.Fill(ui.Rect{X: x - 22, Y: 78, W: 44, H: 44}, c.card, artTileR)
		d.p.Stroke(ui.Rect{X: x - 22, Y: 78, W: 44, H: 44}, c.edge, artTileR, 1.5)
		d.p.Icon(icon, ui.Rect{X: x - 10, Y: 90, W: 20, H: 20}, c.text)
	}
	d.text(160, 132, L.TourArtApps(), sizeSmall, 400, c.muted, 1, 0)
	line := c.muted.Alpha(0.5).Mix(c.flow, tun)
	moving := tun > 0.5
	d.flow(84, 154, 150, 280, line, moving)
	d.flow(160, 154, 160, 280, line, moving)
	d.flow(236, 154, 170, 280, line, moving)
	if no := 1 - tun; no > 0.01 {
		for _, x := range []float32{117, 160, 203} {
			mark := (&ui.Path{}).Circle(x, 217, 9)
			d.p.FillPath(mark, c.card.Alpha(no))
			d.p.StrokePath(mark, 1.6, c.cross.Alpha(no))
			cross := (&ui.Path{}).MoveTo(x-3.5, 213.5).LineTo(x+3.5, 220.5).MoveTo(x+3.5, 213.5).LineTo(x-3.5, 220.5)
			d.p.StrokePath(cross, 1.8, c.cross.Alpha(no))
		}
	}
	d.computer(160, 310, 0)
	if tun > 0.01 {
		d.p.Line(214, 354, 236, 354, 1.5, c.accent.Alpha(tun))
		d.chip(236, 354, L.TourArtDevice(), c.accent.Alpha(tun), artWhite.Alpha(tun), false)
	}
	d.flow(150, 402, 150, 452, c.flow.Alpha(tun), true)
	d.flow(170, 452, 170, 402, c.inbound, true)
	d.peer(160, 480, iconLaptop)
	d.text(160, 508, L.TourArtOther(), sizeSmall, 400, c.muted, 1, 0)
	d.flow(48, 548, 68, 548, c.flow, false)
	d.text(76, 540, L.TourArtOut(), sizeSmall, 400, c.muted, 0, 0)
	d.flow(48, 568, 68, 568, c.inbound, false)
	d.text(76, 560, L.TourArtIn(), sizeSmall, 400, c.muted, 0, 0)
}

// modesScene is the run modes step: the app, the helper and the system as
// layers of this computer, the running network on the one that runs it.
func (d artPainter) modesScene() {
	c, m := d.col, d.st.mode
	clamp := func(v float32) float32 { return max(0, min(v, 1)) }
	helperA := 0.3 + 0.7*clamp(m)
	appA := 1 - 0.55*clamp(m-1)
	sessionA := clamp(1 - float32(math.Abs(float64(m-1))))
	slab := func(y float32, face ui.Color, label string, alpha float32) {
		layerModel(face).drawAlpha(d.p, 150, y, alpha)
		d.text(242, y-8, label, sizeSmall, 400, c.text.Alpha(alpha), 0, 76)
	}
	slab(410, ui.Hex("#2C2D31"), L.TourArtSystem(), 1)
	slab(280, c.accent.Mix(artWhite, 0.72), L.TourArtHelper(), helperA)
	slab(150, c.card, L.TourArtApp(), appA)
	if sessionA > 0.01 {
		d.flow(150, 166, 150, 266, c.flow.Alpha(sessionA), true)
		d.text(160, 208, L.TourArtSession(), sizeSmall, 400, c.accent.Alpha(sessionA), 0, 0)
	}
	orbY := 150 + 130*clamp(m)
	pulse := d.phase(artPulse)
	if !d.st.still {
		d.p.FillPath((&ui.Path{}).Circle(150, orbY, artOrbR*(1+1.6*pulse)), c.accent.Alpha(0.55*(1-pulse)))
	}
	d.p.FillPath((&ui.Path{}).Circle(150, orbY, artOrbR), c.accent)
	d.p.FillPath((&ui.Path{}).Circle(150, orbY, 4), artWhite)
	if a := clamp(m); a > 0.01 {
		d.chip(110, 488, L.TourArtDevice(), c.accent.Alpha(a), artWhite.Alpha(a), true)
	}
	if a := clamp(m - 1); a > 0.01 {
		d.chip(214, 488, L.TourArtStartup(), c.text.Alpha(a), c.card.Alpha(a), true)
	}
	caption := []string{L.TourArtUserCaption(), L.TourArtFollowCaption(), L.TourArtBackgroundCaption()}[int(m+0.5)]
	d.text(160, 516, caption, sizeSmall, 400, c.text, 1, 300)
	d.p.FillPath((&ui.Path{}).Circle(100, 556, 6), c.accent)
	d.text(112, 548, L.TourArtLegend(), sizeSmall, 400, c.muted, 0, 0)
}

// startupScene is the startup step: what happens after the computer
// starts, and the stretch of it in which this computer is online.
func (d artPainter) startupScene() {
	c, s := d.col, d.st
	d.text(40, 76, L.TourArtAfterStart(), sizeSmall, 600, c.muted, 0, 0)
	d.p.Line(48, 124, 48, 470, 2, c.edge)
	d.p.Fill(ui.Rect{X: 204, Y: 124, W: 16, H: 346}, c.edge.Alpha(0.6), 8)
	if s.wait > 0.01 {
		d.p.StrokeDashed(ui.Rect{X: 204, Y: 316, W: 16, H: 40}, c.accent.Alpha(s.wait), 8, 1.2)
	}
	d.p.Fill(ui.Rect{X: 204, Y: s.barY, W: 16, H: s.barH}, c.accent, 8)
	node := func(y float32, label, sub string, ink ui.Color, r float32, alpha float32) {
		dot := (&ui.Path{}).Circle(48, y, r)
		d.p.FillPath(dot, c.card.Alpha(alpha))
		d.p.StrokePath(dot, 2, ink.Alpha(alpha))
		d.text(68, y-9, label, 13, 400, ink.Alpha(alpha), 0, 128)
		if sub != "" {
			d.text(68, y+9, sub, sizeSmall, 400, c.muted.Alpha(alpha), 0, 128)
		}
	}
	node(130, L.TourArtPower(), "", c.text, 7, 1)
	node(220, L.TourArtSignInScreen(), L.TourArtNobody(), c.text, 7, 1)
	node(316, L.TourArtSignIn(), "", c.text, 7, 1)
	node(356, L.TourArtOpenApp(), "", c.accent, 5, s.wait)
	node(400, L.TourArtQuit(), "", c.text, 7, 1)
	label, caption := L.TourArtAlways(), L.TourArtAlwaysCaption()
	switch {
	case s.mode < 1.5 && s.login > 0.5:
		label, caption = L.TourArtAfterSignIn(), L.TourArtAfterSignInCaption()
	case s.mode < 1.5:
		label, caption = L.TourArtAfterOpen(), L.TourArtAfterOpenCaption()
	}
	d.text(228, s.barY+s.barH/2-8, label, sizeSmall, 600, c.accent, 0, 88)
	d.text(160, 506, caption, sizeSmall, 400, c.text, 1, 300)
}

// awakeScene is the staying-online step: this computer awake and online,
// then asleep and offline, in turn.
func (d artPainter) awakeScene() {
	c := d.col
	awake := float32(1)
	if !d.st.still {
		ph := d.phase(artSleep)
		switch {
		case ph < 0.42:
			awake = 1
		case ph < 0.5:
			awake = 1 - (ph-0.42)/0.08
		case ph < 0.92:
			awake = 0
		default:
			awake = (ph - 0.92) / 0.08
		}
	}
	asleep := 1 - awake
	d.flow(160, 300, 64, 182, c.flow.Alpha(awake), true)
	d.flow(256, 182, 160, 300, c.flow.Alpha(awake), true)
	if asleep > 0.01 {
		d.flow(160, 300, 64, 182, c.muted.Alpha(0.5*asleep), false)
		d.flow(256, 182, 160, 300, c.muted.Alpha(0.5*asleep), false)
	}
	d.p.Icon(iconSun, ui.Rect{X: 228, Y: 220, W: 24, H: 24}, ui.Hex("#D97706").Alpha(awake))
	d.p.Icon(iconMoon, ui.Rect{X: 228, Y: 220, W: 24, H: 24}, c.muted.Alpha(asleep))
	if asleep > 0.01 {
		z := d.phase(2400 * time.Millisecond)
		d.text(256, 206-8*z, "z", 13, 700, c.muted.Alpha(asleep*(1-z)), 0, 0)
		d.text(268, 192-8*z, "z", 16, 700, c.muted.Alpha(asleep*z), 0, 0)
	}
	d.peer(64, 160, iconLaptop)
	d.peer(256, 160, iconPhone)
	d.computer(160, 320, asleep)
	d.text(160, 456, L.TourArtAwake(), 13, 600, c.good.Alpha(awake), 1, 0)
	d.text(160, 456, L.TourArtAsleep(), 13, 600, c.muted.Alpha(asleep), 1, 0)
	d.text(160, 486, L.TourArtScreenOK(), sizeSmall, 400, c.muted, 1, 300)
}

// boxModel is a rounded box seen through a camera from the front left,
// above: a computer, or a layer of the run-mode illustration. Sizes are in
// the illustration's units, angles in degrees.
type boxModel struct {
	w, d, h, r float64
	yaw, pitch float64
	zoom       float64
	top        [2]ui.Color // the top, from its back to its front
	side       ui.Color    // the sides, before the light falls on them
	edge       ui.Color    // the top's rim
	logo       string      // the system's mark on the top (assets/logos), or ""
	logoColor  ui.Color
	logoSize   float32
	detail     ui.Color // ports and grooves
	detailW    float32
	ports      [][4]float64 // on the front face: x0, y0, x1, y1, the heights as fractions of h
	shadow     bool
}

// computerModel is the computer the illustrations show for a system: a
// Windows Dev Kit, a Mac mini, or a mini PC.
func computerModel(goos string) projectedBox {
	var m boxModel
	led := [2]float64{-30, -20} // the light, on the front face: x and height
	switch goos {
	case "windows":
		m = boxModel{w: 112, d: 86, h: 12, r: 10, yaw: 28, pitch: 30, zoom: 1.12,
			top: [2]ui.Color{ui.Hex("#6E6E72"), ui.Hex("#3A3A3D")}, side: ui.RGB(58, 58, 61), edge: ui.RGBA(255, 255, 255, 0.18),
			logo: "windows", logoColor: ui.RGBA(18, 18, 20, 0.5), logoSize: 20, detail: ui.Hex("#0B0B0C"), detailW: 2.2,
			ports: [][4]float64{{-46, 0.5, -42, 0.5}, {-37, 0.5, -33, 0.5}, {-26, 0.5, -19, 0.5}, {10, 0.5, 14, 0.5}}}
		led = [2]float64{40, -6}
	case "darwin":
		m = boxModel{w: 78, d: 78, h: 30, r: 15, yaw: 36, pitch: 28, zoom: 1.12,
			top: [2]ui.Color{ui.Hex("#FBFBFC"), ui.Hex("#D2D2D6")}, side: ui.RGB(230, 230, 234), edge: ui.RGBA(0, 0, 0, 0.1),
			logo: "apple", logoColor: ui.Hex("#1D1D1F"), logoSize: 26, detail: ui.Hex("#4A4A4F"), detailW: 2.2,
			ports: [][4]float64{{-22, 0.36, -22, 0.64}, {-15, 0.36, -15, 0.64}}}
		led = [2]float64{29, -15}
	default:
		m = boxModel{w: 84, d: 84, h: 40, r: 6, yaw: 34, pitch: 30, zoom: 1.08,
			top: [2]ui.Color{ui.Hex("#55565C"), ui.Hex("#2F3035")}, side: ui.RGB(54, 55, 60), edge: ui.RGBA(255, 255, 255, 0.14),
			logo: "linux", logoColor: ui.Hex("#ECECEF"), logoSize: 30, detail: ui.Hex("#1C1D20"), detailW: 1.2}
		// Two grooves around the front and the right side.
		for _, y := range []float64{1.0 / 3, 2.0 / 3} {
			m.ports = append(m.ports, [4]float64{-36, y, 36, y})
		}
	}
	m.shadow = true
	pb := m.project()
	pb.led = pb.at(led[0], led[1], -m.d/2-0.3)
	if goos != "windows" && goos != "darwin" {
		// The right side's grooves.
		for _, y := range []float64{-m.h / 3, -m.h * 2 / 3} {
			a, b := pb.at(m.w/2+0.3, y, -m.d/2+m.r), pb.at(m.w/2+0.3, y, m.d/2-m.r)
			pb.details = append(pb.details, [2][2]float32{a, b})
		}
	}
	return pb
}

// layerModel is a layer of the run-mode illustration, in a color.
func layerModel(face ui.Color) projectedBox {
	side := face.Mix(ui.RGB(0, 0, 0), 0.12)
	return boxModel{w: 108, d: 108, h: 9, r: 12, yaw: 45, pitch: 32, zoom: 1,
		top: [2]ui.Color{face, face.Mix(ui.RGB(0, 0, 0), 0.05)}, side: side, edge: ui.RGBA(0, 0, 0, 0.08)}.project()
}

// projectedBox is a boxModel seen through its camera, in the
// illustration's units around the top's centre.
type projectedBox struct {
	m       boxModel
	top     [][2]float32
	bottom  [][2]float32
	quads   [][4][2]float32
	stops   []ui.GradientStop
	details [][2][2]float32
	led     [2]float32
	logoT   ui.Transform
	at      func(x, y, z float64) [2]float32
}

// project sees m through its camera: the top's outline, the sides that face
// the camera as quads with their shading as a gradient from left to right,
// its ports, and the transform that lays the logo on the top.
func (m boxModel) project() projectedBox {
	ya, pa := m.yaw*math.Pi/180, m.pitch*math.Pi/180
	ca, sa, cb, sb := math.Cos(ya), math.Sin(ya), math.Cos(pa), math.Sin(pa)
	at := func(x, y, z float64) [2]float32 {
		x1, z1 := x*ca+z*sa, -x*sa+z*ca
		y2, z2 := y*cb+z1*sb, z1*cb-y*sb
		s := artCamera / (artCamera + z2) * m.zoom
		return [2]float32{float32(x1 * s), float32(-y2 * s)}
	}
	// The outline, around four quarter circles.
	const n = 6
	hw, hd, r := m.w/2, m.d/2, m.r
	var ring [][2]float64
	for _, k := range [][3]float64{{hw - r, hd - r, 0}, {-(hw - r), hd - r, 90}, {-(hw - r), -(hd - r), 180}, {hw - r, -(hd - r), 270}} {
		for i := 0; i <= n; i++ {
			t := (k[2] + 90*float64(i)/n) * math.Pi / 180
			ring = append(ring, [2]float64{k[0] + r*math.Cos(t), k[1] + r*math.Sin(t)})
		}
	}
	pb := projectedBox{m: m, at: at}
	for _, q := range ring {
		pb.top = append(pb.top, at(q[0], 0, q[1]))
		pb.bottom = append(pb.bottom, at(q[0], -m.h, q[1]))
	}
	// The sides facing the camera, lit from the front left.
	type lit struct {
		x float32
		c ui.Color
	}
	var shades []lit
	lx, lz := -0.35, -0.94
	minX, maxX := float32(math.Inf(1)), float32(math.Inf(-1))
	for i := range ring {
		p, q := ring[i], ring[(i+1)%len(ring)]
		dx, dz := q[0]-p[0], q[1]-p[1]
		l := math.Hypot(dx, dz)
		if l < 0.01 {
			continue
		}
		nx, nz := dz/l, -dx/l
		if nx*(p[0]+q[0])+nz*(p[1]+q[1]) < 0 {
			nx, nz = -nx, -nz
		}
		if -nx*sa+nz*ca >= 0 {
			continue
		}
		quad := [4][2]float32{at(p[0], 0, p[1]), at(q[0], 0, q[1]), at(q[0], -m.h, q[1]), at(p[0], -m.h, p[1])}
		pb.quads = append(pb.quads, quad)
		mid := (quad[0][0] + quad[1][0] + quad[2][0] + quad[3][0]) / 4
		k := float32(0.62 + 0.42*max(0, nx*lx+nz*lz))
		shades = append(shades, lit{mid, scaleColor(m.side, k)})
		for _, v := range quad {
			minX, maxX = min(minX, v[0]), max(maxX, v[0])
		}
	}
	for i := range shades {
		for j := i + 1; j < len(shades); j++ {
			if shades[j].x < shades[i].x {
				shades[i], shades[j] = shades[j], shades[i]
			}
		}
	}
	if span := maxX - minX; span > 0 {
		for _, s := range shades {
			pb.stops = append(pb.stops, ui.GradientStop{Color: s.c, At: (s.x - minX) / span})
		}
	}
	// Ports on the front face: from (x0, y0·h) to (x1, y1·h).
	zf := -hd - 0.3
	for _, pt := range m.ports {
		pb.details = append(pb.details, [2][2]float32{at(pt[0], -pt[1]*m.h, zf), at(pt[2], -pt[3]*m.h, zf)})
	}
	// The logo: the top's plane around its centre, as an affine transform
	// (scale, then slant, then turn), which is what perspective looks like
	// over so small a mark.
	o, ux, dv := at(0, 0, 0), at(10, 0, 0), at(0, 0, -10)
	sx, sy, skew, turn := affineParts(float64(ux[0]-o[0])/10, float64(ux[1]-o[1])/10, float64(dv[0]-o[0])/10, float64(dv[1]-o[1])/10)
	pb.logoT = ui.Scaling(float32(sx), float32(sy)).Then(ui.Skewing(float32(skew), 0)).Then(ui.Rotation(float32(turn)))
	return pb
}

// affineParts splits the linear map x' = a·x + c·y, y' = b·x + d·y into a
// scale (sx, sy), then a horizontal slant of skew degrees, then a clockwise
// turn of turn degrees, the transforms ui offers.
// Notes: the map must keep orientation (a·d − b·c > 0), as a top seen from
// above does.
func affineParts(a, b, c, d float64) (sx, sy, skew, turn float64) {
	sx, theta := math.Hypot(a, b), math.Atan2(b, a)
	slant := math.Cos(theta)*c + math.Sin(theta)*d
	sy = -math.Sin(theta)*c + math.Cos(theta)*d
	return sx, sy, math.Atan(slant/sy) * 180 / math.Pi, theta * 180 / math.Pi
}

// scaleColor darkens or lightens c by k.
func scaleColor(c ui.Color, k float32) ui.Color {
	f := func(v uint8) uint8 { return uint8(min(255, max(0, float32(v)*k+0.5))) }
	return ui.Color{R: f(c.R), G: f(c.G), B: f(c.B), A: c.A}
}

// poly is a closed path through pts, moved by (x, y).
func poly(pts [][2]float32, x, y float32) *ui.Path {
	var p ui.Path
	for i, v := range pts {
		if i == 0 {
			p.MoveTo(x+v[0], y+v[1])
		} else {
			p.LineTo(x+v[0], y+v[1])
		}
	}
	return p.Close()
}

// draw paints the box with its top's centre at (x, y).
func (pb projectedBox) draw(p *ui.Painter, x, y float32) { pb.drawAlpha(p, x, y, 1) }

// drawAlpha paints the box at an opacity: its shadow, its sides, its top,
// the logo on it, then its ports.
func (pb projectedBox) drawAlpha(p *ui.Painter, x, y float32, alpha float32) {
	m := pb.m
	if m.shadow {
		x0, x1, y1 := float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(-1))
		for _, v := range pb.bottom {
			x0, x1, y1 = min(x0, v[0]), max(x1, v[0]), max(y1, v[1])
		}
		// A soft oval on the ground: rings growing and fading, for a blur.
		cx, cy, rx := x+(x0+x1)/2, y+y1+2, (x1-x0)/2
		p.Transform(ui.Scaling(1, 0.16), cx, cy, func() {
			for i := range 5 {
				p.FillPath((&ui.Path{}).Circle(cx, cy, rx*(0.7+0.09*float32(i))), ui.RGBA(0, 0, 0, 0.07*alpha))
			}
		})
	}
	var sides ui.Path
	for _, q := range pb.quads {
		sides.MoveTo(x+q[0][0], y+q[0][1]).LineTo(x+q[1][0], y+q[1][1]).LineTo(x+q[2][0], y+q[2][1]).LineTo(x+q[3][0], y+q[3][1]).Close()
	}
	stops := make([]ui.GradientStop, len(pb.stops))
	for i, s := range pb.stops {
		stops[i] = ui.GradientStop{Color: s.Color.Alpha(alpha), At: s.At}
	}
	if len(stops) > 0 {
		g := ui.LinearGradient{From: stops[0].Color, To: stops[len(stops)-1].Color, Angle: 90, Stops: stops}
		p.FillPathGradient(&sides, g)
		p.StrokePathGradient(&sides, 0.6, g)
	}
	top := poly(pb.top, x, y)
	p.FillPathGradient(top, ui.LinearGradient{From: m.top[0].Alpha(alpha), To: m.top[1].Alpha(alpha), Angle: 180})
	p.StrokePath(top, 1, m.edge.Alpha(alpha))
	if m.logo != "" {
		if logo := loadLogo(m.logo); logo != nil {
			s := m.logoSize
			p.Transform(pb.logoT, x, y, func() {
				p.Icon(logo, ui.Rect{X: x - s/2, Y: y - s/2, W: s, H: s}, m.logoColor.Alpha(alpha))
			})
		}
	}
	if len(pb.details) > 0 {
		var det ui.Path
		for _, l := range pb.details {
			det.MoveTo(x+l[0][0], y+l[0][1]).LineTo(x+l[1][0], y+l[1][1])
		}
		p.StrokePath(&det, m.detailW, m.detail.Alpha(alpha))
	}
}
