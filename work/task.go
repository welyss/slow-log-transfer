package work

import (
	"bytes"
	"container/list"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/welyss/slow-log-transfer/elasticsearch"
	"github.com/welyss/slow-log-transfer/mysql"
	"github.com/xwb1989/sqlparser"
)

const (
	EsKeywordMaxLen = 32766
	SQLShortLength  = 512
)

var (
	meta           = []byte(fmt.Sprintf(`{ "index" : { "_type" : "doc" } }%s`, "\n"))
	BufferSize     = 16777216
	SQLRedactLimit = -1
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
	log.Println("Interval In Second:", intervalInSecond)
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
		query = "SELECT start_time, user_host, query_time, lock_time, rows_sent, rows_examined, db, sql_text, thread_id FROM slow_log_bak lock in share mode"
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
			log.Printf("***** %s loop panic, loop will be continue: %v *****", task.instance, err)
		}

		// wait for next
		time.Sleep(time.Duration(task.intervalInSecond) * time.Second)
	}()
	// Reset the buffer and items counter
	buf.Reset()
	var lastPointTmp string
	task.count = 0
	updated := 0

	// if eviction, switch table
	if task.eviction {
		_, _, err := task.dbs.Exec("drop table if exists slow_log_tmp")
		if err == nil {
			_, _, err := task.dbs.Exec("drop table if exists slow_log_bak")
			if err == nil {
				_, _, err := task.dbs.Exec("create table slow_log_tmp like slow_log")
				if err == nil {
					_, _, err := task.dbs.Exec("rename table slow_log to slow_log_bak, slow_log_tmp to slow_log")
					if err != nil {
						panic(err.Error())
					}
				} else {
					panic(err.Error())
				}
			} else {
				panic(err.Error())
			}
		} else {
			panic(err.Error())
		}
	}

	// Execute the query
	tx := task.dbs.QueryWithFetchWithTx(func(values [][]byte) {
		slowlog := &Slowlog{InstanceId: task.instance}
		// start_time
		slowlog.StartTime = string(values[0])
		// support for mysql v5.6
		if len(slowlog.StartTime) == 19 {
			slowlog.StartTime += ".000001"
		} else if len(slowlog.StartTime) > 19 && slowlog.StartTime[19:26] == ".000000" {
			slowlog.StartTime = slowlog.StartTime[0:25] + "1"
		}
		log.Println("====================slowlog.StartTime1:", slowlog.StartTime)
		// convert by es default timezone
		local, _ := time.ParseInLocation(DateTimeLayout, slowlog.StartTime, time.Local)
		// slowlog.StartTime = local.In(time.UTC).Format(FullTimeLayout)
		slowlog.StartTime = local.Format(FullTimeLayout)
		log.Println("======================slowlog.StartTime:", slowlog.StartTime)
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
		slowlog.Tables = []string{"unknown"}
		if SQLRedactLimit > 0 && len(slowlog.SqlText) > SQLRedactLimit {
			parsedSQL = slowlog.SqlText
			slowlog.Tables = []string{"unknown"}
			log.Println("sql size:", len(slowlog.SqlText), "over", SQLRedactLimit, ", skip RedactSQLQuery.")
		} else {
			parseResult, err := sqlparser.Parse(slowlog.SqlText)
			if err == nil {
				// parse successful
				switch stmt := parseResult.(type) {
				case *sqlparser.Select:
					tables := list.New()
					for _, tableExpr := range stmt.From {
						fetchTables(tableExpr, tables)
					}
					slowlog.Tables = make([]string, tables.Len())
					i := 0
					for e := tables.Front(); e != nil; e = e.Next() {
						slowlog.Tables[i] = e.Value.(string)
						i++
					}
				case *sqlparser.Insert:
					slowlog.Tables = []string{stmt.Table.Name.String()}
				case *sqlparser.Update:
					tables := list.New()
					for _, tableExpr := range stmt.TableExprs {
						fetchTables(tableExpr, tables)
					}
					slowlog.Tables = make([]string, tables.Len())
					i := 0
					for e := tables.Front(); e != nil; e = e.Next() {
						slowlog.Tables[i] = e.Value.(string)
						i++
					}
				case *sqlparser.Delete:
					tables := list.New()
					for _, tableExpr := range stmt.TableExprs {
						fetchTables(tableExpr, tables)
					}
					slowlog.Tables = make([]string, tables.Len())
					i := 0
					for e := tables.Front(); e != nil; e = e.Next() {
						slowlog.Tables[i] = e.Value.(string)
						i++
					}
				default: //default case
					slowlog.Tables = []string{"unknown"}
				}
			}
			parsedSQL, err = sqlparser.RedactSQLQuery(slowlog.SqlText)
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
			log.Printf("instance:[%s] transaction inner loop panic, tx rollback: %v", task.instance, err)
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

	//	if task.count > 0 {
	//		// clear table
	//		if task.eviction {
	//			_, _, err := task.dbs.ExecWithTx(tx, "truncate table slow_log")
	//			if err != nil {
	//				panic(err.Error())
	//			}
	//			log.Println("truncate table slow_log was success")
	//		}
	//	}

	// tx commit
	tx.Commit()

	// update last Point
	if lastPointTmp > task.lastPoint {
		task.lastPoint = lastPointTmp
	}
}

func fetchTables(tableExpr sqlparser.TableExpr, tables *list.List) {
	buf := sqlparser.NewTrackedBuffer(nil)
	switch t := tableExpr.(type) {
	case *sqlparser.AliasedTableExpr:
		t.RemoveHints().Expr.Format(buf)
		if strings.Contains(buf.String(), " ") {
			nestedSql := strings.TrimSuffix(strings.TrimPrefix(strings.Trim(buf.String(), " "), "("), ")")
			if nested, err := sqlparser.Parse(nestedSql); err == nil {
				switch stmt := nested.(type) {
				case *sqlparser.Select:
					for _, tableExpr := range stmt.From {
						fetchTables(tableExpr, tables)
					}
				case *sqlparser.Union:
					for _, tableExpr := range stmt.Left.(*sqlparser.Select).From {
						fetchTables(tableExpr, tables)
					}
					for _, tableExpr := range stmt.Right.(*sqlparser.Select).From {
						fetchTables(tableExpr, tables)
					}
				}
			}
		} else {
			tables.PushBack(buf.String())
		}
	case *sqlparser.JoinTableExpr:
		fetchTables(t.LeftExpr, tables)
		fetchTables(t.RightExpr, tables)
	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			fetchTables(expr, tables)
		}
	}
}

func init() {
	log.SetOutput(os.Stdout)
}
