package main

import (
	"flag"
	"github.com/welyss/slow-log-transfer/work"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"runtime"
)

const ()

var ()

type Config struct {
	Tasks []Task `yaml:"tasks"`
}

type Task struct {
	Instance string   `yaml:"instance"`
	Mysql    string   `yaml:"mysql"`
	Es       []string `yaml:"es"`
	Interval int64    `yaml:"interval"`
	Eviction bool     `yaml:"eviction"`
}

func main() {
	var numCores = flag.Int("n", 2, "number of CPU cores to use")
	var file = flag.String("f", "config.yaml", "Config file full path")
	flag.Parse()
	runtime.GOMAXPROCS(*numCores)

	config := getConf(*file)
	stopSignal := make(chan int)
	for _, tc := range config.Tasks {
		task := work.NewTask(tc.Instance, tc.Mysql, tc.Es, tc.Interval, tc.Eviction)
		go task.Run(stopSignal)
	}
	// wait all go routines finished.
	for _, _ = range config.Tasks {
		stopSignal <- 0
	}
}

func getConf(file string) *Config {
	c := Config{}
	yamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		log.Fatalf("yamlFile.Get err #%v ", err)
	}

	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}
	return &c
}

func init() {
	log.SetOutput(os.Stdout)
}
