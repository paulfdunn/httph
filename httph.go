package httph

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/paulfdunn/logh"
)

// URLCollectionData - Functions that collect data from multiple URLs will return instance(s) of this
// structure, in order to allow association of URL, Byte (data), and errors.
type URLCollectionData struct {
	URL      string
	Bytes    []byte
	Response *http.Response
	Err      error
}

const (
	appName = "quant"
)

// CollectURL - Pass in a URL, request timeout, HTTP method to use, and get back
// the body of the request. HTTP method MUST be one of: [MethodGet, MethodHead]
func CollectURL(urlIn string, timeout time.Duration, method string) ([]byte, *http.Response, error) {
	var req *http.Request
	u, err := url.Parse(urlIn)
	if err != nil {
		logh.Map[appName].Printf(logh.Error, "CollectURL error parsing urlIn:%v", err)
		return []byte{}, nil, err
	}

	var reqErr error
	switch method {
	case http.MethodGet:
		req, reqErr = http.NewRequest(http.MethodGet, u.String(), nil)
	case http.MethodHead:
		req, reqErr = http.NewRequest(http.MethodHead, u.String(), nil)
	default:
		err := fmt.Errorf("invalid method: %s", method)
		logh.Map[appName].Printf(logh.Error, "%v", err)
		return nil, nil, err
	}

	if reqErr != nil {
		logh.Map[appName].Printf(logh.Error, "Error creating http.Request:%+v", reqErr)
		return nil, nil, reqErr
	}
	req.Header.Set("Connection", "close")
	req.Close = true

	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Dial: (&net.Dialer{
			// This timeout is require in order to prevent "too many open file" errors.
			Timeout:   timeout,
			KeepAlive: timeout,
		}).Dial}
	client := http.Client{Timeout: timeout, Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		// Warning level, as the IP/host may be invalid, host down, etc.
		logh.Map[appName].Printf(logh.Warning, "CollectURL client error:%v", err)
		return []byte{}, resp, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	return body, resp, err
}

// CollectURLs - Pass in a slice of URLs, request timeout, HTTP method to use, and
// get back a slice of URLCollectionData with results.
// The URLs are processed in parallel using threads number of parallel requests.
func CollectURLs(urls []string, timeout time.Duration, method string, threads int) []URLCollectionData {
	// Channel to feed work to the go routines
	tasks := make(chan string, threads)
	// Channel to return data from the workers.
	workerOut := make(chan URLCollectionData, len(urls))
	// Data to return to caller
	var returnData []URLCollectionData

	// Spawn threads number of workers
	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(sendResult chan URLCollectionData) {
			for url := range tasks {
				b, resp, e := CollectURL(url, timeout, method)
				sendResult <- URLCollectionData{url, b, resp, e}
			}
			wg.Done()
		}(workerOut)
	}

	for _, url := range urls {
		tasks <- url
	}
	close(tasks)

	wg.Wait()
	// Workers are done, all data should have already been returned.
	close(workerOut)
	for r := range workerOut {
		returnData = append(returnData, r)
		logh.Map[appName].Printf(logh.Debug, "CollectURLs url:%v, error:%v", r.URL, r.Err)
	}

	return returnData
}
