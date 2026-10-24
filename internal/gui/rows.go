package gui

import (
	"github.com/egoist/mygo/ui"
)

// Layout of the pages built from rows: Settings, the editor and a
// disconnected network's summary.
const (
	pageColumnW   float32 = 720 // a page's column, at most
	sizePageTitle float32 = 20  // its title
	listRadius    float32 = 12  // a list of rows
	rowMinH       float32 = 54  // a row
	tileSize      float32 = 32  // the tile a row starts with
	rowControlW   float32 = 260 // a field on a row's right
	blockIndent   float32 = 62  // a block under its row, lined up with the row's words
)

// pageColumn is a page's body: it scrolls, its column centred and no
// wider than maxW.
func pageColumn(c *ui.Context, key string, maxW float32, body func()) {
	ui.Scroll(c.Key(key)).Grow(1).FillWidth().Padding(4, 24, 24, 24).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key(key + "-col")).FillWidth().MaxWidth(maxW).Children(body)
	})
}

// pageIntro is a page's title and its one sentence on what it is for.
func pageIntro(c *ui.Context, title, about string) {
	t := c.Theme()
	ui.Text(c, title).FontSize(sizePageTitle).Bold().Margin(14, 0, 0, 0)
	if about != "" {
		ui.Text(c, about).TextColor(t.TextMuted).Margin(4, 0, 0, 0)
	}
}

// rows adds a list's rows, a line between each; hit, when set, tells
// whether a row is the setting a search leads to (Settings), to scroll it
// into view and tint it. Each call takes the frame's context, which the
// struct must not keep.
type rows struct {
	n   int
	hit func(title string) (tint, scroll bool)
}

// section is a titled group of rows: its muted head with aside on the
// right (a note or a button; nil for none), then its rows in one rounded
// list.
func (r *rows) section(c *ui.Context, key, title string, aside func(), body func(r *rows)) {
	t := c.Theme()
	head := ui.Row(c.Key(key+"-head")).FillWidth().MinHeight(28).AlignItems(ui.Center).Padding(18, 4, 8, 4).Gap(gap)
	if r.hit != nil {
		if _, scroll := r.hit(title); scroll {
			head.ScrollIntoView()
		}
	}
	head.Children(func() {
		ui.Text(c, title).FontSize(sizeSmall).Bold().TextColor(t.TextMuted).Grow(1)
		if aside != nil {
			aside()
		}
	})
	bg := t.Background
	if t.Dark {
		bg = t.Surface
	}
	ui.Column(c.Key(key)).FillWidth().Radius(listRadius).Border(1, t.Border).Background(bg).Clip().Children(func() {
		body(&rows{hit: r.hit})
	})
}

// line draws the line above every row but the first.
func (r *rows) line(c *ui.Context) {
	if r.n > 0 {
		ui.Box(c).Height(1).FillWidth().Shrink(0).Background(c.Theme().Border)
	}
	r.n++
}

// row is one setting: its icon in a tile (nil for none), its title, one
// sentence (muted unless col is set), and its control on the right (nil
// for none).
func (r *rows) row(c *ui.Context, key string, icon *ui.SVG, title, about string, control func()) {
	r.rowIn(c, key, icon, title, about, ui.Color{}, control)
}

// rowIn is row with its sentence in col.
func (r *rows) rowIn(c *ui.Context, key string, icon *ui.SVG, title, about string, col ui.Color, control func()) {
	t := c.Theme()
	r.line(c)
	e := ui.Row(c.Key(key)).FillWidth().MinHeight(rowMinH).Padding(9, 16).Gap(14).AlignItems(ui.Center)
	if r.hit != nil {
		if tint, scroll := r.hit(title); tint || scroll {
			if tint {
				e.Background(t.Accent.Alpha(0.1))
			}
			if scroll {
				e.ScrollIntoView()
			}
		}
	}
	e.Children(func() {
		if icon != nil {
			ui.Box(c).Size(tileSize, tileSize).Shrink(0).Radius(8).Center().Background(tileBackground(t)).Children(func() {
				ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(t.TextMuted)
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontWeight(500)
			if about != "" {
				if col.A == 0 {
					col = t.TextMuted
				}
				ui.Text(c, about).FontSize(sizeSmall).TextColor(col)
			}
		})
		if control != nil {
			control()
		}
	})
}

// block is free content under the row before it, lined up with its words:
// a text area, a list, a form.
func (r *rows) block(c *ui.Context, key string, body func()) {
	ui.Column(c.Key(key)).FillWidth().Padding(0, 16, 14, blockIndent).Gap(8).Children(body)
}

// callout is a tinted strip with an icon, words and actions: in the accent
// for something to decide, in the warning color for a problem.
func callout(c *ui.Context, key string, tone ui.Color, icon *ui.SVG, msg string, actions func()) {
	ui.Row(c.Key(key)).FillWidth().Shrink(0).Padding(10, 12, 10, 14).Radius(10).Border(1, tone).Background(tone.Alpha(0.1)).
		Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(tone)
		ui.Text(c, msg).Grow(1).MinWidth(0)
		if actions != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(actions)
		}
	})
}
