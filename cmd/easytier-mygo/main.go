// easytier-mygo is the desktop GUI. The interface is being rebuilt (see
// PLAN.md, P2); until then this window only states that.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

func main() {
	mygo.App.SetName("EasyTier MyGo")
	mygo.App.SetVersion(version.Version)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title: "EasyTier MyGo", Width: 480, Height: 240,
			Content: ui.View(func(c *ui.Context) {
				ui.Column(c).Fill().Padding(24).Gap(8).Children(func() {
					ui.Text(c, "EasyTier MyGo "+version.Version).Bold()
					ui.Text(c, "The interface is being rebuilt. Use easytier-mygo-ctl meanwhile.")
				})
			}),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
