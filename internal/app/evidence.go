package app

import (
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

func sampleFresh(status localapi.Status, sampled, now time.Time, rounds int) bool {
	interval := time.Duration(status.Daemon.CheckIntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Second
	}
	age := now.Sub(sampled)
	return !sampled.IsZero() && age >= 0 && age <= time.Duration(rounds+2)*interval+3*time.Second
}

func sampleLabel(status localapi.Status, name string, sampled, now time.Time, rounds int) string {
	if sampled.IsZero() {
		return name + "：尚无成功采样"
	}
	label := name + "：最近成功采样 " + relTime(now, sampled)
	if !sampleFresh(status, sampled, now, rounds) {
		label += "（已过期，显示缓存）"
	}
	return label
}

func peerCountLabel(status localapi.Status, now time.Time) string {
	if status.PeerCollectorError != "" || !sampleFresh(status, status.PeerSampledAt, now, 2) {
		return "未知"
	}
	return itoa(len(status.Peers)) + " 个"
}
