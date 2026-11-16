package domain

import "time"

func elapsedMilliseconds(start *time.Time, now time.Time) int64 {
	// 缺少记录或时钟回拨时不生成负耗时。
	if start == nil || now.Before(*start) {
		return 0
	}
	return now.Sub(*start).Milliseconds()
}
