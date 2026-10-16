package app

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"time"
)

func (a *App) setHistory(enabled bool) {
	client := a.currentClient()
	a.do(func(ctx context.Context) error {
		err := client.SetTrafficHistory(ctx, enabled)
		a.publishFor(client, func() {
			if err != nil {
				a.historyMessage = err.Error()
			} else {
				a.loadHistory()
			}
		})
		return err
	})
}

func (a *App) deleteHistory() {
	client := a.currentClient()
	a.confirmThen("删除当前网络流量历史？", "删除仅影响已关闭的流量历史，健康事件不删除。", func(ctx context.Context) error {
		err := client.DeleteTrafficHistory(ctx)
		a.publishFor(client, func() {
			if err != nil {
				a.historyMessage = err.Error()
			} else {
				a.loadHistory()
			}
		})
		return err
	})
}

func (a *App) loadHistory() {
	client := a.currentClient()
	a.historyPreview = nil
	a.historyMessage = "正在读取历史…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h, err := client.TrafficHistory(ctx)
		a.publishFor(client, func() {
			if err != nil {
				a.historyMessage = err.Error()
				return
			}
			a.historyPreview = h
			a.historyMessage = h.Error
		})
	}()
}

func (a *App) historyCard(c *ui.Context) {
	ui.Column(c.Key("history")).FillWidth().Padding(12).Gap(6).Children(func() {
		ui.Text(c, "可选流量历史").Bold()
		ui.Text(c, "默认关闭；仅 EasyTier 隧道连接计数器，缺测不是零流量。").FontSize(12)
		ui.Button(c, "读取流量历史").OnClick(a.loadHistory)
		if h := a.historyPreview; h != nil {
			if !a.readOnly() {
				if h.Enabled {
					ui.Button(c, "关闭历史记录").OnClick(func() { a.setHistory(false) })
				} else {
					ui.Button(c, "启用历史记录").OnClick(func() { a.setHistory(true) })
					ui.Button(c, "删除历史记录…").OnClick(a.deleteHistory)
				}
			}
			if !h.Enabled {
				ui.Text(c, "历史记录未启用")
			} else {
				var rx, tx, covered, gaps float64
				for _, b := range h.Buckets {
					rx += b.RxBytes
					tx += b.TxBytes
					covered += b.CoveredSeconds
					gaps += b.GapSeconds
				}
				ui.Text(c, fmt.Sprintf("%d 个桶 · 测得接收 %.0f 字节 / 发送 %.0f 字节", len(h.Buckets), rx, tx))
				ui.Text(c, fmt.Sprintf("覆盖 %.0f 秒 · 缺测 %.0f 秒 · 缺测流量未知", covered, gaps))
			}
		}
		if a.historyMessage != "" {
			ui.Text(c, a.historyMessage).TextColor(c.Theme().Warning)
		}
	})
}
