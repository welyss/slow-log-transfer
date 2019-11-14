package main

import (
	//	"encoding/json"
	//	"fmt"
	"github.com/welyss/slow-log-transfer/work"
	//	"log"
	//	"regexp"
	//	"strconv"
	//	"strings"
	//	"bytes"
	//	"time"
)

const ()

var ()

func main() {
	dsn := "wuysh:wys19851011@/?readTimeout=30s&charset=utf8"
	ess := []string{"http://10.0.29.73:9200", "http://10.0.29.75:9200", "http://10.0.29.117:9200"}
	var intervalInSecond int64 = 10
	task := work.NewTask(dsn, ess, intervalInSecond)
	task.Run()
}
