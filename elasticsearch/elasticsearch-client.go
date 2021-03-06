package elasticsearch

import (
	"github.com/elastic/go-elasticsearch/v5"
	"net/http"
	//	"github.com/elastic/go-elasticsearch/v5/esapi"
	"bytes"
	"encoding/json"
	"log"
	"os"
	//	"crypto/tls"
	"time"
	"net"
)

type ESService struct {
	es *elasticsearch.Client
}

type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []struct {
		Index struct {
			ID     string `json:"_id"`
			Result string `json:"result"`
			Status int    `json:"status"`
			Error  struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
				Cause  struct {
					Type   string `json:"type"`
					Reason string `json:"reason"`
				} `json:"caused_by"`
			} `json:"error"`
		} `json:"index"`
	} `json:"items"`
}

func NewESService(address []string) *ESService {
	cfg := elasticsearch.Config{
		Addresses: address,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: 5* 60 * time.Second,
			DialContext:           (&net.Dialer{Timeout: 5* 60 * time.Second}).DialContext,
		},
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &ESService{es}
}

func (ess *ESService) Bulk(indexName string, body []byte) {
	res, err := ess.es.Bulk(bytes.NewReader(body), ess.es.Bulk.WithIndex(indexName))
	// Close the response body, to prevent reaching the limit for goroutines or file handles
	if err != nil {
		panic(err.Error())
	} else {
		defer res.Body.Close()
		var blk *bulkResponse
		if err = json.NewDecoder(res.Body).Decode(&blk); err != nil {
			panic(err.Error())
		} else {
			for _, d := range blk.Items {
				if d.Index.Status > 201 {
					panic(d.Index.Error.Reason)
				}
			}
		}
	}
}

func init() {
	log.SetOutput(os.Stdout)
}
