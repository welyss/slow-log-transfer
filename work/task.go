package work

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/welyss/slow-log-transfer/elasticsearch"
	"github.com/welyss/slow-log-transfer/mysql"
	"github.com/xwb1989/sqlparser"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EsKeywordMaxLen = 32766
	SQLShortLength  = 512
)

var (
	meta       = []byte(fmt.Sprintf(`{ "index" : { "_type" : "doc" } }%s`, "\n"))
	BufferSize = 16777216
	SQLRedactLimit  = -1
)

type Task struct {
	instance         string
	dbs              *mysql.DBService
	es               *elasticsearch.ESService
	lastPoint        string
	intervalInSecond int64
	eviction         bool
	index            string
	count            int64
}

func NewTask(instance string, mysqlDsn string, esServices []string, intervalInSecond int64, eviction bool, index string) *Task {
	// service init
	dbs := mysql.NewDBService(mysqlDsn, 1, 1, 0)
	es := elasticsearch.NewESService(esServices)
	if intervalInSecond <= 0 {
		intervalInSecond = 30
	}
	return &Task{instance, dbs, es, "", intervalInSecond, eviction, index, 0}
}

func (task *Task) Run(stopSignal <-chan int) {
	log.Printf("task [%s] start at %s\n", task.instance, time.Now())
	// go routines stop signal
	defer func() {
		if err := recover(); err != nil {
			log.Printf("run time panic: %v", err)
		}
		<-stopSignal
	}()
	var buf bytes.Buffer
	var query string
	var args []interface{}
	if task.eviction {
		query = "SELECT start_time, user_host, query_time, lock_time, rows_sent, rows_examined, db, sql_text, thread_id FROM slow_log lock in share mode"
	} else {
		query = "SELECT start_time, user_host, query_time, lock_time, rows_sent, rows_examined, db, sql_text, thread_id FROM slow_log where start_time > ? lock in share mode"
		args = append(args, task.lastPoint)
	}
	log.Printf("task %s query: %s , args: %d.\n", task.instance, query, len(args))
	for {
		task.count = 0
		actionInLoop(task, &buf, query, args...)
	}
}

func actionInLoop(task *Task, buf *bytes.Buffer, query string, args ...interface{}) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("***** loop panic, loop will be continue: %v *****", err)
		}

		// wait for next
		time.Sleep(time.Duration(task.intervalInSecond) * time.Second)
	}()
	// Reset the buffer and items counter
	buf.Reset()
	var lastPointTmp string
	task.count = 0
	updated := 0

	// Execute the query
	tx := task.dbs.QueryWithFetchWithTx(func(values [][]byte) {
		slowlog := &Slowlog{InstanceId: task.instance}
		// start_time
		slowlog.StartTime = string(values[0])
		// support for mysql v5.6
		if len(slowlog.StartTime) == 19 {
			slowlog.StartTime += ".000001"
		}
		// convert by es default timezone
		local, _ := time.ParseInLocation(FullTimeLayout, slowlog.StartTime, time.Local)
		slowlog.StartTime = local.In(time.UTC).Format(FullTimeLayout)
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
		// sql_text_short
		// replace params of sql to ?
		var parsedSQL string
		if SQLRedactLimit > 0 && len(slowlog.SqlText) > SQLRedactLimit {
			parsedSQL = slowlog.SqlText
			log.Println("sql size:", len(slowlog.SqlText), "over", SQLRedactLimit)
		} else {
			parsedSQL, _ = sqlparser.RedactSQLQuery(slowlog.SqlText)
			if err == nil && parsedSQL != "" {
				parsedSQL = strings.Replace(parsedSQL, ":redacted", "?", -1)
			} else {
				parsedSQL = slowlog.SqlText
			}
		}
		// cut
		runes := []rune(parsedSQL)
		if len(runes) > SQLShortLength {
			parsedSQL = string(runes[:SQLShortLength])
		}
		slowlog.SqlTextShort = parsedSQL
		// sql_text_encode
		slowlog.SqlTextEncode = url.QueryEscape(slowlog.SqlTextShort)
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
		task.count++
		updated++

		// flush to es
		//	log.Printf("task %s fetch %d records.\n", task.instance, task.count)
		if buf.Len() > BufferSize {
			// add suffix to index name
			index := task.index + "-" + time.Now().Format("2006.01.02")
			esBegin := time.Now()
			task.es.Bulk(index, buf.Bytes())
			log.Printf("task %s indexed %d records in %f seconds in loop with %d bytes.\n", task.instance, updated, time.Since(esBegin).Seconds(), buf.Len())
			buf.Reset()
			updated = 0
		}

	}, query, args...)

	defer func() {
		if err := recover(); err != nil {
			log.Printf("transaction inner loop panic, tx rollback: %v", err)
			tx.Rollback()
		}
	}()

	if updated > 0 && buf.Len() > 0 {
		// add suffix to index name
		index := task.index + "-" + time.Now().Format("2006.01.02")
		esBegin := time.Now()
		task.es.Bulk(index, buf.Bytes())
		log.Printf("task %s indexed %d records in %f seconds in the end.\n", task.instance, updated, time.Since(esBegin).Seconds())
		buf.Reset()
		updated = 0
	}

	if task.count > 0 {
		// clear table
		if task.eviction {
			_, _, err := task.dbs.ExecWithTx(tx, "truncate table slow_log")
			if err != nil {
				panic(err.Error())
			}
			log.Println("truncate table slow_log was success")
		}
	}

	// tx commit
	tx.Commit()

	// update last Point
	if lastPointTmp > task.lastPoint {
		task.lastPoint = lastPointTmp
	}
}

func init() {
	log.SetOutput(os.Stdout)
}
