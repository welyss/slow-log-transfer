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
	instance         string
	dbs              *mysql.DBService
	es               *elasticsearch.ESService
	lastPoint        string
	intervalInSecond int64
	eviction         bool
}

func NewTask(instance string, mysqlDsn string, esServices []string, intervalInSecond int64, eviction bool) *Task {
	// service init
	dbs := mysql.NewDBService(mysqlDsn, 1, 1, 0)
	es := elasticsearch.NewESService(esServices)
	if intervalInSecond <= 0 {
		intervalInSecond = 30
	}
	return &Task{instance, dbs, es, "", intervalInSecond, eviction}
}

func (task *Task) Run(stopSignal <-chan int) {
	// go routines stop signal
	defer func() {
		if err := recover(); err != nil {
			log.Printf("run time panic: %v", err)
		}
		<-stopSignal
	}()
	var buf bytes.Buffer
	for {
		actionInLoop(task, &buf)
	}
}

func actionInLoop(task *Task, buf *bytes.Buffer) {
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
	count := 0
	esBegin := time.Now()
	// Execute the query
	tx := task.dbs.QueryWithFetchWithTx(func(values [][]byte) {
		slowlog := &Slowlog{InstanceId: task.instance}
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
	}, "SELECT start_time, user_host, query_time, lock_time, rows_sent, rows_examined, db, sql_text, thread_id FROM slow_log where start_time > ? lock in share mode", task.lastPoint)
	defer func() {
		if err := recover(); err != nil {
			log.Printf("transaction inner loop panic, tx rollback: %v", err)
			tx.Rollback()
		}
	}()

	// flush to es
//	log.Printf("task %s fetch %d records.", task.instance, count)
	if count > 0 {
		task.es.Bulk("mysqlslowlogs", buf.Bytes())
		log.Printf("task %s indexed %d records in %f seconds.\n", task.instance, count, time.Since(esBegin).Seconds())

		// clear table
		if task.eviction {
			_, _, err := task.dbs.ExecWithTx(tx, "truncate table slow_log")
			if err != nil {
				panic(err.Error())
			}
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
