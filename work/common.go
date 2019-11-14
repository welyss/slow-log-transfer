package work

import (
	"log"
	"regexp"
	"time"
)

const (
	TimeLayout     = "15:04:05.999999"
	FullTimeLayout = "2006-01-02 15:04:05.999999"
	ZeroTime       = "0000-01-01 00:00:00.000000"
)

var (
	re   = regexp.MustCompile(`[^\[]+\[([^\]]+)\]\s*@\s*\[([^\]]+)\]\s*`)
	zero time.Time
)

type Slowlog struct {
	InstanceId   string  `json:"instance_id"`
	StartTime    string  `json:"start_time"`
	User         string  `json:"user"`
	Host         string  `json:"host"`
	QueryTime    float64 `json:"query_time"`
	LockTime     float64 `json:"lock_time"`
	RowsSent     int64   `json:"rows_sent"`
	RowsExamined int64   `json:"rows_examined"`
	Db           string  `json:"db"`
	SqlText      string  `json:"sql_text"`
	ThreadId     int64   `json:"thread_id"`
}

func init() {
	var err error
	zero, err = time.ParseInLocation(FullTimeLayout, ZeroTime, time.Local)
	if err != nil {
		log.Fatal("zero time parse error.", err.Error())
	}
}
