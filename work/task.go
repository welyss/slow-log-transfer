package work

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/welyss/slow-log-transfer/elasticsearch"
	"github.com/welyss/slow-log-transfer/mysql"
	"log"
	"os"
	"strconv"
	"time"
)

const ()

var (
	meta = []byte(fmt.Sprintf(`{ "index" : { "_type" : "doc" } }%s`, "\n"))
)

type Task struct {
	dbs              *mysql.DBService
	es               *elasticsearch.ESService
	lastPoint        string
	intervalInSecond int64
}

func NewTask(mysqlDsn string, esServices []string, intervalInSecond int64) *Task {
	// service init
	dbs := mysql.NewDBService(mysqlDsn, 1, 1, 0)
	es := elasticsearch.NewESService(esServices)
	return &Task{dbs, es, "", intervalInSecond}
}

func (work *Task) Run() {
	var buf bytes.Buffer
	for {
		// Reset the buffer and items counter
		buf.Reset()
		var lastPointTmp string
		count := 0
		esBegin := time.Now()
		// Execute the query
		work.dbs.QueryWithFetch(func(values [][]byte) {
			slowlog := &Slowlog{InstanceId: `wystest`}
			// start_time
			slowlog.StartTime = string(values[0])
			lastPointTmp = slowlog.StartTime
			// user_host
			matched := re.FindAllStringSubmatch(string(values[1]), -1)
			if len(matched) > 0 {
				match := matched[0]
				slowlog.User = match[1]
				slowlog.Host = match[2]
			}
			// query_time
			t, err := time.ParseInLocation(TimeLayout, string(values[2]), time.Local)
			if err != nil {
				panic(err.Error())
			} else {
				slowlog.QueryTime = t.Sub(zero).Seconds()
			}
			// lock_time
			t, err = time.ParseInLocation(TimeLayout, string(values[3]), time.Local)
			if err != nil {
				panic(err.Error())
			} else {
				slowlog.LockTime = t.Sub(zero).Seconds()
			}
			// rows_sent
			intValue, _ := strconv.Atoi(string(values[4]))
			slowlog.RowsSent = int64(intValue)
			// rows_examined
			intValue, _ = strconv.Atoi(string(values[5]))
			slowlog.RowsExamined = int64(intValue)
			// db
			slowlog.Db = string(values[6])
			// sql_text
			slowlog.SqlText = string(values[7])
			// thread_id
			intValue, _ = strconv.Atoi(string(values[8]))
			slowlog.ThreadId = int64(intValue)

			// format into bytes
			data, err := json.Marshal(slowlog)
			if err != nil {
				panic(err.Error())
			}
			// Append newline to the data payload
			//
			data = append(data, "\n"...) // <-- Comment out to trigger failure for batch

			// Append payloads to the buffer (ignoring write errors)
			buf.Grow(len(meta) + len(data))
			buf.Write(meta)
			buf.Write(data)
			count++
		}, "SELECT start_time, user_host, query_time, lock_time, rows_sent, rows_examined, db, sql_text, thread_id FROM test.slow_log where start_time > ?", work.lastPoint)
		// flush to es
		if count > 0 {
			work.es.Bulk("mysqlslowlogs", buf.Bytes())
			log.Printf("there are %d records indexed in %f seconds.\n", count, time.Since(esBegin).Seconds())
		}
		// update last Point
		if lastPointTmp > work.lastPoint {
			work.lastPoint = lastPointTmp
		}
		// wait for next
		time.Sleep(time.Duration(work.intervalInSecond) * time.Second)
	}
}

func init() {
	log.SetOutput(os.Stdout)
}
